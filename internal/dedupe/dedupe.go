// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package dedupe cleans up the copies of identical files found in folders
// (the duplicates of `onefold stats`): in each group one copy is kept by a
// rule, and every other copy is moved to the Trash, replaced by a hard link
// or a clone of the kept one, or listed in a deletion script for a person to
// run. `onefold dedupe` and the Statistics view of the GUI.
//
// Guarantees (pinned by dedupe_test.go):
//   - groups are confirmed on the whole content; right before acting on a
//     copy, it and the kept copy are read again in full and must both hold
//     the group's content, or nothing happens to it;
//   - dry run unless Execute; an execute needs the typed count of its
//     actions (Expect) and writes its journal (fsync'ed JSONL) before the
//     first change;
//   - nothing is deleted for good: the Trash, a replacement by the same
//     content, or a script a person runs;
//   - a replacement never leaves the name without the content: the new link
//     or clone is built under a temporary name, the original steps aside
//     under a `.onefold-dd` name, the new file takes the name, the original
//     is read once more and only then removed (else put back);
//     Resume finishes or undoes an interrupted one from its journal.
package dedupe

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mauriziofonte/onefold/internal/fsx"
	"github.com/mauriziofonte/onefold/internal/hashcache"
	"github.com/mauriziofonte/onefold/internal/purge"
	"github.com/mauriziofonte/onefold/internal/runctl"
	"github.com/mauriziofonte/onefold/internal/sparsehash"
	"github.com/mauriziofonte/onefold/internal/statedir"
	"github.com/mauriziofonte/onefold/internal/stats"
)

// Keep rules: which copy of a group stays.
const (
	KeepOldest   = "oldest"   // the oldest modification time
	KeepNewest   = "newest"   // the newest modification time
	KeepShortest = "shortest" // the shortest path
	KeepFirst    = "first"    // the copy under the first folder given
)

// Actions on the other copies.
const (
	ActTrash  = "trash"  // Recycle Bin / Trash: they can be put back
	ActLink   = "link"   // hard links of the kept copy (same volume)
	ActClone  = "clone"  // clones of the kept copy (same volume, APFS, Btrfs, XFS)
	ActScript = "script" // a deletion script for a person to review and run
)

// OldSuffix names an original copy set aside during its replacement: data
// in flight, never deleted except by the run or Resume that verified it.
const OldSuffix = ".onefold-dd"

// Options configure a cleanup.
type Options struct {
	Roots []string
	// Keep is the rule (default KeepOldest); KeepIn, when set, keeps a copy
	// inside that folder (the rule picks among several there) and leaves the
	// groups without one alone.
	Keep    string
	KeepIn  string
	Action  string
	MinSize int64 // smaller files are left alone (default 1 byte)
	Execute bool
	// Expect, when >= 0, must equal the number of copies an execute acts on
	// (the typed confirmation); checked before anything is touched.
	Expect  int
	Workers int
	Gate    *runctl.Gate
	// Cache speeds up finding the groups; nothing acts on a cached digest.
	Cache *hashcache.Cache
	// JournalDir holds the journals ("" = <state dir>/dedupe).
	JournalDir  string
	ProgressFor func(phase string) (stats.Progress, func())
	// BeforeExecute is called with the plan before the journal is written;
	// an error aborts with nothing touched (GUI: executed = confirmed).
	BeforeExecute func(*Result) error
}

// Item is one copy to act on.
type Item struct {
	Keep    string `json:"keep"`
	Extra   string `json:"extra"`
	Size    int64  `json:"size"`
	Content string `json:"content"`
	Why     string `json:"why,omitempty"`
}

// Result of a cleanup.
type Result struct {
	Roots     []string `json:"roots"`
	Keep      string   `json:"keep"`
	KeepIn    string   `json:"keepIn,omitempty"`
	Action    string   `json:"action"`
	Groups    int      `json:"groups"`            // groups acted on
	Items     []Item   `json:"items"`             // the copies to act on
	Bytes     int64    `json:"bytes"`             // their size: what the action frees
	Skipped   []Item   `json:"skipped,omitempty"` // left alone, with why
	Done      int      `json:"done"`
	DoneBytes int64    `json:"doneBytes"`
	Failed    []Item   `json:"failed,omitempty"`
	Journal   string   `json:"journal,omitempty"`
	Stopped   bool     `json:"stopped,omitempty"`
}

func (o *Options) defaults() error {
	if len(o.Roots) == 0 {
		return errors.New("dedupe: no folder given")
	}
	if o.Keep == "" {
		o.Keep = KeepOldest
	}
	switch o.Keep {
	case KeepOldest, KeepNewest, KeepShortest, KeepFirst:
	default:
		return fmt.Errorf("dedupe: keep rule %q: use oldest, newest, shortest or first", o.Keep)
	}
	switch o.Action {
	case ActTrash, ActLink, ActClone, ActScript:
	case "":
		return errors.New("dedupe: choose an action: trash, link, clone or script")
	default:
		return fmt.Errorf("dedupe: action %q: use trash, link, clone or script", o.Action)
	}
	if o.Workers <= 0 {
		o.Workers = max(1, runtime.NumCPU()/2)
	}
	if o.MinSize <= 0 {
		o.MinSize = 1
	}
	if o.KeepIn != "" {
		k, err := fsx.Canonical(o.KeepIn)
		if err != nil {
			return err
		}
		o.KeepIn = k
	}
	return nil
}

// Plan finds the groups (stats, confirmed on the whole content) and chooses
// the copy to keep in each. Read-only.
func Plan(o Options) (*Result, error) {
	if err := o.defaults(); err != nil {
		return nil, err
	}
	var groups []group
	so := stats.Options{Roots: o.Roots, Workers: o.Workers, OneVolume: true, Dups: stats.DupsFull, DupMin: o.MinSize,
		Gate: o.Gate, Cache: o.Cache, ProgressFor: o.ProgressFor, Depth: 1, Top: 1,
		OnGroup: func(size int64, content string, paths []string) {
			groups = append(groups, group{size: size, content: content, paths: paths})
		}}
	sr, err := stats.Run(so)
	if err != nil {
		return nil, err
	}
	res := &Result{Roots: sr.Roots, Keep: o.Keep, KeepIn: o.KeepIn, Action: o.Action}
	sort.Slice(groups, func(i, j int) bool { return groups[i].paths[0] < groups[j].paths[0] })
	for _, g := range groups {
		o.choose(res, g)
	}
	return res, nil
}

type group struct {
	size    int64
	content string
	paths   []string
}

type copyInfo struct {
	path string
	fi   os.FileInfo
	mt   time.Time
	root int
	in   bool // inside KeepIn
}

// choose picks the kept copy of g and lists the others.
func (o Options) choose(res *Result, g group) {
	var cs []copyInfo
	for _, p := range g.paths {
		fi, err := os.Lstat(p)
		if err != nil || !fi.Mode().IsRegular() || fi.Size() != g.size {
			continue // gone or changed since the scan
		}
		c := copyInfo{path: p, fi: fi, mt: fi.ModTime(), root: len(o.Roots)}
		for i, r := range o.Roots {
			if within(p, r) {
				c.root = i
				break
			}
		}
		c.in = o.KeepIn != "" && within(p, o.KeepIn)
		cs = append(cs, c)
	}
	if len(cs) < 2 {
		return
	}
	sort.SliceStable(cs, func(i, j int) bool { return o.better(cs[i], cs[j]) })
	keep := cs[0]
	if o.KeepIn != "" && !keep.in {
		for _, c := range cs {
			res.Skipped = append(res.Skipped, Item{Keep: "", Extra: c.path, Size: g.size, Content: g.content, Why: "no copy of this group inside " + o.KeepIn})
		}
		return
	}
	n := 0
	for _, c := range cs[1:] {
		if os.SameFile(keep.fi, c.fi) {
			continue // already one file (a hard link: Windows stats lists both names)
		}
		it := Item{Keep: keep.path, Extra: c.path, Size: g.size, Content: g.content}
		if (o.Action == ActLink || o.Action == ActClone) && !sameVolume(keep.path, c.path) {
			it.Why = "on another volume than the kept copy: a link or clone cannot cross volumes"
			res.Skipped = append(res.Skipped, it)
			continue
		}
		res.Items = append(res.Items, it)
		res.Bytes += g.size
		n++
	}
	if n > 0 {
		res.Groups++
	}
}

// better orders the copies of a group: the first one is kept. A copy inside
// KeepIn always comes first; ties end on the path, so the choice is stable.
func (o Options) better(a, b copyInfo) bool {
	if a.in != b.in {
		return a.in
	}
	switch o.Keep {
	case KeepOldest:
		if !a.mt.Equal(b.mt) {
			return a.mt.Before(b.mt)
		}
	case KeepNewest:
		if !a.mt.Equal(b.mt) {
			return a.mt.After(b.mt)
		}
	case KeepFirst:
		if a.root != b.root {
			return a.root < b.root
		}
	}
	if len(a.path) != len(b.path) {
		return len(a.path) < len(b.path)
	}
	if !a.mt.Equal(b.mt) {
		return a.mt.Before(b.mt)
	}
	return a.path < b.path
}

// within reports whether p is dir or below it (canonical spellings).
func within(p, dir string) bool {
	cp, err1 := fsx.Canonical(filepath.Dir(p))
	cd, err2 := fsx.Canonical(dir)
	if err1 != nil || err2 != nil {
		return false
	}
	rel, err := filepath.Rel(cd, cp)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// Run plans, then executes when asked: the Trash, the replacements, or (for
// ActScript) nothing but checks: the script is written by Script.
func Run(o Options) (*Result, error) {
	res, err := Plan(o)
	if err != nil || !o.Execute || o.Action == ActScript {
		return res, err
	}
	if err := o.defaults(); err != nil {
		return res, err
	}
	if o.Expect >= 0 && o.Expect != len(res.Items) {
		return res, fmt.Errorf("dedupe: %d copies to act on, confirmation said %d: the folders changed since the preview; preview again", len(res.Items), o.Expect)
	}
	if len(res.Items) == 0 {
		return res, nil
	}
	if o.Action == ActTrash {
		extras := make([]string, len(res.Items))
		for i, it := range res.Items {
			extras[i] = it.Extra
		}
		if err := purge.CheckTrash(extras); err != nil {
			return res, err
		}
	}
	if o.BeforeExecute != nil {
		if err := o.BeforeExecute(res); err != nil {
			return res, err
		}
	}
	j, err := openJournal(o, res)
	if err != nil {
		return res, fmt.Errorf("journal: %w", err)
	}
	defer j.close()
	res.Journal = j.path
	prog, stop := stats.Progress(nil), func() {}
	if o.ProgressFor != nil {
		prog, stop = o.ProgressFor("dedupe")
	}
	defer stop()
	if prog != nil {
		for _, it := range res.Items {
			prog.AddTotal(it.Size)
		}
		prog.TotalKnown()
	}
	var mu sync.Mutex
	var flushed fsx.DirSet
	jobs := make(chan Item)
	var wg sync.WaitGroup
	for w := 0; w < o.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for it := range jobs {
				t0 := time.Now()
				if !o.Gate.Wait() {
					continue
				}
				err := act(o.Action, it, j, &flushed)
				mu.Lock()
				if err != nil {
					it.Why = err.Error()
					res.Failed = append(res.Failed, it)
					j.write(entry{Op: "fail", Extra: it.Extra, Keep: it.Keep, Why: it.Why})
				} else {
					res.Done++
					res.DoneBytes += it.Size
				}
				mu.Unlock()
				if prog != nil {
					if err != nil {
						prog.Err()
					}
					prog.Item(it.Size, time.Since(t0))
				}
			}
		}()
	}
	for _, it := range res.Items {
		if o.Gate.Stopped() {
			break
		}
		jobs <- it
	}
	close(jobs)
	wg.Wait()
	res.Stopped = o.Gate.Stopped()
	sort.Slice(res.Failed, func(a, b int) bool { return res.Failed[a].Extra < res.Failed[b].Extra })
	j.write(entry{Op: "end", Done: res.Done, Failed: len(res.Failed)})
	return res, nil
}

// verify reads the kept copy and the other one in full: both must hold the
// group's content.
func verify(it Item) error {
	kh, err := sparsehash.FullFile(it.Keep)
	if err != nil {
		return fmt.Errorf("kept copy unreadable (%v): NOT touched", err)
	}
	eh, err := sparsehash.FullFile(it.Extra)
	if err != nil {
		return fmt.Errorf("unreadable (%v): NOT touched", err)
	}
	if kh != it.Content || eh != it.Content {
		return errors.New("content differs from the kept copy now: NOT touched")
	}
	return nil
}

// durable is fsx.Durable; a test replaces it.
var durable = fsx.Durable

func act(action string, it Item, j *journal, flushed *fsx.DirSet) error {
	if err := verify(it); err != nil {
		return err
	}
	// the kept copy is the one left: its bytes and name reach the disk first
	if err := durable(it.Keep, flushed); err != nil {
		return fmt.Errorf("kept copy could not be flushed to disk (%v): NOT touched", err)
	}
	switch action {
	case ActTrash:
		if err := purge.MoveToTrash(it.Extra); err != nil {
			return err
		}
		j.write(entry{Op: "trash", Extra: it.Extra, Keep: it.Keep, Content: it.Content})
		return nil
	case ActLink, ActClone:
		return replace(action, it, j)
	}
	return fmt.Errorf("dedupe: action %q", action)
}

// oldName is where the original copy waits while its name gets the
// replacement.
func oldName(extra string) string {
	return filepath.Join(filepath.Dir(extra), "."+filepath.Base(extra)+OldSuffix)
}

// replace swaps the copy for a hard link or a clone of the kept one.
func replace(action string, it Item, j *journal) error {
	fi, err := os.Lstat(it.Extra)
	if err != nil {
		return err
	}
	tmp, old := fsx.TempFor(it.Extra, false), oldName(it.Extra)
	if _, err := os.Lstat(old); err == nil {
		return fmt.Errorf("%s exists (an interrupted run: onefold dedupe -resume its journal): NOT touched", filepath.Base(old))
	}
	os.Remove(tmp) // a onefold temp left by an interrupted run
	switch action {
	case ActLink:
		if err := fsx.LinkNoReplace(it.Keep, tmp); err != nil {
			return err
		}
	case ActClone:
		if err := fsx.Clone(it.Keep, tmp); err != nil {
			if errors.Is(err, fsx.ErrNoClone) {
				return errors.New("this file system cannot clone (use -action link or trash): NOT touched")
			}
			return err
		}
		if h, err := sparsehash.FullFile(tmp); err != nil || h != it.Content {
			os.Remove(tmp)
			return fmt.Errorf("the clone does not read back as the content (%v): NOT touched", err)
		}
		// the copy keeps what a file manager shows of it
		_ = os.Chmod(tmp, fi.Mode().Perm())
		_ = os.Chtimes(tmp, fi.ModTime(), fi.ModTime())
	}
	if err := fsx.SyncFile(tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := j.write(entry{Op: "replace", Action: action, Extra: it.Extra, Keep: it.Keep, Content: it.Content, Old: old, Tmp: tmp}); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("journal not writable (%v): NOT touched", err)
	}
	if err := fsx.RenameNoReplace(it.Extra, old); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := fsx.RenameNoReplace(tmp, it.Extra); err != nil {
		if berr := fsx.RenameNoReplace(old, it.Extra); berr != nil {
			return fmt.Errorf("%v; the original is %s (put it back by hand)", err, old)
		}
		os.Remove(tmp)
		return err
	}
	// the original, out of the way: written to meanwhile? then it goes back
	if h, herr := sparsehash.FullFile(old); herr != nil || h != it.Content {
		if os.Remove(it.Extra) == nil && fsx.RenameNoReplace(old, it.Extra) == nil {
			return errors.New("changed during the replacement: put back, NOT replaced")
		}
		return fmt.Errorf("changed during the replacement; the original is %s", old)
	}
	if err := fsx.SyncDir(filepath.Dir(it.Extra)); err != nil {
		return fmt.Errorf("folder could not be flushed (%v); the original stays as %s", err, old)
	}
	if err := os.Remove(old); err != nil {
		return fmt.Errorf("replaced; the original could not be removed: %s (%v)", old, err)
	}
	j.write(entry{Op: "done", Extra: it.Extra})
	return nil
}

// Script writes the commands that delete the copies of res, each read again
// now with its kept copy (only verified ones are written; the others go to
// res.Failed): PowerShell when ps (Remove-Item -LiteralPath), else POSIX sh
// (rm --). The script stops at the first failure; onefold runs nothing.
func Script(w io.Writer, res *Result, ps bool) error {
	var ok []Item
	for _, it := range res.Items {
		if err := verify(it); err != nil {
			it.Why = err.Error()
			res.Failed = append(res.Failed, it)
			continue
		}
		ok = append(ok, it)
	}
	var b strings.Builder
	oneLine := strings.NewReplacer("\n", " ", "\r", " ")
	if ps {
		b.WriteString("# onefold dedupe: copies verified identical to the kept one; review before running\n$ErrorActionPreference = 'Stop'\n")
		for _, it := range ok {
			fmt.Fprintf(&b, "Remove-Item -LiteralPath '%s'  # = %s\n", strings.ReplaceAll(it.Extra, "'", "''"), oneLine.Replace(it.Keep))
		}
	} else {
		b.WriteString("#!/bin/sh\n# onefold dedupe: copies verified identical to the kept one; review before running\nset -e\n")
		for _, it := range ok {
			fmt.Fprintf(&b, "rm -- '%s'  # = %s\n", strings.ReplaceAll(it.Extra, "'", `'\''`), oneLine.Replace(it.Keep))
		}
	}
	res.Done = len(ok)
	for _, it := range ok {
		res.DoneBytes += it.Size
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// journal: fsync'ed JSONL, one event per line.
type journal struct {
	mu   sync.Mutex
	f    *os.File
	path string
}

type entry struct {
	Op      string   `json:"op"` // start, trash, replace, done, fail, end
	T       string   `json:"t,omitempty"`
	Action  string   `json:"action,omitempty"`
	Roots   []string `json:"roots,omitempty"`
	Keep    string   `json:"keep,omitempty"`
	Extra   string   `json:"extra,omitempty"`
	Content string   `json:"content,omitempty"`
	Old     string   `json:"old,omitempty"`
	Tmp     string   `json:"tmp,omitempty"`
	Why     string   `json:"why,omitempty"`
	Items   int      `json:"items,omitempty"`
	Done    int      `json:"done,omitempty"`
	Failed  int      `json:"failed,omitempty"`
}

func openJournal(o Options, res *Result) (*journal, error) {
	dir := o.JournalDir
	if dir == "" {
		d, err := statedir.Dir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(d, "dedupe")
	}
	if err := os.MkdirAll(dir, statedir.DirMode); err != nil {
		return nil, err
	}
	p := filepath.Join(dir, "dedupe-"+time.Now().Format("20060102-150405.000000000")+".jsonl")
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, statedir.FileMode)
	if err != nil {
		return nil, err
	}
	j := &journal{f: f, path: p}
	if err := j.write(entry{Op: "start", Action: res.Action, Roots: res.Roots, Keep: res.Keep, Items: len(res.Items)}); err != nil {
		f.Close()
		return nil, err
	}
	return j, nil
}

func (j *journal) write(e entry) error {
	e.T = time.Now().Format(time.RFC3339Nano)
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if _, err := j.f.Write(append(b, '\n')); err != nil {
		return err
	}
	return j.f.Sync()
}

func (j *journal) close() { j.f.Close() }

// Resume finishes the replacements an interrupted run left half done, from
// its journal: where the original still waits under its `.onefold-dd` name,
// it is removed when the name holds the new copy and both read as the
// content, or put back when the name is empty; anything else is reported and
// left as it is. Temporary files of the run are removed.
func Resume(journalPath string) (finished, restored int, problems []string, err error) {
	b, err := os.ReadFile(journalPath)
	if err != nil {
		return 0, 0, nil, err
	}
	pending := map[string]entry{}
	var order []string
	for _, line := range strings.Split(string(b), "\n") {
		var e entry
		if line == "" || json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		switch e.Op {
		case "replace":
			if _, ok := pending[e.Extra]; !ok {
				order = append(order, e.Extra)
			}
			pending[e.Extra] = e
		case "done":
			delete(pending, e.Extra)
		}
	}
	for _, extra := range order {
		e, ok := pending[extra]
		if !ok {
			continue
		}
		os.Remove(e.Tmp) // ours: never the user's file
		if _, err := os.Lstat(e.Old); err != nil {
			continue // never set aside, or already finished
		}
		if _, err := os.Lstat(e.Extra); os.IsNotExist(err) {
			if err := fsx.RenameNoReplace(e.Old, e.Extra); err != nil {
				problems = append(problems, fmt.Sprintf("%s: put back failed: %v", e.Extra, err))
				continue
			}
			restored++
			continue
		}
		oh, err1 := sparsehash.FullFile(e.Old)
		nh, err2 := sparsehash.FullFile(e.Extra)
		if err1 != nil || err2 != nil || oh != e.Content || nh != e.Content {
			problems = append(problems, fmt.Sprintf("%s: the name and its original %s differ: both left as they are", e.Extra, e.Old))
			continue
		}
		if err := os.Remove(e.Old); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", e.Old, err))
			continue
		}
		finished++
	}
	return finished, restored, problems, nil
}

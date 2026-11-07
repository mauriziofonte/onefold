// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mauriziofonte/onefold/internal/fsx"
	"github.com/mauriziofonte/onefold/internal/merge"
	"github.com/mauriziofonte/onefold/internal/runctl"
	"github.com/mauriziofonte/onefold/internal/sparsehash"
	"github.com/mauriziofonte/onefold/internal/statedir"
)

// The two sides of a backup: the tree it was made from and the copy.
const (
	SideTree = "tree"
	SideCopy = "copy"
)

// DamagedDir is where a restore keeps the files it replaced, inside the
// state folder of their side: <side>/.onefold/damaged/<run>/<path>.
const DamagedDir = "damaged"

// Repair is one file to restore from the side that still holds the content
// the manifests recorded to the side that lost it.
type Repair struct {
	Rel     string `json:"rel"`
	To      string `json:"to"`  // the side repaired: tree or copy
	Why     string `json:"why"` // changed, missing, unreadable
	Size    int64  `json:"size"`
	Content string `json:"content"`
	Aside   string `json:"aside,omitempty"` // execute: where the damaged file was kept
	Error   string `json:"error,omitempty"`
}

// Health compares a tree and its copy with the manifests of the copy: the
// content each file had when the backup read it.
type Health struct {
	Manifests int      `json:"manifests"`
	Tree      *Audit   `json:"tree"`
	Copy      *Audit   `json:"copy"`
	Repairs   []Repair `json:"repairs"`
	// Lost: damaged or missing on both sides, nothing to restore from.
	Lost []string `json:"lost,omitempty"`
	// Moved: missing under its name on one side, the same content under a
	// path the manifests do not name (renamed after the backup): not restored.
	Moved map[string]string `json:"moved,omitempty"`
}

// Clean reports whether both sides hold every file as recorded.
func (h *Health) Clean() bool {
	return len(h.Repairs)+len(h.Lost)+len(h.Moved) == 0
}

// Compare reads the tree and the copy again against the manifests of the
// copy and lists what a restore would do. Read-only.
func Compare(tree, copyDir string, workers int, gate *runctl.Gate) (*Health, error) {
	tree, copyDir = filepath.Clean(tree), filepath.Clean(copyDir)
	if within(copyDir, tree) || within(tree, copyDir) {
		return nil, fmt.Errorf("the copy and the tree must not contain each other")
	}
	want, n, err := loadManifests(copyDir)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, fmt.Errorf("%s: no backup manifest in %s", copyDir, StateDir)
	}
	h := &Health{Manifests: n}
	if h.Tree, err = audit(tree, want, workers, gate); err != nil {
		return nil, err
	}
	if h.Copy, err = audit(copyDir, want, workers, gate); err != nil {
		return nil, err
	}
	h.Tree.Manifests, h.Copy.Manifests = n, n
	bad := func(a *Audit) map[string]string {
		m := map[string]string{}
		for _, r := range a.Changed {
			m[r] = "changed"
		}
		for _, r := range a.Missing {
			m[r] = "missing"
		}
		for _, r := range a.Failed {
			m[r] = "unreadable"
		}
		return m
	}
	bt, bc := bad(h.Tree), bad(h.Copy)
	moved := map[string]string{}
	findMoved(tree, h.Tree, want, moved)
	findMoved(copyDir, h.Copy, want, moved)
	for rel, why := range bt {
		switch {
		case moved[rel] != "":
		case bc[rel] != "":
			h.Lost = append(h.Lost, rel)
		default:
			h.Repairs = append(h.Repairs, Repair{Rel: rel, To: SideTree, Why: why, Size: want[rel].Size, Content: want[rel].Content})
		}
	}
	for rel, why := range bc {
		if bt[rel] == "" && moved[rel] == "" {
			h.Repairs = append(h.Repairs, Repair{Rel: rel, To: SideCopy, Why: why, Size: want[rel].Size, Content: want[rel].Content})
		}
	}
	if len(moved) > 0 {
		h.Moved = moved
	}
	sort.Strings(h.Lost)
	sort.Slice(h.Repairs, func(i, j int) bool {
		if h.Repairs[i].Rel != h.Repairs[j].Rel {
			return h.Repairs[i].Rel < h.Repairs[j].Rel
		}
		return h.Repairs[i].To < h.Repairs[j].To
	})
	return h, nil
}

// findMoved: a file missing under its name whose content is now under an
// untracked path of the same side (same size, then the same full hash) was
// renamed, not lost. The renamed copy leaves the untracked list.
func findMoved(root string, a *Audit, want map[string]Entry, moved map[string]string) {
	if len(a.Missing) == 0 || len(a.Untracked) == 0 {
		return
	}
	bySize := map[int64][]string{}
	for _, rel := range a.Missing {
		bySize[want[rel].Size] = append(bySize[want[rel].Size], rel)
	}
	var keep []string
	for _, u := range a.Untracked {
		p := filepath.Join(root, filepath.FromSlash(u))
		fi, err := os.Stat(p)
		if err != nil || len(bySize[fi.Size()]) == 0 {
			keep = append(keep, u)
			continue
		}
		cands := bySize[fi.Size()]
		h, err := sparsehash.FullFile(p)
		found := false
		for i, rel := range cands {
			if err == nil && want[rel].Content == h && moved[rel] == "" {
				moved[rel] = u
				bySize[fi.Size()] = append(cands[:i:i], cands[i+1:]...)
				found = true
				break
			}
		}
		if !found {
			keep = append(keep, u)
		}
	}
	a.Untracked = keep
}

// RestoreOptions of Restore.
type RestoreOptions struct {
	Execute bool
	// Expect is the typed count of repairs an execute confirms: it must
	// equal the repairs found now, else nothing is done.
	Expect     int
	Workers    int
	Gate       *runctl.Gate
	JournalDir string // "" = <state dir>/restore
}

// RestoreResult of Restore.
type RestoreResult struct {
	*Health
	Done    int      `json:"done"`
	Healthy int      `json:"healthy"` // already holding the content when read again: left alone
	Failed  []Repair `json:"failed,omitempty"`
	Journal string   `json:"journal,omitempty"`
	Stopped bool     `json:"stopped,omitempty"`
}

// Restore puts the recorded content back where a side lost it, from the
// other side. Dry run unless Execute; an execute needs Expect, writes its
// journal (fsync'ed JSONL) before the first change, and right before each
// repair reads the healthy file again in full (it must still hold the
// recorded content) and the damaged one (still damaged, else it is left
// alone). Nothing is deleted: a replaced file steps aside into
// <side>/.onefold/damaged/<run>/<path>, on the same volume. The healthy
// bytes are copied next to the name and verified first, so the name is
// never without a whole file. ResumeRestore finishes an interrupted run.
func Restore(tree, copyDir string, o RestoreOptions) (*RestoreResult, error) {
	tree, copyDir = filepath.Clean(tree), filepath.Clean(copyDir)
	h, err := Compare(tree, copyDir, o.Workers, o.Gate)
	if err != nil {
		return nil, err
	}
	res := &RestoreResult{Health: h}
	if !o.Execute || len(h.Repairs) == 0 {
		return res, nil
	}
	if o.Expect != len(h.Repairs) {
		return res, fmt.Errorf("restore: %d files to restore, confirmation said %d: the sides changed since the preview; preview again", len(h.Repairs), o.Expect)
	}
	j, err := openRestoreJournal(o.JournalDir, tree, copyDir, len(h.Repairs))
	if err != nil {
		return res, fmt.Errorf("journal: %w", err)
	}
	defer j.f.Close()
	res.Journal = j.path
	run := time.Now().Format("20060102-150405")
	buf := make([]byte, 4<<20)
	for i := range h.Repairs {
		if !o.Gate.Wait() {
			res.Stopped = true
			break
		}
		rp := &h.Repairs[i]
		from, to := copyDir, tree
		if rp.To == SideCopy {
			from, to = tree, copyDir
		}
		healthy, err := restoreOne(rp, from, to, run, j, buf)
		switch {
		case err != nil:
			rp.Error = err.Error()
			res.Failed = append(res.Failed, *rp)
			j.write(rentry{Op: "fail", Rel: rp.Rel, To: rp.To, Why: rp.Error})
		case healthy:
			res.Healthy++
		default:
			res.Done++
		}
	}
	j.write(rentry{Op: "end", Done: res.Done, Failed: len(res.Failed)})
	return res, nil
}

// restoreOne repairs one file of the side rooted at to from the side rooted
// at from. healthy: the file read as the recorded content again, untouched.
func restoreOne(rp *Repair, from, to, run string, j *rjournal, buf []byte) (healthy bool, err error) {
	src := filepath.Join(from, filepath.FromSlash(rp.Rel))
	dst := filepath.Join(to, filepath.FromSlash(rp.Rel))
	if h, err := sparsehash.FullFile(src); err != nil || h != rp.Content {
		return false, fmt.Errorf("the healthy side no longer reads as recorded (%v): NOT touched", err)
	}
	fi, lerr := os.Lstat(dst)
	switch {
	case lerr == nil && !fi.Mode().IsRegular():
		return false, errors.New("not a regular file: NOT touched")
	case lerr == nil:
		if h, err := sparsehash.FullFile(dst); err == nil && h == rp.Content {
			return true, nil
		}
	case !errors.Is(lerr, fs.ErrNotExist):
		return false, lerr
	}
	stage := fsx.TempFor(dst, false)
	os.Remove(stage) // ours: a temp left by an interrupted run
	h, err := merge.CopyVerified(src, stage, rp.Size, buf)
	if err == nil && h != rp.Content {
		err = errors.New("the healthy side changed while it was copied")
	}
	if err == nil {
		err = fsx.SyncFile(stage)
	}
	if err != nil {
		os.Remove(stage)
		return false, fmt.Errorf("%v: NOT touched", err)
	}
	if lerr != nil { // missing: the name only gains a file
		if err := j.write(rentry{Op: "add", Rel: rp.Rel, Path: dst, Tmp: stage, Content: rp.Content}); err != nil {
			os.Remove(stage)
			return false, fmt.Errorf("journal not writable (%v): NOT touched", err)
		}
		if err := fsx.RenameNoReplace(stage, dst); err != nil {
			os.Remove(stage)
			return false, err
		}
		if err := fsx.SyncDir(filepath.Dir(dst)); err != nil {
			return false, fmt.Errorf("restored; the folder could not be flushed: %v", err)
		}
		j.write(rentry{Op: "done", Path: dst})
		return false, nil
	}
	aside := filepath.Join(to, StateDir, DamagedDir, run, filepath.FromSlash(rp.Rel))
	if err := os.MkdirAll(filepath.Dir(aside), 0o755); err != nil {
		os.Remove(stage)
		return false, fmt.Errorf("%v: NOT touched", err)
	}
	if err := j.write(rentry{Op: "replace", Rel: rp.Rel, Path: dst, Tmp: stage, Aside: aside, Content: rp.Content}); err != nil {
		os.Remove(stage)
		return false, fmt.Errorf("journal not writable (%v): NOT touched", err)
	}
	if err := fsx.RenameNoReplace(dst, aside); err != nil {
		os.Remove(stage)
		return false, fmt.Errorf("the damaged file could not step aside (%v): NOT touched", err)
	}
	if err := fsx.RenameNoReplace(stage, dst); err != nil {
		if berr := fsx.RenameNoReplace(aside, dst); berr != nil {
			return false, fmt.Errorf("%v; the damaged file is %s (put it back by hand)", err, aside)
		}
		os.Remove(stage)
		return false, err
	}
	rp.Aside = aside
	if err := fsx.SyncDir(filepath.Dir(dst)); err != nil {
		return false, fmt.Errorf("restored; the folder could not be flushed: %v", err)
	}
	_ = fsx.SyncDir(filepath.Dir(aside))
	j.write(rentry{Op: "done", Path: dst})
	return false, nil
}

// rjournal: fsync'ed JSONL, one event per line.
type rjournal struct {
	mu   sync.Mutex
	f    *os.File
	path string
}

type rentry struct {
	Op      string `json:"op"` // start, add, replace, done, fail, end
	T       string `json:"t,omitempty"`
	Tree    string `json:"tree,omitempty"`
	Copy    string `json:"copy,omitempty"`
	Rel     string `json:"rel,omitempty"`
	To      string `json:"to,omitempty"`
	Path    string `json:"path,omitempty"`
	Tmp     string `json:"tmp,omitempty"`
	Aside   string `json:"aside,omitempty"`
	Content string `json:"content,omitempty"`
	Why     string `json:"why,omitempty"`
	Items   int    `json:"items,omitempty"`
	Done    int    `json:"done,omitempty"`
	Failed  int    `json:"failed,omitempty"`
}

func openRestoreJournal(dir, tree, copyDir string, n int) (*rjournal, error) {
	if dir == "" {
		d, err := statedir.Dir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(d, "restore")
	}
	if err := os.MkdirAll(dir, statedir.DirMode); err != nil {
		return nil, err
	}
	p := filepath.Join(dir, "restore-"+time.Now().Format("20060102-150405.000000000")+".jsonl")
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, statedir.FileMode)
	if err != nil {
		return nil, err
	}
	j := &rjournal{f: f, path: p}
	if err := j.write(rentry{Op: "start", Tree: tree, Copy: copyDir, Items: n}); err != nil {
		f.Close()
		return nil, err
	}
	return j, nil
}

func (j *rjournal) write(e rentry) error {
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

// ResumeRestore finishes the repairs an interrupted restore left half done,
// from its journal. Where the name holds the recorded content the repair is
// finished; where the name is empty, the verified healthy bytes take it, or
// else the damaged file is put back; anything else is reported and left as
// it is. Temporary files of the run are removed.
func ResumeRestore(journalPath string) (finished, putBack int, problems []string, err error) {
	b, err := os.ReadFile(journalPath)
	if err != nil {
		return 0, 0, nil, err
	}
	pending := map[string]rentry{}
	var order []string
	for _, line := range strings.Split(string(b), "\n") {
		var e rentry
		if line == "" || json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		switch e.Op {
		case "add", "replace":
			if _, ok := pending[e.Path]; !ok {
				order = append(order, e.Path)
			}
			pending[e.Path] = e
		case "done":
			delete(pending, e.Path)
		}
	}
	reads := func(p, content string) bool {
		h, err := sparsehash.FullFile(p)
		return err == nil && h == content
	}
	for _, path := range order {
		e, ok := pending[path]
		if !ok {
			continue
		}
		if _, err := os.Lstat(e.Path); err == nil {
			if reads(e.Path, e.Content) {
				os.Remove(e.Tmp)
				finished++
			} else if e.Op == "replace" {
				os.Remove(e.Tmp)
				problems = append(problems, fmt.Sprintf("%s: not replaced (the damaged file is still there): run the restore again", e.Path))
			} else {
				os.Remove(e.Tmp)
				problems = append(problems, fmt.Sprintf("%s: another file took the name: left as it is", e.Path))
			}
			continue
		}
		if reads(e.Tmp, e.Content) && fsx.RenameNoReplace(e.Tmp, e.Path) == nil {
			finished++
			continue
		}
		os.Remove(e.Tmp)
		if e.Op == "replace" {
			if _, err := os.Lstat(e.Aside); err == nil {
				if err := fsx.RenameNoReplace(e.Aside, e.Path); err != nil {
					problems = append(problems, fmt.Sprintf("%s: put back failed: %v (the damaged file is %s)", e.Path, err, e.Aside))
					continue
				}
				putBack++
				continue
			}
		}
		problems = append(problems, fmt.Sprintf("%s: nothing to finish it with: run the restore again", e.Path))
	}
	return finished, putBack, problems, nil
}

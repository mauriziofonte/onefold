// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package rearrange reorganizes a tree IN PLACE according to a naming spec:
// same-volume renames only (bytes and mtime untouched), confirmed duplicates
// optionally deleted, everything journaled so an interrupted run can be resumed
// and a completed one undone (moves only: deletions are final).
//
// Pinned by rearrange_test.go.
package rearrange

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charlievieth/fastwalk"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/fsx"
	"github.com/mauriziofonte/onefold/internal/merge"
	"github.com/mauriziofonte/onefold/internal/names"
	"github.com/mauriziofonte/onefold/internal/naming"
	"github.com/mauriziofonte/onefold/internal/perf"
	"github.com/mauriziofonte/onefold/internal/plan"
	"github.com/mauriziofonte/onefold/internal/report"
	"github.com/mauriziofonte/onefold/internal/runctl"
	"github.com/mauriziofonte/onefold/internal/sparsehash"
)

// MoveSuffix marks a file between the two passes. NEVER auto-deleted: it is
// user data in flight; only -resume / -undo act on it.
const MoveSuffix = ".onefold-mv"

// Options configure a rearrange.
type Options struct {
	Root          string
	Naming        string
	Catalog       catalog.Options
	Execute       bool
	DeleteDupes   bool // delete confirmed duplicates (else report and rearrange them too)
	KeepEmptyDirs bool
	NoCache       bool
	Workers       int
	Log           io.Writer     // status lines; nil = silent (no progress either)
	Heartbeat     time.Duration // non-TTY progress interval (0 = default)

	// Gate pauses or stops the run between units of work (GUI); nil = never.
	// A stop during execute leaves the rest journaled: Recover completes it.
	Gate *runctl.Gate
	// BeforeExecute is called with the plan before the journal is written; an
	// error aborts with nothing touched (GUI: executed plan == confirmed plan).
	BeforeExecute func(*Result) error
	// StateCache is an extra catalog cache outside the tree (GUI job state):
	// read at start, written after a preview and after an execute.
	StateCache string
	// Records re-plans a previous preview of the same root (GUI): its
	// records replace the walk; the user exclusions of Catalog are applied
	// (Excluded lists the ones they leave out); the state cache is not
	// rewritten.
	Records []catalog.Record
	// Packages of that preview (re-plan): recounted on the records kept.
	Packages []catalog.Package
	// OnJournal is called with the journal path right after it is written and
	// before any change (GUI: a power loss mid-run must still find it).
	OnJournal func(path string)
}

// Move is one planned rename (relative, slash-separated paths).
type Move struct {
	ID   int    `json:"id"`
	Src  string `json:"src"`
	Tmp  string `json:"tmp"`
	Dst  string `json:"dst"`
	Size int64  `json:"size"`
	Hash string `json:"hash"` // sparse digest: recovery only moves a file that IS this one
}

// Delete is one planned duplicate deletion.
type Delete struct {
	ID      int    `json:"id"`
	Path    string `json:"path"`
	Keeper  string `json:"keeper"`
	Content string `json:"content"` // full-content XXH128 both files must still have
	Size    int64  `json:"size"`
}

// Result is the plan + outcome.
type Result struct {
	Root        string   `json:"root"`
	Naming      string   `json:"naming"`
	Files       int      `json:"files"`
	Stay        int      `json:"stay"`
	Moves       []Move   `json:"moves"`
	Deletes     []Delete `json:"deletes"`
	DupGroups   int      `json:"duplicateGroups"`
	DeleteBytes int64    `json:"deleteBytes"`
	Skipped     int      `json:"skippedByFilters"`
	Journal     string   `json:"journal,omitempty"`
	// Excluded lists the files and directories the filters left out (they
	// stay where they are), with their reason, sorted by path.
	Excluded []catalog.Skip `json:"excluded,omitempty"`
	// Packages: folders that look like driver or installer packages.
	Packages []catalog.Package `json:"packages,omitempty"`

	Verify      plan.VerifyStats `json:"verify"`
	Moved       int              `json:"moved"`
	Deleted     int              `json:"deleted"`
	DirsRemoved int              `json:"emptyDirsRemoved"`
	Failed      []string         `json:"failed,omitempty"`
	Stopped     bool             `json:"stopped,omitempty"` // gate stopped: the journal holds the rest

	coverage, sample int
	keepEmpty        bool
}

func (o *Options) logf(f string, a ...any) {
	if o.Log != nil {
		fmt.Fprintf(o.Log, f, a...)
	}
}

// progress returns a live reporter, or nil when the caller wants no output.
func (o *Options) progress(label string, unit report.Unit, growing bool, total int64) *report.Reporter {
	if o.Log == nil {
		return nil
	}
	return report.New(report.Options{Label: label, Unit: unit, Growing: growing, Total: total, Heartbeat: o.Heartbeat})
}

// Run plans (and with Execute performs) the rearrangement.
func Run(o Options) (*Result, error) {
	if o.Catalog.KeepEmpty || o.Catalog.KeepDotfiles {
		// empty files are identified by their name: two of them in different
		// folders would be taken as duplicates and one deleted
		return nil, errors.New("rearrange never takes dotfiles or empty files (a merge option)")
	}
	if o.Workers <= 0 {
		o.Workers = runtime.NumCPU()
	}
	root, err := filepath.Abs(o.Root)
	if err != nil {
		return nil, err
	}
	root = filepath.Clean(root)
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("root %s is not a directory", root)
	}
	if o.Naming == "" {
		o.Naming = "ext-year"
	}
	nm, err := naming.Resolve(namingOrDefault(o.Naming))
	if err != nil {
		return nil, err
	}
	res := &Result{Root: root, Naming: nm.Spec(), coverage: o.Catalog.Coverage, sample: o.Catalog.Sample, keepEmpty: o.KeepEmptyDirs}

	// Refuse to run over an interrupted rearrange.
	if pending := findPending(root); len(pending) > 0 {
		return nil, fmt.Errorf("%d file(s) are mid-move (*%s) from an interrupted run: use -resume <journal> (in %s) first, e.g. %s",
			len(pending), MoveSuffix, filepath.Join(root, merge.CacheDir), pending[0])
	}

	cachePath := filepath.Join(root, merge.CacheDir, "merge-cache.jsonl")
	cache := map[string]catalog.Record{}
	if !o.NoCache {
		if m, err := catalog.LoadManifestByPath(cachePath); err == nil {
			cache = m
		}
	}
	if o.StateCache != "" {
		if m, err := catalog.LoadManifestByPath(o.StateCache); err == nil {
			for k, v := range m {
				if _, ok := cache[k]; !ok {
					cache[k] = v
				}
			}
		}
	}

	// 1. catalog (filters decide what is IN SCOPE; everything else stays put)
	t0 := time.Now()
	co := o.Catalog
	co.SkipDirs = append(append([]string(nil), co.SkipDirs...), filepath.Join(root, merge.CacheDir))
	co.Reuse, co.Host, co.Gate = cache, "root", o.Gate
	co.OnSkip = func(sk catalog.Skip) { res.Excluded = append(res.Excluded, sk) }
	o.logf("cataloging %s…\n", root)
	pc := o.progress("catalog", report.Items, true, 0)
	if pc != nil {
		co.Progress = pc
	}
	var recs []catalog.Record
	var sum *catalog.Summary
	if o.Records != nil {
		sum = &catalog.Summary{}
		for _, r := range o.Records {
			if rule := o.Catalog.UserExclusion(root, r.Path); rule != "" {
				res.Excluded = append(res.Excluded, catalog.Skip{Path: r.Path, Rule: rule})
				continue
			}
			recs = append(recs, r)
		}
		res.Packages = catalog.RecountPackages(o.Packages, recs)
	} else {
		recs, sum, err = catalog.Collect([]string{root}, co)
		if sum != nil {
			res.Packages = sum.Packages
		}
	}
	if pc != nil {
		pc.Stop()
	}
	if err != nil {
		return nil, err
	}
	res.Files, res.Skipped = len(recs), sum.Skipped
	catalog.SortSkips(res.Excluded)
	perf.PhaseDone("catalog", t0, co.Workers, int64(len(recs)), 0)

	// 2. confirm duplicate groups on the full content
	t1 := time.Now()
	var vp catalog.Progress
	pv := o.progress("verify", report.Bytes, true, 0)
	if pv != nil {
		vp = pv
	}
	recs, res.Verify = plan.VerifyGroupsWith(recs, o.Workers, vp, o.Gate)
	if pv != nil {
		pv.Stop()
	}
	perf.PhaseDone("verify-full", t1, o.Workers, int64(res.Verify.Groups), res.Verify.Bytes)
	if o.Gate.Stopped() {
		res.Stopped = true
		return res, runctl.ErrStopped
	}

	// 3. targets, keepers, deletions
	t2 := time.Now()
	rel := func(p string) string {
		r, _ := filepath.Rel(root, p)
		return filepath.ToSlash(r)
	}
	known := make(map[string]bool, len(recs))
	for _, r := range recs {
		known[names.FoldKey(r.Path)] = true
	}
	exists := func(p string) bool { return known[names.FoldKey(p)] }
	targets := make([]string, len(recs))
	for i, r := range recs {
		targets[i] = nm.Target(naming.VarsOfCtx(r, exists))
	}
	groups := map[string][]int{}
	for i, r := range recs {
		groups[r.Hash] = append(groups[r.Hash], i)
	}
	drop := make([]bool, len(recs))
	keeperOf := map[int]int{}
	for _, idxs := range groups {
		if len(idxs) < 2 {
			continue
		}
		res.DupGroups++
		sort.SliceStable(idxs, func(a, b int) bool {
			ra, rb := recs[idxs[a]], recs[idxs[b]]
			ha, hb := rel(ra.Path) == targets[idxs[a]], rel(rb.Path) == targets[idxs[b]]
			if ha != hb {
				return ha
			}
			ca, cb := !hasMarkers(ra.Path), !hasMarkers(rb.Path)
			if ca != cb {
				return ca
			}
			if ra.MTimeNs != rb.MTimeNs && ra.MTimeNs != 0 && rb.MTimeNs != 0 {
				return ra.MTimeNs < rb.MTimeNs
			}
			if len(ra.Path) != len(rb.Path) {
				return len(ra.Path) < len(rb.Path)
			}
			return ra.Path < rb.Path
		})
		if o.DeleteDupes {
			for _, i := range idxs[1:] {
				drop[i], keeperOf[i] = true, idxs[0]
			}
		}
	}

	// names not available: every path in the tree that is not an item
	// (out-of-scope files, directories, files to delete, .onefold)
	all, err := listAll(root)
	if err != nil {
		return nil, err
	}
	isItem := map[string]bool{}
	for i, r := range recs {
		if !drop[i] {
			isItem[names.FoldKey(rel(r.Path))] = true
		}
	}
	taken := map[string]bool{}
	for k := range all {
		if !isItem[k] {
			taken[k] = true
		}
	}
	var idx []int
	var items []naming.Item
	for i, r := range recs {
		if drop[i] {
			continue
		}
		idx = append(idx, i)
		items = append(items, naming.Item{Current: rel(r.Path), Target: targets[i], Tie: r.Hash + "\x00" + r.Path})
	}
	final := naming.Assign(items, taken)
	id := 0
	for k, i := range idx {
		cur := rel(recs[i].Path)
		if final[k] == cur || final[k] == "" {
			res.Stay++
			continue
		}
		id++
		h := recs[i].Hash
		if j := strings.IndexByte(h, '-'); j > 0 {
			h = h[:j] // verification suffix: keep the raw sparse digest
		}
		res.Moves = append(res.Moves, Move{ID: id, Src: cur, Dst: final[k], Tmp: final[k] + "." + strconv.Itoa(id) + MoveSuffix,
			Size: recs[i].Size, Hash: h})
	}
	for i := range recs {
		if !drop[i] {
			continue
		}
		id++
		k := keeperOf[i]
		res.Deletes = append(res.Deletes, Delete{ID: id, Path: rel(recs[i].Path), Keeper: rel(recs[k].Path),
			Content: recs[i].ContentHash, Size: recs[i].Size})
		res.DeleteBytes += recs[i].Size
	}
	sort.Slice(res.Moves, func(a, b int) bool { return res.Moves[a].Src < res.Moves[b].Src })
	perf.PhaseDone("plan", t2, 1, int64(len(res.Moves)), 0)
	if !o.Execute {
		if o.StateCache != "" && o.Records == nil {
			if err := merge.WriteCache(o.StateCache, recs); err != nil {
				o.logf("warning: state cache not written: %v\n", err)
			}
		}
		return res, nil
	}
	if o.BeforeExecute != nil {
		if err := o.BeforeExecute(res); err != nil {
			return res, err
		}
	}

	if len(res.Moves) == 0 && len(res.Deletes) == 0 {
		return res, nil
	}
	// 4. journal BEFORE touching anything
	jp, err := writeJournal(root, res)
	if err != nil {
		return res, fmt.Errorf("journal: %w", err)
	}
	res.Journal = jp
	if o.OnJournal != nil {
		o.OnJournal(jp)
	}

	t3 := time.Now()
	tick := func() {}
	if pe := o.progress("rearrange", report.Items, false, int64(len(res.Deletes)+2*len(res.Moves))); pe != nil {
		defer pe.Stop()
		tick = func() { pe.Advance(0) }
	}
	deleted := doDeletes(root, res, o.Workers, tick, o.Gate)
	movedTo := doMoves(root, res, o.Workers, tick, o.Gate)
	res.Stopped = o.Gate.Stopped()
	perf.PhaseDone("execute", t3, o.Workers, int64(res.Moved+res.Deleted), 0)

	if !o.KeepEmptyDirs {
		res.DirsRemoved = removeEmptyDirs(root, res)
	}

	// 5. cache with the new paths (moved) minus deleted files
	if !o.NoCache {
		var keep []catalog.Record
		for _, r := range recs {
			rp := rel(r.Path)
			if deleted[rp] {
				continue
			}
			if nd, ok := movedTo[rp]; ok {
				r.Path = filepath.Join(root, filepath.FromSlash(nd))
			}
			keep = append(keep, r)
		}
		if err := merge.WriteCache(cachePath, keep); err != nil {
			o.logf("warning: cache not written: %v\n", err)
		}
		if o.StateCache != "" {
			if err := merge.WriteCache(o.StateCache, keep); err != nil {
				o.logf("warning: state cache not written: %v\n", err)
			}
		}
	}
	return res, nil
}

func hasMarkers(p string) bool {
	b := filepath.Base(p)
	return names.HasMarkers(strings.TrimSuffix(b, filepath.Ext(b)))
}

// listAll returns every file and directory under root (relative, lowercased).
func listAll(root string) (map[string]bool, error) {
	m := map[string]bool{}
	var mu sync.Mutex
	conf := fastwalk.Config{NumWorkers: runtime.NumCPU()}
	err := fastwalk.Walk(&conf, root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == root {
			return nil
		}
		r, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		mu.Lock()
		m[names.FoldKey(filepath.ToSlash(r))] = true
		mu.Unlock()
		return nil
	})
	return m, err
}

func findPending(root string) []string {
	var out []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(d.Name(), MoveSuffix) {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// journal: line 1 = header, then one line per planned move / delete.
type header struct {
	Op       string `json:"op"`
	Root     string `json:"root"`
	Naming   string `json:"naming"`
	Time     string `json:"time"`
	Coverage int    `json:"coverage"` // sparse hash parameters used for the identity checks
	Sample   int    `json:"sample"`
	Blocks   int    `json:"blocks"`
	// KeepEmptyDirs of the run, so a resume cleans up the same way (absent in
	// journals written before it: false = remove, the default).
	KeepEmptyDirs bool `json:"keepEmptyDirs,omitempty"`
}

type entry struct {
	Op string `json:"op"` // move | delete
	Move
	Keeper  string `json:"keeper,omitempty"`
	Content string `json:"content,omitempty"`
}

func writeJournal(root string, res *Result) (string, error) {
	dir := filepath.Join(root, merge.CacheDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, "rearrange-"+time.Now().Format("20060102-150405.000000000")+".jsonl")
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	bw := bufio.NewWriter(f)
	enc := json.NewEncoder(bw)
	enc.Encode(header{Op: "header", Root: root, Naming: res.Naming, Time: time.Now().Format(time.RFC3339),
		Coverage: res.coverage, Sample: res.sample, Blocks: sparsehash.MaxBlocks, KeepEmptyDirs: res.keepEmpty})
	for _, d := range res.Deletes {
		enc.Encode(entry{Op: "delete", Move: Move{ID: d.ID, Src: d.Path}, Keeper: d.Keeper, Content: d.Content})
	}
	for _, m := range res.Moves {
		enc.Encode(entry{Op: "move", Move: m})
	}
	if err := bw.Flush(); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return "", err
	}
	return p, f.Close()
}

func abs(root, rel string) string { return filepath.Join(root, filepath.FromSlash(rel)) }

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

// exactName reports whether the directory holds an entry named exactly like p
// (case-sensitive), so NTFS case folding cannot make two names look equal.
func exactName(p string) bool {
	ents, err := os.ReadDir(filepath.Dir(p))
	if err != nil {
		return false
	}
	base := filepath.Base(p)
	for _, e := range ents {
		if e.Name() == base {
			return true
		}
	}
	return false
}

// isFile reports whether the file at p is the journaled one (size + sparse digest).
func isFile(p string, m Move, coverage, sample int) bool {
	if !exactName(p) {
		return false // case-insensitive FS: "X.PDF" must not match a request for "X.pdf"
	}
	fi, err := os.Stat(p)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() != m.Size {
		return false
	}
	if m.Hash == "" {
		return false
	}
	h, err := sparsehash.HashFile(p, coverage, sample)
	return err == nil && h.Hex == m.Hash
}

// doDeletes re-verifies each duplicate against its keeper on the FULL content
// right before deleting it.
func doDeletes(root string, res *Result, workers int, tick func(), gate *runctl.Gate) map[string]bool {
	deleted := map[string]bool{}
	var mu sync.Mutex
	run(workers, len(res.Deletes), func(i int) {
		if !gate.Wait() {
			tick()
			return // stopped: the journal keeps it, Recover finishes it
		}
		d := res.Deletes[i]
		err := func() error {
			ch, err := sparsehash.FullFile(abs(root, d.Path))
			if err != nil {
				return err
			}
			kh, err := sparsehash.FullFile(abs(root, d.Keeper))
			if err != nil {
				return fmt.Errorf("keeper unreadable: %w", err)
			}
			if ch != kh || (d.Content != "" && ch != d.Content) {
				return errors.New("content differs from its keeper: NOT deleted")
			}
			return os.Remove(abs(root, d.Path))
		}()
		mu.Lock()
		if err != nil {
			res.Failed = append(res.Failed, "delete "+d.Path+": "+err.Error())
		} else {
			res.Deleted++
			deleted[d.Path] = true
		}
		mu.Unlock()
		tick()
	})
	return deleted
}

// doMoves: pass 1 src -> tmp (frees every source), pass 2 tmp -> dst.
// Returns src -> dst of the completed moves. tick is called once per move per pass.
func doMoves(root string, res *Result, workers int, tick func(), gate *runctl.Gate) map[string]string {
	ok1 := make([]bool, len(res.Moves))
	var mu sync.Mutex
	fail := func(m Move, stage string, err error) {
		mu.Lock()
		res.Failed = append(res.Failed, stage+" "+m.Src+" -> "+m.Dst+": "+err.Error())
		mu.Unlock()
	}
	run(workers, len(res.Moves), func(i int) {
		defer tick()
		if !gate.Wait() {
			return // stopped before pass 1: still at its source, journaled
		}
		m := res.Moves[i]
		if err := os.MkdirAll(filepath.Dir(abs(root, m.Tmp)), 0o755); err != nil {
			fail(m, "mkdir", err)
			return
		}
		t := perf.Start()
		if err := fsx.RenameNoReplace(abs(root, m.Src), abs(root, m.Tmp)); err != nil {
			fail(m, "pass1", err)
			return
		}
		perf.End("rename:pass1", t, 0)
		ok1[i] = true
	})
	moved := map[string]string{}
	var dirs fsx.DirSet // folders whose entries changed: flushed once each at the end
	run(workers, len(res.Moves), func(i int) {
		defer tick()
		if !ok1[i] {
			return
		}
		m := res.Moves[i]
		dirs.Add(abs(root, m.Src))
		dirs.Add(abs(root, m.Dst))
		t := perf.Start()
		if err := fsx.RenameNoReplace(abs(root, m.Tmp), abs(root, m.Dst)); err != nil {
			// put it back where it was (its source is free: nobody targets a
			// source unless it moved away, and that move went through pass 1)
			if berr := fsx.RenameNoReplace(abs(root, m.Tmp), abs(root, m.Src)); berr != nil {
				fail(m, "pass2 (left as "+m.Tmp+")", err)
				return
			}
			fail(m, "pass2 (restored to source)", err)
			return
		}
		perf.End("rename:pass2", t, 0)
		mu.Lock()
		res.Moved++
		moved[m.Src] = m.Dst
		mu.Unlock()
	})
	// A rename lives in its folders' entries: without this flush a power cut
	// could undo some of them after the journal already says they ran. The
	// journal's resume still repairs that, but a finished run is final here.
	if err := dirs.SyncAll(); err != nil {
		res.Failed = append(res.Failed, "flushing the moved folders to disk: "+err.Error())
	}
	return moved
}

// removeEmptyDirs removes directories emptied by the run (sources' parents and
// their ancestors), deepest first; os.Remove never deletes a non-empty dir.
func removeEmptyDirs(root string, res *Result) int {
	cand := map[string]bool{}
	addParents := func(rel string) {
		for d := path.Dir(rel); d != "." && d != "/" && d != ""; d = path.Dir(d) {
			cand[d] = true
		}
	}
	for _, m := range res.Moves {
		addParents(m.Src)
	}
	for _, d := range res.Deletes {
		addParents(d.Path)
	}
	dirs := make([]string, 0, len(cand))
	for d := range cand {
		if d != merge.CacheDir && !strings.HasPrefix(d, merge.CacheDir+"/") {
			dirs = append(dirs, d)
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return strings.Count(dirs[i], "/") > strings.Count(dirs[j], "/") })
	n := 0
	for _, d := range dirs {
		if os.Remove(abs(root, d)) == nil {
			n++
		}
	}
	return n
}

func run(workers, n int, f func(i int)) {
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				f(i)
			}
		}()
	}
	for i := 0; i < n; i++ {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
}

// Recover completes (undo=false) or reverts (undo=true) a journal, deciding
// each step from where the files actually are. It is RecoverWith without a
// gate or progress; done counts moves and deletions.
func Recover(journal string, undo bool) (done, failed int, err error) {
	r, err := RecoverWith(journal, undo, RecoverOptions{})
	return r.Moved + r.Deleted, r.Failed, err
}

// RecoverOptions tune RecoverWith.
type RecoverOptions struct {
	Gate      *runctl.Gate  // pause/stop between steps (GUI); nil = never
	Log       io.Writer     // progress; nil = silent
	Heartbeat time.Duration // non-TTY progress interval
}

// RecoverResult is the outcome of RecoverWith.
type RecoverResult struct {
	Root        string   `json:"root"`
	Moved       int      `json:"moved"`
	Deleted     int      `json:"deleted"`
	Failed      int      `json:"failed"`
	Errors      []string `json:"errors,omitempty"`
	DirsRemoved int      `json:"emptyDirsRemoved"`
	Stopped     bool     `json:"stopped,omitempty"`
}

// RecoverWith completes (undo=false) or reverts (undo=true) a journal.
// Forward: pending duplicate deletions are finished first (each one re-checked
// on the full content against its keeper, wherever the keeper is now), then
// every move is completed, then folders emptied by the run are removed unless
// the run kept them. Undo: moves only, deletions are final.
func RecoverWith(journal string, undo bool, o RecoverOptions) (RecoverResult, error) {
	var out RecoverResult
	f, err := os.Open(journal)
	if err != nil {
		return out, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	var root string
	var hdr header
	var moves []Move
	var dels []entry
	for sc.Scan() {
		var e entry
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		switch e.Op {
		case "header":
			json.Unmarshal(sc.Bytes(), &hdr)
			root = hdr.Root
		case "move":
			moves = append(moves, e.Move)
		case "delete":
			dels = append(dels, e)
		}
	}
	if root == "" {
		return out, errors.New("journal has no header")
	}
	out.Root = root
	if hdr.Blocks > 0 {
		prev := sparsehash.MaxBlocks
		sparsehash.MaxBlocks = hdr.Blocks
		defer func() { sparsehash.MaxBlocks = prev }()
	}
	var rep *report.Reporter
	if o.Log != nil {
		total := int64(len(moves))
		if !undo {
			total += int64(len(dels))
		}
		label := "resume"
		if undo {
			label = "undo"
		}
		rep = report.New(report.Options{Label: label, Unit: report.Items, Total: total, Heartbeat: o.Heartbeat})
		defer rep.Stop()
	}
	tick := func() {
		if rep != nil {
			rep.Advance(0)
		}
	}
	fail := func(what string, err error) {
		out.Failed++
		out.Errors = append(out.Errors, what+": "+err.Error())
		if rep != nil {
			rep.ErrorAt(what, err)
		}
	}
	idOK := func(p string, m Move) bool { return isFile(p, m, hdr.Coverage, hdr.Sample) }

	// 1. pending deletions (forward only)
	if !undo {
		for _, d := range dels {
			if !o.Gate.Wait() {
				out.Stopped = true
				break
			}
			p := abs(root, d.Src)
			if !exactName(p) {
				tick()
				continue // already deleted
			}
			keeper := abs(root, d.Keeper)
			if !exactName(keeper) {
				keeper = ""
				for _, m := range moves {
					if m.Src == d.Keeper {
						for _, c := range []string{abs(root, m.Tmp), abs(root, m.Dst)} {
							if exactName(c) && idOK(c, m) {
								keeper = c
							}
						}
					}
				}
			}
			if keeper == "" {
				fail("delete "+d.Src, errors.New("keeper not found: NOT deleted"))
				tick()
				continue
			}
			ch, err1 := sparsehash.FullFile(p)
			kh, err2 := sparsehash.FullFile(keeper)
			switch {
			case err1 != nil:
				fail("delete "+d.Src, err1)
			case err2 != nil:
				fail("delete "+d.Src, fmt.Errorf("keeper unreadable: %w", err2))
			case ch != kh || (d.Content != "" && ch != d.Content):
				fail("delete "+d.Src, errors.New("content differs from its keeper: NOT deleted"))
			default:
				if err := os.Remove(p); err != nil {
					fail("delete "+d.Src, err)
				} else {
					out.Deleted++
				}
			}
			tick()
		}
	}

	// 2. moves, two passes like the forward run: park every file that is not
	// where it must end up at its journal temp name (frees swapped/cyclic
	// paths), then publish temps to their wanted path. State is read from the
	// filesystem. A stop only prevents new parking; parked files are published.
	type step struct {
		tmp, want string
		m         Move
	}
	var steps []step
	for _, m := range moves {
		src, tmp, dst := abs(root, m.Src), abs(root, m.Tmp), abs(root, m.Dst)
		want, other := dst, src
		if undo {
			want, other = src, dst
		}
		switch {
		case exists(tmp):
			steps = append(steps, step{tmp, want, m})
		case !out.Stopped && idOK(other, m) && !idOK(want, m):
			// the file at "other" must BE this one (a swap/cycle may have put
			// another file there)
			if !o.Gate.Wait() {
				out.Stopped = true
				continue
			}
			// the temp lives next to the destination, whose folder may not exist yet
			if err := os.MkdirAll(filepath.Dir(tmp), 0o755); err != nil {
				fail("move "+m.Src, err)
				tick()
				continue
			}
			if err := fsx.RenameNoReplace(other, tmp); err != nil {
				fail("move "+m.Src, err)
				tick()
				continue
			}
			steps = append(steps, step{tmp, want, m})
		default:
			tick()
		}
	}
	var dirs fsx.DirSet
	for _, st := range steps {
		dirs.Add(abs(root, st.m.Src))
		dirs.Add(abs(root, st.m.Dst))
		if err := os.MkdirAll(filepath.Dir(st.want), 0o755); err != nil {
			fail("move "+st.m.Src, err)
			tick()
			continue
		}
		if err := fsx.RenameNoReplace(st.tmp, st.want); err != nil {
			fail("move "+st.m.Src, err)
			tick()
			continue
		}
		out.Moved++
		tick()
	}
	if err := dirs.SyncAll(); err != nil {
		fail("flush the moved folders", err)
	}

	// 3. folders emptied by the run (forward, completed, unless the run kept them)
	if !undo && !out.Stopped && !hdr.KeepEmptyDirs {
		r := &Result{Moves: moves}
		for _, d := range dels {
			r.Deletes = append(r.Deletes, Delete{Path: d.Src})
		}
		out.DirsRemoved = removeEmptyDirs(root, r)
	}
	return out, nil
}

// namingOrDefault is the -naming value, ext-year when empty.
func namingOrDefault(spec string) string {
	if strings.TrimSpace(spec) == "" {
		return "ext-year"
	}
	return spec
}

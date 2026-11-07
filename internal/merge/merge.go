// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package merge implements `onefold merge`: fold N input trees into an output
// tree (which may itself be one of the inputs) so that every distinct content
// exists in the output exactly once more than before, never less.
//
// Guarantees (pinned by merge_test.go):
//   - inputs are read-only; files already in the output are never moved,
//     renamed, overwritten or deleted (only onefold' own *.onefold-tmp leftovers);
//   - dedup is EXACT content only (sparse hash confirmed by a full-content hash
//     before anything is skipped); media variants are never dropped;
//   - every copy is written to a temp file, hashed while written, fsync'ed,
//     re-read and compared, then renamed without replacing anything.
package merge

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charlievieth/fastwalk"
	"github.com/zeebo/xxh3"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/fsx"
	"github.com/mauriziofonte/onefold/internal/names"
	"github.com/mauriziofonte/onefold/internal/naming"
	"github.com/mauriziofonte/onefold/internal/perf"
	"github.com/mauriziofonte/onefold/internal/plan"
	"github.com/mauriziofonte/onefold/internal/report"
	"github.com/mauriziofonte/onefold/internal/runctl"
)

// Options configure a merge.
type Options struct {
	Inputs  []string
	Output  string
	Catalog catalog.Options // filters + hashing for INPUT rooms (Out, Host, SkipDirs, Reuse are managed here)
	Layout  string          // historical alias of Naming (dedupe | timeline | document | music | device) or any preset
	Naming  string          // naming preset, template or selector map (default ext-year)

	Execute      bool
	TrustSparse  bool // skip the full-content confirmation of duplicate groups (NOT recommended)
	NoVerifyCopy bool // skip re-reading each copy
	NoFsync      bool // skip fsync before rename
	// VerifyUncached re-reads each copy from the device instead of the OS
	// cache (Linux: cached pages dropped; Windows: unbuffered reads): proves
	// the media, at the cost of a real read. Falls back to a cached read
	// where the OS refuses it (counted in Result.CachedVerify). Needs fsync.
	VerifyUncached bool
	NoCache        bool // do not read/write <output>/.onefold/merge-cache.jsonl
	Workers        int  // copy/verify workers (0 = NumCPU)
	Heartbeat      time.Duration
	Log            io.Writer // progress/messages (stderr)

	// Gate pauses or stops the run between units of work (GUI); nil = never.
	Gate *runctl.Gate
	// BeforeExecute is called with the plan right before the first write; an
	// error aborts the run with nothing written (GUI: the executed plan must be
	// the confirmed one).
	BeforeExecute func(*Result) error
	// StateCache is an extra catalog cache outside the output (GUI job state):
	// read at start, written after a preview and after an execute, so a preview
	// writes nothing into the output yet the execute does not re-hash.
	StateCache string
	// Records re-plans a previous preview of the same folders (GUI):
	// its records (output + inputs, as the state cache holds them) replace
	// the walk; the user exclusions of Catalog are applied to the input
	// records (Excluded lists the ones they leave out). Nothing is read from
	// disk but the output listing; the state cache is not rewritten.
	Records []catalog.Record
	// Packages of that preview (re-plan): recounted on the records kept.
	Packages []catalog.Package
}

// CacheDir is the output-local state directory (never cataloged, never copied).
const CacheDir = ".onefold"

// Copy is one planned copy.
type Copy struct {
	Src     catalog.Record `json:"-"`
	SrcPath string         `json:"src"`
	Rel     string         `json:"target"` // slash-separated, relative to the output
	Size    int64          `json:"size"`
	Renamed bool           `json:"renamed,omitempty"` // collision suffix applied
	Dupes   []string       `json:"inputDupes,omitempty"`
}

// Result is the merge plan + outcome.
type Result struct {
	Output          string     `json:"output"`
	Rooms           []string   `json:"rooms"`
	InputFiles      int        `json:"inputFiles"`
	OutputFiles     int        `json:"outputFiles"`
	Distinct        int        `json:"distinctContents"`
	AlreadyPresent  int        `json:"inputFilesAlreadyInOutput"`
	InputDupes      int        `json:"inputDuplicatesNotCopied"`
	OutputDupGroups int        `json:"outputDuplicateGroups"`
	OutputDupFiles  int        `json:"outputDuplicateFiles"`
	OutputDupList   [][]string `json:"outputDuplicates,omitempty"` // paths of each duplicate group already in the output
	Copies          []Copy     `json:"copies"`
	CopyBytes       int64      `json:"copyBytes"`
	Renamed         int        `json:"renamedForCollision"`
	Blocked         []string   `json:"blocked,omitempty"` // targets whose parent is an existing FILE

	Verify         plan.VerifyStats `json:"verify"`
	StaleTemps     int              `json:"staleTempsRemoved"`
	Copied         int              `json:"copied"`
	CopiedBytes    int64            `json:"copiedBytes"`
	Failed         []string         `json:"failed,omitempty"`
	SkippedByRules int              `json:"inputFilesSkippedByFilters"`
	EmptyInputs    int              `json:"inputEmptyFilesSkipped"`
	// Pending copies were not started because the gate was stopped; a new run
	// with the same options completes them.
	Pending int  `json:"pendingCopies,omitempty"`
	Stopped bool `json:"stopped,omitempty"`

	// Excluded lists the input files and directories the filters left out,
	// with their reason (sorted by path).
	Excluded []catalog.Skip `json:"excluded,omitempty"`
	// Settled lists, after an execute, every input file whose content is in
	// the output: copied and verified by this run (with its input duplicates)
	// or already there. Failed, blocked and pending copies are not in it. The
	// source purge deletes from this list only.
	Settled []Settle `json:"settled,omitempty"`
	// Packages: input folders that look like driver or installer packages,
	// with the files the plan holds there.
	Packages     []catalog.Package `json:"packages,omitempty"`
	present      []Settle          // input files already in the output (decide)
	CachedVerify int               `json:"cachedVerify,omitempty"` // -verify-uncached copies verified through the cache (OS refused an uncached read)
}

// Settle pairs an input file with the output file holding its content.
type Settle struct {
	Src     string `json:"src"`
	Keeper  string `json:"keeper"`
	Size    int64  `json:"size"`
	Content string `json:"content,omitempty"` // full-content XXH128 when known
}

// Missing is the number of distinct contents that are NOT in the output after
// the run (0 = merge complete).
func (r *Result) Missing() int { return len(r.Failed) + len(r.Blocked) + r.Pending }

func (o *Options) logf(format string, a ...any) {
	if o.Log != nil {
		fmt.Fprintf(o.Log, format, a...)
	}
}

// progress returns a live reporter, or nil when the caller wants no output.
func (o *Options) progress(label string, unit report.Unit) *report.Reporter {
	if o.Log == nil {
		return nil
	}
	return report.New(report.Options{Label: label, Unit: unit, Growing: true, Heartbeat: o.Heartbeat})
}

// collect catalogs rooms with a live progress (when output is wanted).
func (o *Options) collect(label string, rooms []string, co catalog.Options) ([]catalog.Record, *catalog.Summary, error) {
	if r := o.progress(label, report.Items); r != nil {
		co.Progress = r
		defer r.Stop()
	}
	return catalog.Collect(rooms, co)
}

// cleanAbs is fsx.Canonical: one spelling per folder, also for an output
// that does not exist yet (resolved through its existing parent), so that a
// link or a short name cannot hide that the output is inside an input.
func cleanAbs(p string) (string, error) { return fsx.Canonical(p) }

// fold normalizes a path for comparisons: case-insensitive on Windows.
func fold(p string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}

// within reports whether p is dir or inside dir.
func within(p, dir string) bool {
	fp, fd := fold(p), fold(dir)
	return fp == fd || strings.HasPrefix(fp, strings.TrimSuffix(fd, string(filepath.Separator))+string(filepath.Separator))
}

// Run plans (and with Execute, performs) the merge.
func Run(o Options) (*Result, error) {
	if o.Workers <= 0 {
		o.Workers = runtime.NumCPU()
	}
	// -naming is a preset or template; only -layout takes the historical aliases
	nm, err := naming.Resolve(o.Naming)
	if strings.TrimSpace(o.Naming) == "" {
		nm, err = naming.Layout(o.Layout)
	}
	if err != nil {
		return nil, err
	}
	out, err := cleanAbs(o.Output)
	if err != nil {
		return nil, err
	}
	if fi, err := os.Stat(out); err == nil && !fi.IsDir() {
		return nil, fmt.Errorf("output %s is not a directory", out)
	} else if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	res := &Result{Output: out}

	// Rooms: the output (always, unfiltered) + inputs not inside the output.
	var inputs []string
	for _, in := range o.Inputs {
		a, err := cleanAbs(in)
		if err != nil {
			return nil, err
		}
		fi, err := os.Stat(a)
		if err != nil || !fi.IsDir() {
			return nil, fmt.Errorf("input %s is not a readable directory", in)
		}
		if within(a, out) {
			continue // input == output or inside it: covered by the output room
		}
		dup := false
		for _, x := range inputs {
			if within(a, x) {
				dup = true // nested in an earlier input: covered by it
			}
		}
		if !dup {
			inputs = append(inputs, a)
		}
	}
	res.Rooms = append([]string{out}, inputs...)

	cache := map[string]catalog.Record{}
	cachePath := filepath.Join(out, CacheDir, "merge-cache.jsonl")
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

	// 1. catalog
	t0 := time.Now()
	var recs []catalog.Record
	if o.Records != nil {
		for _, r := range o.Records {
			if r.Host == "output" {
				res.OutputFiles++
			} else if !within(r.Path, out) {
				if rule := o.Catalog.UserExclusion(r.Room, r.Path); rule != "" {
					res.Excluded = append(res.Excluded, catalog.Skip{Path: r.Path, Rule: rule})
					continue
				}
				res.InputFiles++
			} else {
				continue
			}
			recs = append(recs, r)
		}
		catalog.SortSkips(res.Excluded)
		var ins []catalog.Record
		for _, r := range recs {
			if r.Host != "output" {
				ins = append(ins, r)
			}
		}
		res.Packages = catalog.RecountPackages(o.Packages, ins)
	} else if _, err := os.Stat(out); err == nil {
		oc := o.Catalog.Unfiltered()
		oc.SkipDirs = []string{filepath.Join(out, CacheDir)}
		oc.Reuse, oc.Host, oc.Gate = cache, "output", o.Gate
		o.logf("cataloging output %s (every file, no filters)…\n", out)
		rs, _, err := o.collect("catalog output", []string{out}, oc)
		if err != nil {
			return nil, err
		}
		recs = append(recs, rs...)
		res.OutputFiles = len(rs)
	}
	if len(inputs) > 0 && o.Records == nil {
		ic := o.Catalog
		ic.SkipDirs = append(append([]string(nil), ic.SkipDirs...), out)
		ic.Reuse, ic.Host, ic.Gate = cache, "input", o.Gate
		ic.OnSkip = func(sk catalog.Skip) { res.Excluded = append(res.Excluded, sk) }
		o.logf("cataloging %d input room(s)…\n", len(inputs))
		rs, sum, err := o.collect("catalog inputs", inputs, ic)
		if err != nil {
			return nil, err
		}
		recs = append(recs, rs...)
		res.InputFiles = len(rs)
		res.SkippedByRules = sum.Skipped - sum.Empty
		res.EmptyInputs = sum.Empty
		res.Packages = sum.Packages
		catalog.SortSkips(res.Excluded)
	}
	perf.PhaseDone("catalog", t0, o.Catalog.Workers, int64(len(recs)), 0)

	// De-duplicate records by path (nested rooms, symlinks).
	seen := map[string]bool{}
	uniq := recs[:0]
	for _, r := range recs {
		k := fold(r.Path)
		if !seen[k] {
			seen[k] = true
			uniq = append(uniq, r)
		}
	}
	recs = uniq

	// 2. confirm duplicates over the full content
	t1 := time.Now()
	if !o.TrustSparse {
		var vp catalog.Progress
		r := o.progress("verify", report.Bytes)
		if r != nil {
			vp = r
		}
		recs, res.Verify = plan.VerifyGroupsWith(recs, o.Workers, vp, o.Gate)
		if r != nil {
			r.Stop()
		}
	}
	perf.PhaseDone("verify-full", t1, o.Workers, int64(res.Verify.Groups), res.Verify.Bytes)
	if o.Gate.Stopped() {
		res.Stopped = true
		return res, runctl.ErrStopped
	}

	// 3. decide
	t2 := time.Now()
	occupied, fileSet, err := listOutput(out)
	if err != nil {
		return nil, err
	}
	decide(o, nm, res, recs, out, occupied, fileSet)
	perf.PhaseDone("plan", t2, 1, int64(len(res.Copies)), res.CopyBytes)

	if !o.Execute {
		if o.StateCache != "" && o.Records == nil {
			if err := WriteCache(o.StateCache, recs); err != nil {
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
	// Stale temps of an interrupted run (only our suffix), now that writing is certain.
	res.StaleTemps = removeStaleTemps(out)

	// 4. execute
	t3 := time.Now()
	if err := os.MkdirAll(out, 0o755); err != nil {
		return res, err
	}
	copied := execute(o, res, out)
	perf.PhaseDone("execute", t3, o.Workers, int64(res.Copied), res.CopiedBytes)
	res.settle(copied)

	// 5. cache for the next run: every record + the new output files.
	recs = append(recs, copied...)
	if !o.NoCache {
		if err := WriteCache(cachePath, recs); err != nil {
			o.logf("warning: cache not written: %v\n", err)
		}
	}
	if o.StateCache != "" {
		if err := WriteCache(o.StateCache, recs); err != nil {
			o.logf("warning: state cache not written: %v\n", err)
		}
	}
	res.Stopped = o.Gate.Stopped()
	return res, nil
}

// settle fills Settled: the files already present, then every copy that was
// published (copied records carry their source in SrcPath order of res.Copies).
func (res *Result) settle(copied []catalog.Record) {
	done := map[string]catalog.Record{}
	for _, r := range copied {
		done[r.Path] = r
	}
	res.Settled = append([]Settle(nil), res.present...)
	for _, c := range res.Copies {
		r, ok := done[filepath.Join(res.Output, filepath.FromSlash(c.Rel))]
		if !ok {
			continue
		}
		for _, src := range append([]string{c.SrcPath}, c.Dupes...) {
			res.Settled = append(res.Settled, Settle{Src: src, Keeper: r.Path, Size: c.Size, Content: r.ContentHash})
		}
	}
	sort.Slice(res.Settled, func(i, j int) bool { return res.Settled[i].Src < res.Settled[j].Src })
}

// removeStaleTemps deletes *.onefold-tmp files under out (never anything else).
func removeStaleTemps(out string) int {
	n := 0
	filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && fsx.IsTemp(d.Name()) {
			if os.Remove(p) == nil {
				n++
			}
		}
		return nil
	})
	return n
}

// listOutput returns every existing path under out (files AND dirs, relative,
// slash-separated, lowercased: NTFS is case-insensitive) and the set of files.
func listOutput(out string) (occupied, files map[string]bool, err error) {
	occupied, files = map[string]bool{}, map[string]bool{}
	if _, err := os.Stat(out); os.IsNotExist(err) {
		return occupied, files, nil
	}
	var mu sync.Mutex
	conf := fastwalk.Config{NumWorkers: runtime.NumCPU()}
	err = fastwalk.Walk(&conf, out, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == out {
			return nil
		}
		rel, rerr := filepath.Rel(out, p)
		if rerr != nil {
			return nil
		}
		k := names.FoldKey(filepath.ToSlash(rel))
		mu.Lock()
		occupied[k] = true
		if !d.IsDir() {
			files[k] = true
		}
		mu.Unlock()
		return nil
	})
	return occupied, files, err
}

func decide(o Options, nm *naming.Naming, res *Result, recs []catalog.Record, out string, occupied, files map[string]bool) {
	inputRank := map[string]int{}
	for i, in := range res.Rooms[1:] {
		inputRank[fold(in)] = i
	}
	rank := func(r catalog.Record) int {
		best := len(inputRank)
		for dir, i := range inputRank {
			if within(r.Path, dir) && i < best {
				best = i
			}
		}
		return best
	}
	known := make(map[string]bool, len(recs))
	for _, r := range recs {
		known[names.FoldKey(r.Path)] = true
	}
	exists := func(p string) bool { return known[names.FoldKey(p)] }
	groups := map[string][]catalog.Record{}
	var order []string
	for _, r := range recs {
		if _, ok := groups[r.Hash]; !ok {
			order = append(order, r.Hash)
		}
		groups[r.Hash] = append(groups[r.Hash], r)
	}
	sort.Strings(order)
	res.Distinct = len(order)
	for _, h := range order {
		g := groups[h]
		var ins, outs []catalog.Record
		for _, r := range g {
			if within(r.Path, out) {
				outs = append(outs, r)
			} else {
				ins = append(ins, r)
			}
		}
		if len(outs) > 0 {
			res.AlreadyPresent += len(ins)
			sort.Slice(outs, func(i, j int) bool { return outs[i].Path < outs[j].Path })
			for _, r := range ins {
				content := r.ContentHash
				if content == "" {
					content = outs[0].ContentHash
				}
				res.present = append(res.present, Settle{Src: r.Path, Keeper: outs[0].Path, Size: r.Size, Content: content})
			}
			if len(outs) > 1 {
				res.OutputDupGroups++
				res.OutputDupFiles += len(outs) - 1
				var g []string
				for _, r := range outs {
					g = append(g, r.Path)
				}
				sort.Strings(g)
				res.OutputDupList = append(res.OutputDupList, g)
			}
			continue
		}
		sort.SliceStable(ins, func(i, j int) bool {
			a, b := ins[i], ins[j]
			if ra, rb := rank(a), rank(b); ra != rb {
				return ra < rb
			}
			if da, db := names.IsCopyName(a.Path), names.IsCopyName(b.Path); da != db {
				return !da
			}
			if a.MTimeNs != b.MTimeNs && a.MTimeNs != 0 && b.MTimeNs != 0 {
				return a.MTimeNs < b.MTimeNs
			}
			if len(a.Path) != len(b.Path) {
				return len(a.Path) < len(b.Path)
			}
			return a.Path < b.Path
		})
		w := ins[0]
		c := Copy{Src: w, SrcPath: w.Path, Size: w.Size, Rel: nm.Target(naming.VarsOfCtx(w, exists))}
		for _, d := range ins[1:] {
			c.Dupes = append(c.Dupes, d.Path)
		}
		res.InputDupes += len(ins) - 1
		res.Copies = append(res.Copies, c)
	}
	// Collision-free targets (onefold _NN), checked against the existing output
	// (files AND dirs, case-insensitive) and against each other.
	sort.SliceStable(res.Copies, func(i, j int) bool { return res.Copies[i].SrcPath < res.Copies[j].SrcPath })
	items := make([]naming.Item, len(res.Copies))
	for i, c := range res.Copies {
		items[i] = naming.Item{Target: c.Rel, Tie: c.Src.Hash + "\x00" + c.SrcPath}
	}
	final := naming.Assign(items, occupied)
	kept := res.Copies[:0]
	for i, c := range res.Copies {
		if final[i] != c.Rel {
			c.Rel, c.Renamed = final[i], true
			res.Renamed++
		}
		if blockedParent(c.Rel, files) {
			res.Blocked = append(res.Blocked, c.SrcPath+" -> "+c.Rel)
			continue
		}
		res.CopyBytes += c.Size
		kept = append(kept, c)
	}
	res.Copies = kept
}

// blockedParent: some parent directory of rel exists in the output as a FILE.
func blockedParent(rel string, files map[string]bool) bool {
	for d := path.Dir(rel); d != "." && d != "/"; d = path.Dir(d) {
		if files[names.FoldKey(d)] {
			return true
		}
	}
	return false
}

// copyDelay holds every copy back by a fixed time before it starts. Only the
// GUI end-to-end test sets it (ONEFOLD_TEST_COPY_DELAY, a Go duration such as
// "20ms"), so pause, resume and a kill mid-copy find a copy phase still
// running on a fast machine; unset or invalid means no delay.
var copyDelay = parseCopyDelay(os.Getenv("ONEFOLD_TEST_COPY_DELAY"))

func parseCopyDelay(v string) time.Duration {
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return 0
	}
	return d
}

func execute(o Options, res *Result, out string) []catalog.Record {
	cached0 := cachedVerify.Load()
	defer func() { res.CachedVerify = int(cachedVerify.Load() - cached0) }()
	var total int64
	for _, c := range res.Copies {
		total += c.Size
	}
	rep := report.New(report.Options{Label: "merge", TotalBytes: total, Heartbeat: o.Heartbeat})
	jobs := make(chan Copy)
	var mu sync.Mutex
	var wg sync.WaitGroup
	var copied []catalog.Record
	var dirs fsx.DirSet // output folders that gained a file: flushed at the end
	for w := 0; w < o.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 4<<20)
			for c := range jobs {
				if !o.Gate.Wait() {
					mu.Lock()
					res.Pending++
					rep.Advance(c.Size)
					mu.Unlock()
					continue
				}
				if copyDelay > 0 {
					time.Sleep(copyDelay)
				}
				t0 := time.Now()
				dst := filepath.Join(out, filepath.FromSlash(c.Rel))
				content, err := copyOne(c, dst, buf, o)
				mu.Lock()
				if err != nil {
					res.Failed = append(res.Failed, c.SrcPath+" -> "+c.Rel+": "+err.Error())
					rep.Err()
					rep.Event(fmt.Sprintf("ERR %s -> %s: %v", c.SrcPath, c.Rel, err))
				} else {
					res.CopiedBytes += c.Size
					nr := c.Src
					nr.Path, nr.Room, nr.Host, nr.ContentHash = dst, out, "output", content
					copied = append(copied, nr)
					dirs.Add(dst)
					rep.Item(c.Size, time.Since(t0))
				}
				mu.Unlock()
			}
		}()
	}
	// Dispatch in SOURCE path order: neighbouring files are read together
	// (locality on cold, seek/IOPS-bound sources). Targets are already fixed.
	order := append([]Copy(nil), res.Copies...)
	sort.Slice(order, func(i, j int) bool { return order[i].SrcPath < order[j].SrcPath })
	for _, c := range order {
		jobs <- c
	}
	close(jobs)
	wg.Wait()
	// Each copy's bytes were flushed before its rename; the renames themselves
	// live in the folders' entries, flushed once per folder here (not per
	// file: thousands of small files share a few folders).
	if !o.NoFsync {
		if err := dirs.SyncAll(); err != nil {
			rep.Event(fmt.Sprintf("WARN flushing the output folders: %v", err))
		}
	}
	rep.Finish()
	res.Copied = len(copied)
	sort.Strings(res.Failed)
	return copied
}

var errChanged = errors.New("source changed since cataloging (size differs)")

// CopyVerified copies srcPath to dst the way merge copies: a onefold temp
// next to dst, the content hash computed while writing, fsync, the temp read
// again and compared, the source mtime, then a rename that never replaces
// (fsx.ErrExists when dst appeared). size is the expected source size. It
// returns the content hash of what was written (sparsehash.FullFile form).
func CopyVerified(srcPath, dst string, size int64, buf []byte) (string, error) {
	return copyOne(Copy{SrcPath: srcPath, Size: size}, dst, buf, Options{})
}

// copyOne: temp in the target dir -> hash while copying -> fsync -> re-read
// and compare -> source mtime -> rename without replace.
func copyOne(c Copy, dst string, buf []byte, o Options) (content string, err error) {
	tm := perf.Start()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	perf.End("mkdir", tm, 0)
	src, err := os.Open(c.SrcPath)
	if err != nil {
		return "", err
	}
	defer src.Close()
	sfi, err := src.Stat()
	if err != nil {
		return "", err
	}
	if sfi.Size() != c.Size {
		return "", errChanged
	}
	tmp := dst + "." + strconv.FormatInt(rand.Int63(), 36) + fsx.TempSuffix
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmp)
		}
	}()
	// Pre-size: one allocation up front (less NTFS extent churn / fragmentation).
	if c.Size > 0 {
		_ = f.Truncate(c.Size)
	}
	h := xxh3.New()
	tc := perf.Start()
	n, err := io.CopyBuffer(io.MultiWriter(f, h), struct{ io.Reader }{src}, buf)
	perf.End("copy", tc, n)
	if err != nil {
		return "", err
	}
	if n != c.Size {
		return "", errChanged
	}
	sum := hexSum(h)
	if (c.Src.Full && sum != c.Src.Hash && !strings.Contains(c.Src.Hash, "-")) || (c.Src.ContentHash != "" && sum != c.Src.ContentHash) {
		return "", errors.New("source content differs from the cataloged hash")
	}
	if !o.NoFsync {
		ts := perf.Start()
		if err = f.Sync(); err != nil {
			return "", err
		}
		perf.End("fsync", ts, 0)
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	if !o.NoVerifyCopy {
		tv := perf.Start()
		got, verr := verifyHash(tmp, buf, o.VerifyUncached && !o.NoFsync)
		perf.End("verify-copy", tv, n)
		if verr != nil {
			err = verr
			return "", err
		}
		if got != hexSum(h) {
			err = errors.New("copy verification failed (written bytes differ)")
			return "", err
		}
	}
	mt := sfi.ModTime()
	_ = os.Chtimes(tmp, mt, mt)
	tr := perf.Start()
	if err = fsx.RenameNoReplace(tmp, dst); err != nil {
		return "", err
	}
	perf.End("rename", tr, 0)
	return sum, nil
}

// verifyHash hashes a written copy, from the device when uncached (only
// after an fsync: dirty pages would still come from the cache).
func verifyHash(p string, buf []byte, uncached bool) (string, error) {
	if !uncached {
		return hashFile(p, buf)
	}
	f, err := fsx.OpenUncached(p)
	if err != nil {
		cachedVerify.Add(1)
		return hashFile(p, buf) // filesystem without uncached reads (tmpfs, network): the cached proof
	}
	defer f.Close()
	abp := alignedBufs.Get().(*[]byte)
	defer alignedBufs.Put(abp)
	ab := *abp
	h := xxh3.New()
	for {
		n, err := f.Read(ab)
		h.Write(ab[:n])
		if err == io.EOF || (err == nil && n == 0) {
			break
		}
		if err != nil {
			return "", err
		}
	}
	return hexSum(h), nil
}

// alignedBufs: read buffers for uncached reads (aligned, 4 MiB).
// The pool holds pointers so that Put does not allocate a slice header.
var alignedBufs = sync.Pool{New: func() any { b := fsx.AlignedBuffer(4 << 20); return &b }}

// cachedVerify counts the verifications that fell back to the cache.
var cachedVerify atomic.Int64

func hashFile(p string, buf []byte) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := xxh3.New()
	if _, err := io.CopyBuffer(h, struct{ io.Reader }{f}, buf); err != nil {
		return "", err
	}
	return hexSum(h), nil
}

func hexSum(h *xxh3.Hasher) string {
	b := h.Sum128().Bytes()
	const hexd = "0123456789abcdef"
	out := make([]byte, 32)
	for i, v := range b {
		out[i*2], out[i*2+1] = hexd[v>>4], hexd[v&15]
	}
	return string(out)
}

// WriteCache writes the cache atomically (records with raw sparse hashes).
func WriteCache(p string, recs []catalog.Record) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := p + fsx.TempSuffix
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	bw := bufio.NewWriterSize(f, 1<<20)
	if err := catalog.WriteHeader(bw, catalog.KindManifest); err != nil {
		f.Close()
		return err
	}
	enc := json.NewEncoder(bw)
	for i := range recs {
		r := recs[i]
		if i := strings.Index(r.Hash, "-"); i > 0 {
			r.Hash = r.Hash[:i] // store the raw sparse hash; verification suffixes are per-run
		}
		if err := enc.Encode(&r); err != nil {
			f.Close()
			return err
		}
	}
	if err := bw.Flush(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, p) // our own cache file: replacing it is intended
}

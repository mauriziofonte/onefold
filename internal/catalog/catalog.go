// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package catalog is the Phase-1 indexer: it walks rooms, and for every in-scope
// asset computes its sparse-digest + canonical metadata, writing a JSONL manifest
// and an aggregate summary.
//
// STRICTLY READ-ONLY on the source rooms: it opens files for reading only, and
// never moves, copies, renames, or deletes anything. The only file it writes is
// the manifest at the caller-chosen output path.
package catalog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charlievieth/fastwalk"

	"github.com/mauriziofonte/onefold/internal/fsx"
	"github.com/mauriziofonte/onefold/internal/kinds"
	"github.com/mauriziofonte/onefold/internal/meta"
	"github.com/mauriziofonte/onefold/internal/names"
	"github.com/mauriziofonte/onefold/internal/perf"
	"github.com/mauriziofonte/onefold/internal/runctl"
	"github.com/mauriziofonte/onefold/internal/sparsehash"
)

// Record is one manifest line: an asset's identity + metadata. Host, MTimeNs and
// Quick are omitempty so manifests written before them load unchanged.
type Record struct {
	Room    string    `json:"room"`
	Path    string    `json:"path"`
	Size    int64     `json:"size"`
	Hash    string    `json:"hash"`
	Full    bool      `json:"fullHash"`
	Meta    meta.Meta `json:"meta"`
	Host    string    `json:"host,omitempty"`    // --host label of the machine that cataloged it
	MTimeNs int64     `json:"mtimeNs,omitempty"` // source mtime (unix ns): reuse key + generic winner tiebreak
	Quick   bool      `json:"quick,omitempty"`   // extracted in --quick mode
	// ContentHash = XXH128 of the WHOLE content when known (full-hashed files,
	// verify-full, merge copies). Valid as long as size+mtime are unchanged.
	ContentHash string `json:"contentHash,omitempty"`
}

// Summary is the aggregate view printed for inspection (no per-file personal data).
type Summary struct {
	Assets  int
	Photos  int
	Videos  int
	Errors  int
	Skipped int
	Limited int // accepted by the filters but over --limit
	Empty   int // 0-byte files (no content: never cataloged, never merged)
	// OnlineOnly: cloud files left out because their content is not on this
	// computer (rule online-only), with their total size.
	OnlineOnly      int
	OnlineOnlyBytes int64
	Reused          int // copied from the --reuse manifest (path+size+mtime unchanged)
	NoTime          int
	FromMetadata    int
	FromFilename    int
	FromMtime       int
	SubSec          int
	DimsMissing     int
	LogicalBytes    int64
	ByKind          map[string]int
	ByDevice        map[string]int
	ByYear          map[string]int
	Elapsed         time.Duration
	Samples         []Record // a few examples for eyeballing
	// Packages: folders that look like driver or installer packages and
	// hold cataloged files, sorted by folder.
	Packages []Package
}

// Options control enumeration, filtering and extraction.
type Options struct {
	Workers  int
	Coverage int
	Sample   int
	Out      string

	Exts    map[string]bool // extension whitelist (no dot); nil = every extension
	Exclude []string        // path substrings, matched RELATIVE to the room root
	// ExcludePaths: absolute files or folders left out (folder = everything
	// inside, component boundary); ExcludeGlobs: patterns on the
	// room-relative path. See UserExclusion.
	ExcludePaths []string
	ExcludeGlobs []Glob
	Prune        []string // directory names (whole component, case-insensitive)
	KeepDotDirs  bool     // default false: directories starting with "." are pruned
	KeepDotfiles bool     // default false: files starting with "." are skipped
	// KeepEmpty catalogs 0-byte files with the hash EmptyHash(name): one
	// empty file per name (merge -keep-dotfiles); default false: skipped.
	KeepEmpty bool
	KeepJunk  bool // default false: __MACOSX, .DS_Store, Thumbs.db, desktop.ini skipped

	SkipSystemDirs bool // prune kinds.SystemDirs
	SkipCloudDirs  bool // prune kinds.CloudDirs
	SkipDevDirs    bool // prune kinds.DevDirs
	SkipSystemExt  bool // only without whitelist
	SkipUnknownExt bool // only without whitelist
	SkipNoExt      bool
	SkipTransient  bool  // incomplete downloads, temp and lock files (kinds.IsTransient)
	SkipShortcuts  bool  // lnk, url, website... (kinds.IsShortcut)
	SkipOnlineOnly bool  // cloud placeholders: reading would download them (fsx.OnlineOnly)
	MinSize        int64 // bytes; 0 = off (0-byte files are always skipped)
	MaxSize        int64 // bytes; 0 = off
	Limit          int   // max accepted files per room; 0 = unlimited

	SkipDirs []string // absolute directories never descended (merge: output inside an input)
	WholeMax int64    // files up to this size are read once, sequentially, and hashed/extracted from memory (0 = off)

	Host  string            // label stored in every record
	Reuse map[string]Record // previous manifest by path

	Progress Progress     // optional (report.Reporter implements it)
	Gate     *runctl.Gate // optional pause/stop (GUI); nil = never pauses
	Quick    bool
	// OnSkip receives every file or directory left out, with its reason (rule
	// name or Reason*); serialized. Onefold' own temp files are not reported.
	OnSkip func(Skip)
}

// Build indexes every room into the manifest at o.Out (JSONL). READ-ONLY on the
// rooms: files are opened for reading only.
func Build(rooms []string, o Options) (*Summary, error) {
	if o.Host == "" {
		o.Host = DefaultHost() // every record of a manifest carries a host
	}
	out, err := os.Create(o.Out)
	if err != nil {
		return nil, err
	}
	bw := bufio.NewWriterSize(out, 1<<20)
	werr := WriteHeader(bw, KindManifest) // first write error: a truncated manifest must fail the run
	sum, err := Walk(rooms, o, func(rec Record) {
		if werr != nil {
			return
		}
		line, merr := json.Marshal(rec)
		if merr != nil {
			werr = merr
			return
		}
		if _, werr = bw.Write(line); werr == nil {
			werr = bw.WriteByte('\n')
		}
	})
	if werr == nil {
		werr = bw.Flush()
	}
	if cerr := out.Close(); werr == nil {
		werr = cerr
	}
	if err == nil && werr != nil {
		err = fmt.Errorf("write manifest %s: %w", o.Out, werr)
	}
	return sum, err
}

// Collect is Walk into memory (merge).
func Collect(rooms []string, o Options) ([]Record, *Summary, error) {
	var recs []Record
	sum, err := Walk(rooms, o, func(r Record) { recs = append(recs, r) })
	return recs, sum, err
}

// sameDir compares cleaned absolute directory paths (case-insensitive on Windows).
func sameDir(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// Walk enumerates, filters, hashes and extracts every file of the rooms,
// calling emit (serialized) for each record. READ-ONLY on the rooms.
// Progress receives per-file progress: AddTotal when a file is queued,
// TotalKnown when enumeration ends, then Item (processed) or Advance (reused,
// failed) once per queued file, Err on failures.
type Progress interface {
	AddTotal(bytes int64)
	TotalKnown()
	Item(bytes int64, dur time.Duration)
	Advance(bytes int64)
	Err()
}

// errorReporter is the optional extension of Progress that receives the path of
// a failed item (report.Reporter implements it; the CLI only counts).
type errorReporter interface {
	ErrorAt(path string, err error)
}

// reportError sends err for path to prog: with its path when supported.
func reportError(prog Progress, path string, err error) {
	if er, ok := prog.(errorReporter); ok {
		er.ErrorAt(path, err)
		return
	}
	prog.Err()
}

type noProgress struct{}

func (noProgress) AddTotal(int64)            {}
func (noProgress) TotalKnown()               {}
func (noProgress) Item(int64, time.Duration) {}
func (noProgress) Advance(int64)             {}
func (noProgress) Err()                      {}

func Walk(rooms []string, o Options, emit func(Record)) (*Summary, error) {
	// the exiftool readers the scan started do not outlive it
	defer meta.CloseExifReaders()
	prog := o.Progress
	if prog == nil {
		prog = noProgress{}
	}
	var skipDirs []string
	for _, d := range o.SkipDirs {
		if abs, err := fsx.Canonical(d); err == nil {
			skipDirs = append(skipDirs, abs)
		}
	}

	prune := o.pruneRules()
	sum := &Summary{ByKind: map[string]int{}, ByDevice: map[string]int{}, ByYear: map[string]int{}}
	dirs := map[string]*dirMarks{} // package markers per folder, guarded by mu
	var mu sync.Mutex              // guards bw + sum
	start := time.Now()
	// skip counts a file left out (dir=false) and reports it with its reason
	skip := func(path, rule string, dir bool) {
		mu.Lock()
		if !dir {
			sum.Skipped++
		}
		if o.OnSkip != nil && rule != "-" {
			o.OnSkip(Skip{Path: path, Dir: dir, Rule: rule})
		}
		mu.Unlock()
	}

	// Enumeration (fastwalk: parallel per DIRECTORY) only filters names and
	// queues files; hashing + extraction run on a separate pool so a directory
	// with thousands of files is processed in parallel too (fastwalk alone runs
	// every file of one directory on the goroutine that read it: measured
	// concurrency ~1 on D:\FILES\HTML).
	type job struct {
		room, path  string
		size, mtime int64
	}
	workers := o.Workers
	if workers < 1 {
		workers = 1
	}
	// Enumeration never waits for the workers (unbounded job buffer, a few
	// dozen bytes per file): the total is known early, so progress % and ETA
	// are honest, and stat traffic does not stall behind hashing.
	jobs := make(chan job, 4*workers)
	work := make(chan job, 4*workers)
	go func() {
		var q []job
		for {
			var out chan job
			var next job
			if len(q) > 0 {
				out, next = work, q[0]
			}
			select {
			case j, ok := <-jobs:
				if !ok {
					for _, j := range q {
						work <- j
					}
					close(work)
					return
				}
				q = append(q, j)
			case out <- next:
				q = q[1:]
			}
		}
	}()
	var wg sync.WaitGroup
	process := func(j job) {
		if !o.Gate.Wait() {
			prog.Advance(j.size) // stopped: the caller discards a partial catalog
			return
		}
		t0 := time.Now()
		var rec Record
		reused := false
		if old, ok := o.Reuse[j.path]; ok && old.Size == j.size && old.MTimeNs == j.mtime && j.mtime != 0 && !(old.Quick && !o.Quick) {
			rec, reused = old, true
			rec.Room, rec.Host = j.room, o.Host
		} else {
			th := perf.Start()
			h, data, herr := sparsehash.HashFileWhole(j.path, o.Coverage, o.Sample, o.WholeMax)
			if h.Full {
				perf.End("hash:full", th, h.SampledBytes)
			} else {
				perf.End("hash:sparse", th, h.SampledBytes)
			}
			if herr != nil {
				mu.Lock()
				sum.Errors++
				mu.Unlock()
				reportError(prog, j.path, herr)
				prog.Advance(j.size)
				return
			}
			te := perf.Start()
			md, _ := meta.ExtractWith(j.path, meta.Options{Quick: o.Quick, Data: data}) // best-effort; never fatal
			stage := "extract:" + md.Kind
			if md.Kind == "archive" {
				stage += ":" + kinds.Ext(j.path)
			}
			perf.End(stage, te, 0)
			perf.Outlier(stage, j.path, te)
			perf.Outlier("file-total", j.path, th)
			rec = Record{Room: j.room, Path: j.path, Size: h.Size, Hash: h.Hex, Full: h.Full, Meta: md,
				Host: o.Host, MTimeNs: j.mtime, Quick: o.Quick}
			if h.Full {
				rec.ContentHash = h.Hex
			}
			if j.size == 0 {
				// every empty file has the same content: its name is what it is
				rec.Hash, rec.Full, rec.ContentHash = EmptyHash(filepath.Base(j.path)), false, ""
			}
		}
		mu.Lock()
		emit(rec)
		accumulate(sum, rec)
		if reused {
			sum.Reused++
		}
		mu.Unlock()
		if reused {
			prog.Advance(j.size)
		} else {
			prog.Item(j.size, time.Since(t0))
		}
	}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range work {
				process(j)
			}
		}()
	}
	var walkErr error
	for _, room := range rooms {
		room := filepath.Clean(room)
		var accepted atomic.Int64
		walkFn := func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				mu.Lock()
				sum.Errors++
				mu.Unlock()
				reportError(prog, path, err)
				return nil
			}
			if o.Gate.Stopped() {
				if d.IsDir() {
					return fastwalk.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				if filepath.Clean(path) == room {
					return nil // root guard: the room itself is never pruned
				}
				for _, sd := range skipDirs {
					if sameDir(filepath.Clean(path), sd) {
						return fastwalk.SkipDir // e.g. the merge output inside an input
					}
				}
				rule := o.dirRule(d.Name(), prune)
				if rule == "" {
					rule = o.UserExclusion(room, path)
				}
				if rule != "" {
					skip(path, rule, true)
					return fastwalk.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			base := filepath.Base(path)
			ext := kinds.Ext(path)
			dm := markDir(&mu, dirs, path, base)
			// Dotfiles (Android ".trashed-<ts>-…", "._" forks, ...) are never archive assets.
			rule := o.fileRule(base, ext)
			if rule == "" {
				rule = o.UserExclusion(room, path)
			}
			if rule != "" {
				skip(path, rule, false)
				return nil
			}
			ts := perf.Start()
			info, ierr := d.Info()
			perf.End("stat", ts, 0)
			if ierr != nil {
				mu.Lock()
				sum.Errors++
				mu.Unlock()
				reportError(prog, path, ierr)
				return nil
			}
			size := info.Size()
			if o.SkipOnlineOnly && fsx.OnlineOnly(info) {
				mu.Lock()
				sum.OnlineOnly++
				sum.OnlineOnlyBytes += size
				mu.Unlock()
				skip(path, RuleOnlineOnly, false)
				return nil
			}
			// Empty files: nothing to hash, nothing to materialize.
			if size == 0 && !o.KeepEmpty {
				mu.Lock()
				sum.Empty++
				mu.Unlock()
				skip(path, ReasonEmpty, false)
				return nil
			}
			if o.MinSize > 0 && size < o.MinSize {
				skip(path, ReasonMinSize, false)
				return nil
			}
			if o.MaxSize > 0 && size > o.MaxSize {
				skip(path, ReasonMaxSize, false)
				return nil
			}
			if o.Limit > 0 && accepted.Add(1) > int64(o.Limit) {
				mu.Lock()
				sum.Limited++
				if o.OnSkip != nil {
					o.OnSkip(Skip{Path: path, Rule: ReasonLimit})
				}
				mu.Unlock()
				return nil
			}
			prog.AddTotal(size)
			mu.Lock()
			dm.planned++
			dm.bytes += size
			mu.Unlock()
			jobs <- job{room: room, path: path, size: size, mtime: info.ModTime().UnixNano()}
			return nil
		}
		conf := fastwalk.Config{NumWorkers: workers}
		if err := fastwalk.Walk(&conf, room, walkFn); err != nil {
			walkErr = err
			break
		}
	}
	close(jobs)
	prog.TotalKnown()
	wg.Wait()
	sum.Elapsed = time.Since(start)
	sum.Packages = packagesOf(dirs)
	if walkErr == nil && o.Gate.Stopped() {
		walkErr = runctl.ErrStopped
	}
	return sum, walkErr
}

// EmptyHash is the identity of a 0-byte file (KeepEmpty): its name, folded
// (NFC, lower case), so one empty file per name exists after a merge.
func EmptyHash(name string) string { return "empty|" + names.FoldKey(name) }

func accumulate(s *Summary, r Record) {
	s.Assets++
	s.LogicalBytes += r.Size
	s.ByKind[r.Meta.Kind]++
	switch r.Meta.Kind {
	case "photo":
		s.Photos++
	case "video":
		s.Videos++
	}
	switch r.Meta.TimeSource {
	case "metadata":
		s.FromMetadata++
	case "filename":
		s.FromFilename++
	case "filemtime":
		s.FromMtime++
	default:
		s.NoTime++
	}
	if r.Meta.SubSec {
		s.SubSec++
	}
	if (r.Meta.Kind == "photo" || r.Meta.Kind == "video") && (r.Meta.Width == 0 || r.Meta.Height == 0) {
		s.DimsMissing++
	}
	dev := r.Meta.Device
	if dev == "" {
		dev = "(none)"
	}
	s.ByDevice[dev]++
	if len(r.Meta.Time) >= 4 {
		s.ByYear[r.Meta.Time[:4]]++
	} else {
		s.ByYear["(none)"]++
	}
	if len(s.Samples) < 8 {
		s.Samples = append(s.Samples, r)
	}
}

// LoadManifest reads a JSONL manifest back into records (for the planner).
// Blank lines are ignored; any other unreadable line fails the read: a plan
// from a manifest with lines missing would silently drop files. The manifest
// must carry the header with the canonical zone of this run: its times
// are wall clocks in that zone.
func LoadManifest(path string) ([]Record, error) {
	recs, zone, err := LoadManifestRecords(path)
	if err != nil {
		return nil, err
	}
	if err := CheckZone(path, KindManifest, zone); err != nil {
		return nil, err
	}
	return recs, nil
}

// LoadManifestRecords is LoadManifest without the zone check: it returns the
// zone of the header ("" when there is none). For fixtures and tools that
// read records whatever their zone.
func LoadManifestRecords(path string) ([]Record, string, error) {
	recs, zone, bad, first, err := readManifest(path)
	if err != nil {
		return nil, "", err
	}
	if bad > 0 {
		return nil, "", fmt.Errorf("%s: %d malformed line(s), first at line %d: not a complete manifest (catalog it again)", path, bad, first)
	}
	return recs, zone, nil
}

// LoadManifestByPath indexes a manifest by source path for reuse (-reuse, the
// merge/rearrange caches, the resume of an interrupted GUI index). Unreadable
// lines are skipped: their files are simply processed again (a manifest cut
// by a crash ends with a truncated line). A manifest of another zone (or
// without header) is refused: its times would be wrong in this one.
func LoadManifestByPath(path string) (map[string]Record, error) {
	recs, zone, _, _, err := readManifest(path)
	if err != nil {
		return nil, err
	}
	if err := CheckZone(path, KindManifest, zone); err != nil {
		return nil, err
	}
	m := make(map[string]Record, len(recs))
	for _, r := range recs {
		m[r.Path] = r
	}
	return m, nil
}

// readManifest parses every readable line; zone is the header's, bad counts
// the unreadable non-blank lines, first is the number of the first one.
func readManifest(path string) (recs []Record, zone string, bad, first int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", 0, 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // allow long lines
	for n := 1; sc.Scan(); n++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if h, ok := ParseHeader(line); ok {
			if h.Kind != KindManifest {
				return nil, "", 0, 0, fmt.Errorf("%s: a onefold %s, not a manifest", path, h.Kind)
			}
			zone = h.Zone
			continue
		}
		var r Record
		if json.Unmarshal(line, &r) != nil {
			if bad++; bad == 1 {
				first = n
			}
			continue
		}
		recs = append(recs, r)
	}
	if err := sc.Err(); err != nil {
		return nil, "", 0, 0, fmt.Errorf("%s: %w", path, err)
	}
	return recs, zone, bad, first, nil
}

// TopDevices returns device slugs sorted by count desc (for the summary print).
func (s *Summary) TopDevices() []string {
	keys := make([]string, 0, len(s.ByDevice))
	for k := range s.ByDevice {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return s.ByDevice[keys[i]] > s.ByDevice[keys[j]] })
	return keys
}

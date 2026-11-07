// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package stats

import (
	"container/heap"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charlievieth/fastwalk"

	"github.com/mauriziofonte/onefold/internal/fsx"
	"github.com/mauriziofonte/onefold/internal/kinds"
)

const (
	maxProblems      = 200
	maxOtherVolumes  = 100
	maxChildren      = 40  // subfolders listed per folder
	maxExts          = 300 // extensions listed
	maxUnknown       = 150
	largestPerFamily = 25
)

// sizeBands are the file size bands of Result.Sizes.
var sizeBands = []SizeBucket{
	{Label: "0", Min: 0, Max: 1},
	{Label: "< 4 KB", Min: 1, Max: 4 << 10},
	{Label: "4–64 KB", Min: 4 << 10, Max: 64 << 10},
	{Label: "64 KB–1 MB", Min: 64 << 10, Max: 1 << 20},
	{Label: "1–16 MB", Min: 1 << 20, Max: 16 << 20},
	{Label: "16–256 MB", Min: 16 << 20, Max: 256 << 20},
	{Label: "256 MB–4 GB", Min: 256 << 20, Max: 4 << 30},
	{Label: "≥ 4 GB", Min: 4 << 30, Max: 0},
}

var famIndex = func() map[string]int {
	m := map[string]int{}
	for i, f := range kinds.Families {
		m[f] = i
	}
	return m
}()

type famBytes [11]int64

func init() {
	if len(kinds.Families) != len(famBytes{}) {
		panic("stats: famBytes must have one slot per kinds.Families entry")
	}
}

type node struct {
	name     string
	path     string
	agg      Agg
	fam      famBytes
	children map[string]*node
	deeper   bool
}

func (n *node) child(name, path string) *node {
	if n.children == nil {
		n.children = map[string]*node{}
	}
	c := n.children[name]
	if c == nil {
		c = &node{name: name, path: path}
		n.children[name] = c
	}
	return c
}

type extAgg struct {
	Agg
	family, category string
	known            bool
}

type yearAgg struct {
	Agg
	fam famBytes
}

// fileHeap keeps the n largest files (a min-heap on size).
type fileHeap []File

func (h fileHeap) Len() int { return len(h) }
func (h fileHeap) Less(i, j int) bool {
	return h[i].Size < h[j].Size || (h[i].Size == h[j].Size && h[i].Path > h[j].Path)
}
func (h fileHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *fileHeap) Push(x any)   { *h = append(*h, x.(File)) }
func (h *fileHeap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}

func (h *fileHeap) offer(f File, n int) {
	if h.Len() < n {
		heap.Push(h, f)
		return
	}
	if (*h)[0].Size < f.Size {
		(*h)[0] = f
		heap.Fix(h, 0)
	}
}

func (h fileHeap) sorted() []File {
	out := append([]File(nil), h...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Size != out[j].Size {
			return out[i].Size > out[j].Size
		}
		return out[i].Path < out[j].Path
	})
	return out
}

type inodeKey struct{ dev, ino uint64 }

type scan struct {
	o       Options
	started time.Time
	mu      sync.Mutex

	res      Result // scalar totals, problems, other volumes
	families [len(famBytes{})]Agg
	cats     map[string]*Agg
	exts     map[string]*extAgg
	years    map[int]*yearAgg
	sizes    []SizeBucket
	largest  fileHeap
	byFam    map[string]*fileHeap
	trees    []*node
	links    map[inodeKey]bool
	sizeN    map[int64]int32 // files per size (duplicate candidates)
	alloc    bool
}

func newScan(o Options) *scan {
	s := &scan{o: o, started: time.Now(), cats: map[string]*Agg{}, exts: map[string]*extAgg{}, years: map[int]*yearAgg{},
		byFam: map[string]*fileHeap{}, links: map[inodeKey]bool{}, sizeN: map[int64]int32{}}
	s.sizes = append([]SizeBucket(nil), sizeBands...)
	s.res.Version, s.res.Roots, s.res.Started = FormatVersion, o.Roots, s.started
	for _, r := range o.Roots {
		s.trees = append(s.trees, &node{name: r, path: r})
		s.res.Volumes = append(s.res.Volumes, VolumeOf(r))
	}
	return s
}

func (s *scan) problem(path string, err error) {
	s.mu.Lock()
	s.res.Errors++
	if len(s.res.Problems) < maxProblems {
		s.res.Problems = append(s.res.Problems, Problem{Path: path, Err: err.Error()})
	}
	s.mu.Unlock()
}

// walk is the first pass: every file counted once.
func (s *scan) walk() error {
	stop := func() {}
	var prog Progress = nopProgress{}
	if s.o.ProgressFor != nil {
		if p, st := s.o.ProgressFor("scan"); p != nil {
			prog, stop = p, st
		}
	}
	defer stop()
	done := make(chan struct{})
	var tick sync.WaitGroup
	if s.o.OnSnapshot != nil {
		tick.Add(1)
		go func() {
			defer tick.Done()
			t := time.NewTicker(s.o.SnapshotEvery)
			defer t.Stop()
			for {
				select {
				case <-done:
					return
				case <-t.C:
					s.o.OnSnapshot(s.export(true))
				}
			}
		}()
	}
	defer func() { close(done); tick.Wait() }()
	for ri, root := range s.o.Roots {
		ri, root := ri, root
		if err := s.walkRoot(root, s.links, true, prog, func(path string, size int64, fi fs.FileInfo, sys fileSys) {
			s.count(ri, root, path, size, fi, sys)
		}); err != nil {
			return err
		}
		if s.o.Gate.Stopped() {
			break
		}
	}
	prog.TotalKnown()
	return nil
}

// walkRoot visits every regular file of root once (hard links: the first
// name seen in this pass only), on the root's volume when OneVolume, calling
// f concurrently. first: the counting pass (records the folders, other
// volumes and hard links).
func (s *scan) walkRoot(root string, seen map[inodeKey]bool, first bool, prog Progress, f func(path string, size int64, fi fs.FileInfo, sys fileSys)) error {
	rootDev, haveDev := devOf(root)
	var seenMu sync.Mutex
	conf := fastwalk.Config{NumWorkers: s.o.Workers}
	return fastwalk.Walk(&conf, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if first {
				s.problem(path, err)
			}
			prog.Err()
			return nil
		}
		if !s.o.Gate.Wait() {
			if d.IsDir() {
				return fastwalk.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if path == root {
				return nil
			}
			if s.o.OneVolume && haveDev {
				if dev, ok := devOf(path); ok && dev != rootDev {
					if !first {
						return fastwalk.SkipDir
					}
					s.mu.Lock()
					if len(s.res.OtherVolumes) < maxOtherVolumes {
						s.res.OtherVolumes = append(s.res.OtherVolumes, path)
					}
					s.mu.Unlock()
					return fastwalk.SkipDir
				}
			}
			if first {
				s.mu.Lock()
				s.res.Dirs++
				s.mu.Unlock()
			}
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			if first {
				s.mu.Lock()
				s.res.Symlinks++
				s.mu.Unlock()
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			if first {
				s.problem(path, err)
			}
			prog.Err()
			return nil
		}
		sys := sysOf(fi)
		if sys.links > 1 {
			k := inodeKey{sys.dev, sys.ino}
			seenMu.Lock()
			dup := seen[k]
			seen[k] = true
			seenMu.Unlock()
			if dup {
				if first {
					s.mu.Lock()
					s.res.HardLinks.add(fi.Size())
					s.mu.Unlock()
				}
				return nil
			}
		}
		t0 := time.Now()
		f(path, fi.Size(), fi, sys)
		prog.Item(fi.Size(), time.Since(t0))
		return nil
	})
}

// count adds one file to every breakdown.
func (s *scan) count(ri int, root, path string, size int64, fi fs.FileInfo, sys fileSys) {
	base := filepath.Base(path)
	ext := kinds.Ext(base)
	t, known := kinds.Lookup(ext)
	fam := kinds.Family(ext)
	fi0 := famIndex[fam]
	mt := fi.ModTime()
	year := mt.Year()
	if year < 1980 || mt.After(s.started.Add(48*time.Hour)) {
		year = 0
	}
	rel, _ := filepath.Rel(root, filepath.Dir(path))
	var comps []string
	if rel != "." && rel != "" {
		comps = strings.Split(rel, string(filepath.Separator))
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.res.Files++
	s.res.Bytes += size
	if sys.allocOK {
		s.alloc = true
		s.res.Allocated += sys.alloc
	}
	if size == 0 {
		s.res.Empty++
	}
	if kinds.IsJunk(base) {
		s.res.Junk.add(size)
	}
	online := fsx.OnlineOnly(fi)
	if online {
		s.res.OnlineOnly.add(size)
	}
	s.families[fi0].add(size)
	cat := t.Category
	if !known {
		cat = ""
	}
	ca := s.cats[cat]
	if ca == nil {
		ca = &Agg{}
		s.cats[cat] = ca
	}
	ca.add(size)
	ea := s.exts[ext]
	if ea == nil {
		ea = &extAgg{family: fam, category: cat, known: known}
		s.exts[ext] = ea
	}
	ea.add(size)
	ya := s.years[year]
	if ya == nil {
		ya = &yearAgg{}
		s.years[year] = ya
	}
	ya.add(size)
	ya.fam[fi0] += size
	for i := range s.sizes {
		if size >= s.sizes[i].Min && (s.sizes[i].Max == 0 || size < s.sizes[i].Max) {
			s.sizes[i].add(size)
			break
		}
	}
	f := File{Path: path, Size: size, Ext: ext, Family: fam, MTime: mt.Unix()}
	s.largest.offer(f, s.o.Top)
	h := s.byFam[fam]
	if h == nil {
		h = &fileHeap{}
		s.byFam[fam] = h
	}
	h.offer(f, largestPerFamily)
	n := s.trees[ri]
	n.agg.add(size)
	n.fam[fi0] += size
	cur := root
	for i, c := range comps {
		if i >= s.o.Depth {
			n.deeper = true
			break
		}
		cur = filepath.Join(cur, c)
		n = n.child(c, cur)
		n.agg.add(size)
		n.fam[fi0] += size
	}
	if size >= s.o.DupMin && (!online || s.o.ReadOnlineOnly) {
		s.sizeN[size]++
	}
}

// export builds the Result from the counters (partial: a live snapshot).
func (s *scan) export(partial bool) *Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.res
	r.Partial = partial
	r.Phase = "scan"
	r.Elapsed = time.Since(s.started).Seconds()
	r.Problems = append([]Problem(nil), s.res.Problems...)
	r.OtherVolumes = append([]string(nil), s.res.OtherVolumes...)
	if !s.alloc {
		r.Allocated = 0
	}
	for i, f := range kinds.Families {
		r.Families = append(r.Families, Bucket{Key: f, Agg: s.families[i]})
	}
	for c, a := range s.cats {
		key := c
		if key == "" {
			key = "unknown"
		}
		r.Categories = append(r.Categories, Bucket{Key: key, Family: kinds.FamilyOfCategory(c), Agg: *a})
	}
	sortBuckets(r.Categories)
	for e, a := range s.exts {
		b := Bucket{Key: e, Family: a.family, Category: a.category, Agg: a.Agg}
		if a.known {
			r.Exts = append(r.Exts, b)
		} else {
			r.Unknown = append(r.Unknown, b)
		}
	}
	sortBuckets(r.Exts)
	sortBuckets(r.Unknown)
	r.Exts = capBuckets(r.Exts, maxExts)
	r.Unknown = capBuckets(r.Unknown, maxUnknown)
	for y, a := range s.years {
		yb := YearBucket{Year: y, Agg: a.Agg, Families: famMap(a.fam)}
		r.Years = append(r.Years, yb)
	}
	sort.Slice(r.Years, func(i, j int) bool { return r.Years[i].Year < r.Years[j].Year })
	r.Sizes = append([]SizeBucket(nil), s.sizes...)
	r.Largest = s.largest.sorted()
	r.LargestBy = map[string][]File{}
	for f, h := range s.byFam {
		r.LargestBy[f] = h.sorted()
	}
	for _, t := range s.trees {
		r.Tree = append(r.Tree, exportNode(t))
	}
	return &r
}

func famMap(f famBytes) map[string]int64 {
	m := map[string]int64{}
	for i, b := range f {
		if b != 0 {
			m[kinds.Families[i]] = b
		}
	}
	return m
}

func exportNode(n *node) *Node {
	out := &Node{Name: n.name, Path: n.path, Files: n.agg.Files, Bytes: n.agg.Bytes, Families: famMap(n.fam), Deeper: n.deeper}
	kids := make([]*node, 0, len(n.children))
	for _, c := range n.children {
		kids = append(kids, c)
	}
	sort.Slice(kids, func(i, j int) bool {
		if kids[i].agg.Bytes != kids[j].agg.Bytes {
			return kids[i].agg.Bytes > kids[j].agg.Bytes
		}
		return kids[i].name < kids[j].name
	})
	for i, c := range kids {
		if i >= maxChildren {
			out.More++
			out.MoreFiles += c.agg.Files
			out.MoreBytes += c.agg.Bytes
			continue
		}
		out.Children = append(out.Children, exportNode(c))
	}
	return out
}

func sortBuckets(b []Bucket) {
	sort.Slice(b, func(i, j int) bool {
		if b[i].Bytes != b[j].Bytes {
			return b[i].Bytes > b[j].Bytes
		}
		if b[i].Files != b[j].Files {
			return b[i].Files > b[j].Files
		}
		return b[i].Key < b[j].Key
	})
}

func capBuckets(b []Bucket, n int) []Bucket {
	if len(b) > n {
		return b[:n]
	}
	return b
}

type nopProgress struct{}

func (nopProgress) AddTotal(int64)            {}
func (nopProgress) TotalKnown()               {}
func (nopProgress) Item(int64, time.Duration) {}
func (nopProgress) Advance(int64)             {}
func (nopProgress) Err()                      {}

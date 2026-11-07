// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package stats

import (
	"io/fs"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/mauriziofonte/onefold/internal/fsx"
	"github.com/mauriziofonte/onefold/internal/hashcache"
	"github.com/mauriziofonte/onefold/internal/kinds"
	"github.com/mauriziofonte/onefold/internal/runctl"
	"github.com/mauriziofonte/onefold/internal/sparsehash"
)

const (
	maxGroupPaths = 50  // paths listed per duplicate group
	maxPairGroup  = 64  // groups larger than this do not feed the folder pairs
	maxPairs      = 100 // folder pairs listed
	maxDupExts    = 50
)

type cand struct {
	path string
	key  string // hashcache identity ("" = not cached)
	size int64
	hash string
}

// duplicates is the second pass: the files that share their size with
// another are read (a sample, or whole in full mode) and grouped by content.
func (s *scan) duplicates() (*Dups, error) {
	d := &Dups{Mode: s.o.Dups, MinSize: s.o.DupMin}
	stop := func() {}
	var prog Progress = nopProgress{}
	if s.o.ProgressFor != nil {
		if p, st := s.o.ProgressFor("dups"); p != nil {
			prog, stop = p, st
		}
	}
	defer stop()

	// candidates: walk again, keep the files whose size is shared
	var cands []cand
	var mu sync.Mutex
	seen := map[inodeKey]bool{}
	for _, root := range s.o.Roots {
		err := s.walkRoot(root, seen, false, nopProgress{}, func(path string, size int64, fi fs.FileInfo, _ fileSys) {
			key := ""
			if s.o.Cache != nil {
				key = hashcache.KeyOf(path, fi)
			}
			if size < s.o.DupMin || (!s.o.ReadOnlineOnly && fsx.OnlineOnly(fi)) {
				return
			}
			s.mu.Lock()
			n := s.sizeN[size]
			s.mu.Unlock()
			if n < 2 {
				return
			}
			mu.Lock()
			cands = append(cands, cand{path: path, key: key, size: size})
			mu.Unlock()
		})
		if err != nil {
			return d, err
		}
	}
	// a size seen twice in the first pass may be unique now (a file gone)
	perSize := map[int64]int{}
	for _, c := range cands {
		perSize[c.size]++
	}
	kept := cands[:0]
	for _, c := range cands {
		if perSize[c.size] > 1 {
			kept = append(kept, c)
		}
	}
	cands = kept
	d.Candidates = int64(len(cands))
	for _, c := range cands {
		prog.AddTotal(c.size) // one item each
	}
	prog.TotalKnown()

	// sample digest of every candidate
	hits0 := s.o.Cache.Hits()
	errs := s.hashAll(cands, prog, func(c *cand) error {
		if h, _, ok := s.o.Cache.Sparse(c.key, 0, 0); ok {
			c.hash = h
			return nil
		}
		r, err := sparsehash.HashFile(c.path, 0, 0)
		c.hash = r.Hex
		if err == nil {
			s.o.Cache.PutSparse(c.key, 0, 0, r)
		}
		return err
	})
	d.Errors += errs
	if s.o.Gate.Stopped() {
		return d, runctl.ErrStopped
	}
	groups := group(cands)
	verified := false
	if s.o.Dups == DupsFull {
		var members []cand
		for _, g := range groups {
			members = append(members, g...)
		}
		errs := s.hashAll(members, nopProgress{}, func(c *cand) error {
			if h, ok := s.o.Cache.Full(c.key); ok {
				c.hash = h
				return nil
			}
			h, err := sparsehash.FullFile(c.path)
			c.hash = h
			if err == nil {
				s.o.Cache.PutFull(c.key, h)
			}
			return err
		})
		d.Errors += errs
		if s.o.Gate.Stopped() {
			return d, runctl.ErrStopped
		}
		groups = group(members)
		verified = true
	}
	summarize(d, groups, verified, s.o.Top)
	d.Cached = s.o.Cache.Hits() - hits0
	if s.o.OnGroup != nil && verified {
		for _, g := range groups {
			paths := make([]string, len(g))
			for i, c := range g {
				paths[i] = c.path
			}
			s.o.OnGroup(g[0].size, g[0].hash, paths)
		}
	}
	return d, nil
}

// hashAll runs f on every candidate in parallel; a failed one gets no hash.
func (s *scan) hashAll(cands []cand, prog Progress, f func(*cand) error) (errs int64) {
	jobs := make(chan int)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for w := 0; w < s.o.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				t0 := time.Now()
				if !s.o.Gate.Wait() {
					cands[i].hash = ""
					continue
				}
				if err := f(&cands[i]); err != nil {
					cands[i].hash = ""
					mu.Lock()
					errs++
					mu.Unlock()
					prog.Err()
					continue
				}
				prog.Item(cands[i].size, time.Since(t0))
			}
		}()
	}
	for i := range cands {
		if s.o.Gate.Stopped() {
			break
		}
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return errs
}

// group returns the groups of 2+ candidates with the same size and hash.
func group(cands []cand) [][]cand {
	type key struct {
		size int64
		hash string
	}
	by := map[key][]cand{}
	for _, c := range cands {
		if c.hash == "" {
			continue
		}
		k := key{c.size, c.hash}
		by[k] = append(by[k], c)
	}
	var out [][]cand
	for _, g := range by {
		if len(g) > 1 {
			sort.Slice(g, func(i, j int) bool { return g[i].path < g[j].path })
			out = append(out, g)
		}
	}
	return out
}

// summarize fills d from the groups.
func summarize(d *Dups, groups [][]cand, verified bool, top int) {
	byFam := map[string]*Bucket{}
	byExt := map[string]*Bucket{}
	type pairKey struct{ a, b string }
	pairs := map[pairKey]*DirPair{}
	all := make([]DupGroup, 0, len(groups))
	for _, g := range groups {
		n := int64(len(g))
		size := g[0].size
		wasted := (n - 1) * size
		ext := kinds.Ext(g[0].path)
		fam := kinds.Family(ext)
		d.Groups++
		d.Files += n
		d.Extra += n - 1
		d.Wasted += wasted
		for _, m := range []struct {
			by  map[string]*Bucket
			key string
		}{{byFam, fam}, {byExt, ext}} {
			b := m.by[m.key]
			if b == nil {
				b = &Bucket{Key: m.key, Family: fam}
				m.by[m.key] = b
			}
			b.Files += n
			b.Bytes += n * size
			b.Wasted += wasted
		}
		dg := DupGroup{Size: size, Hash: g[0].hash, Count: len(g), Wasted: wasted, Ext: ext, Family: fam, Verified: verified}
		for i, c := range g {
			if i >= maxGroupPaths {
				break
			}
			dg.Paths = append(dg.Paths, c.path)
		}
		all = append(all, dg)
		if len(g) > maxPairGroup {
			continue
		}
		dirs := map[string]int{}
		for _, c := range g {
			dirs[filepath.Dir(c.path)]++
		}
		var ds []string
		for k, c := range dirs {
			ds = append(ds, k)
			if c > 1 { // copies inside one folder
				p := pairs[pairKey{k, k}]
				if p == nil {
					p = &DirPair{A: k, B: k}
					pairs[pairKey{k, k}] = p
				}
				p.Files += int64(c - 1)
				p.Bytes += int64(c-1) * size
			}
		}
		sort.Strings(ds)
		for i := 0; i < len(ds); i++ {
			for j := i + 1; j < len(ds); j++ {
				k := pairKey{ds[i], ds[j]}
				p := pairs[k]
				if p == nil {
					p = &DirPair{A: ds[i], B: ds[j]}
					pairs[k] = p
				}
				p.Files++
				p.Bytes += size
			}
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].Wasted != all[j].Wasted {
			return all[i].Wasted > all[j].Wasted
		}
		return all[i].Hash < all[j].Hash
	})
	if len(all) > 2*top {
		all = all[:2*top]
	}
	d.Top = all
	for _, f := range kinds.Families {
		if b := byFam[f]; b != nil {
			d.ByFamily = append(d.ByFamily, *b)
		}
	}
	for _, b := range byExt {
		d.ByExt = append(d.ByExt, *b)
	}
	sort.Slice(d.ByExt, func(i, j int) bool {
		if d.ByExt[i].Wasted != d.ByExt[j].Wasted {
			return d.ByExt[i].Wasted > d.ByExt[j].Wasted
		}
		return d.ByExt[i].Key < d.ByExt[j].Key
	})
	d.ByExt = capBuckets(d.ByExt, maxDupExts)
	for _, p := range pairs {
		d.Pairs = append(d.Pairs, *p)
	}
	// folders sharing several files first: one shared file is a coincidence
	// more often than a copied folder
	sort.Slice(d.Pairs, func(i, j int) bool {
		if si, sj := d.Pairs[i].Files > 1, d.Pairs[j].Files > 1; si != sj {
			return si
		}
		if d.Pairs[i].Bytes != d.Pairs[j].Bytes {
			return d.Pairs[i].Bytes > d.Pairs[j].Bytes
		}
		return d.Pairs[i].A+d.Pairs[i].B < d.Pairs[j].A+d.Pairs[j].B
	})
	if len(d.Pairs) > maxPairs {
		d.Pairs = d.Pairs[:maxPairs]
	}
	if d.ByFamily == nil {
		d.ByFamily = []Bucket{}
	}
	if d.ByExt == nil {
		d.ByExt = []Bucket{}
	}
	if d.Top == nil {
		d.Top = []DupGroup{}
	}
	if d.Pairs == nil {
		d.Pairs = []DirPair{}
	}
}

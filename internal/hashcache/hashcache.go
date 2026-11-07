// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package hashcache remembers the digests of files across commands and runs,
// in the state dir: a file hashed by `stats` is not read again by a later
// `stats`, `catalog`, `merge` or `rearrange` while it has not changed.
//
// A file is known by its identity and its change marks (KeyOf): on Linux and
// macOS the device, inode, size, modification and status-change times (any
// write changes the status-change time, so a changed file never matches an
// old entry); on Windows the same from the file itself (volume serial, file
// ID, size, last-write and change times; NTFS and ReFS only, FAT and exFAT
// keep no change time and are not cached).
//
// What it is trusted for, and what not:
//   - a sampled digest (sparsehash, not the whole content) feeds grouping:
//     catalog records, stats duplicate candidates;
//   - a whole-content digest only feeds reports (stats -dups full);
//   - nothing deletes, skips a copy or decides an archive name on a cached
//     digest alone: full-content verification and every deletion read the
//     bytes again.
//
// The cache is best-effort: unreadable lines are skipped, a cache that
// cannot be opened or written is no cache (never an error of the command),
// ONEFOLD_NO_HASH_CACHE=1 turns it off.
package hashcache

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/mauriziofonte/onefold/internal/sparsehash"
	"github.com/mauriziofonte/onefold/internal/statedir"
)

// FileName is the cache file in the state dir.
const FileName = "hashes.tsv"

const header = "onefold-hashes\t1"

// MaxEntries bounds the cache (about 150 bytes each on disk); the entries
// set or read least recently go first when it is rewritten.
var MaxEntries = 2_000_000

// entry is what is known about one file version.
type entry struct {
	params string // "coverage/sample/blocks" of sparse
	sparse string // sampled digest (sparsehash.Result.Hex)
	full   string // whole-content digest (sparsehash.FullFile)
	seq    int64  // recency: the line or the use that set it
}

// Cache is safe for concurrent use; a nil *Cache is a cache that knows
// nothing and stores nothing.
type Cache struct {
	mu    sync.Mutex
	path  string
	m     map[string]*entry
	f     *os.File
	w     *bufio.Writer
	lines int64
	seq   int64
	hits  int64
}

// Open opens the cache of the state dir; nil when it is turned off or cannot
// be opened.
func Open() *Cache {
	d, err := statedir.Dir()
	if err != nil {
		return nil
	}
	return OpenIn(d)
}

// OpenIn opens the cache of the state dir dir (the GUI's -state); nil when it
// is turned off or cannot be opened.
func OpenIn(dir string) *Cache {
	if os.Getenv("ONEFOLD_NO_HASH_CACHE") == "1" {
		return nil
	}
	c, err := OpenAt(filepath.Join(dir, FileName))
	if err != nil {
		return nil
	}
	return c
}

// OpenAt opens (or creates) the cache file at path.
func OpenAt(path string) (*Cache, error) {
	if err := statedir.Ensure(filepath.Dir(path)); err != nil {
		return nil, err
	}
	c := &Cache{path: path, m: map[string]*entry{}}
	if f, err := os.Open(path); err == nil {
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		first := true
		for sc.Scan() {
			line := sc.Text()
			if first {
				first = false
				if line != header {
					break // another format: start over
				}
				continue
			}
			c.lines++
			p := strings.Split(line, "\t")
			if len(p) != 4 || p[0] == "" {
				continue
			}
			c.seq++
			e := c.m[p[0]]
			if e == nil {
				e = &entry{}
				c.m[p[0]] = e
			}
			if p[2] != "" {
				e.params, e.sparse = p[1], p[2]
			}
			if p[3] != "" {
				e.full = p[3]
			}
			e.seq = c.seq
		}
		f.Close()
		if first || c.lines == 0 && len(c.m) == 0 {
			os.Remove(path) // empty or another format: rewritten below
		}
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, statedir.FileMode)
	if err != nil {
		return nil, err
	}
	if fi, err := f.Stat(); err == nil && fi.Size() == 0 {
		if _, err := f.WriteString(header + "\n"); err != nil {
			f.Close()
			return nil, err
		}
	}
	c.f, c.w = f, bufio.NewWriterSize(f, 256<<10)
	return c, nil
}

func params(coverage, sample int) string {
	if coverage <= 0 {
		coverage = sparsehash.DefaultCoverage
	}
	if sample <= 0 {
		sample = sparsehash.DefaultSampleSize
	}
	return fmt.Sprintf("%d/%d/%d", coverage, sample, sparsehash.MaxBlocks) // -blocks changes the digest too
}

// Sparse returns the digest sparsehash.HashFile gave for key with these
// parameters; full = that digest read the whole file (a small or text file).
func (c *Cache) Sparse(key string, coverage, sample int) (hex string, full, ok bool) {
	if c == nil || key == "" {
		return "", false, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.m[key]
	if e == nil || e.sparse == "" || e.params != params(coverage, sample) {
		return "", false, false
	}
	c.seq++
	e.seq = c.seq
	c.hits++
	return e.sparse, e.full == e.sparse, true
}

// Sampled is Sparse for a digest that sampled the file (not read whole):
// what catalog may take from the cache.
func (c *Cache) Sampled(key string, coverage, sample int) (string, bool) {
	if c == nil || key == "" {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.m[key]
	if e == nil || e.sparse == "" || e.params != params(coverage, sample) || e.full == e.sparse {
		return "", false
	}
	c.seq++
	e.seq = c.seq
	c.hits++
	return e.sparse, true
}

// Full returns the whole-content digest recorded for key.
func (c *Cache) Full(key string) (string, bool) {
	if c == nil || key == "" {
		return "", false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.m[key]
	if e == nil || e.full == "" {
		return "", false
	}
	c.seq++
	e.seq = c.seq
	c.hits++
	return e.full, true
}

// PutSparse records a sampled digest; a digest that read the whole file
// (Result.Full) is recorded as the whole-content one too.
func (c *Cache) PutSparse(key string, coverage, sample int, r sparsehash.Result) {
	full := ""
	if r.Full {
		full = r.Hex
	}
	c.put(key, params(coverage, sample), r.Hex, full)
}

// PutFull records a whole-content digest.
func (c *Cache) PutFull(key, hex string) { c.put(key, "", "", hex) }

func (c *Cache) put(key, prm, sparse, full string) {
	if c == nil || key == "" || strings.ContainsAny(key, "\t\n") {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.m[key]
	if e == nil {
		e = &entry{}
		c.m[key] = e
	}
	if (sparse == "" || e.sparse == sparse && e.params == prm) && (full == "" || e.full == full) {
		return // known already
	}
	if sparse != "" {
		e.params, e.sparse = prm, sparse
	}
	if full != "" {
		e.full = full
	}
	c.seq++
	e.seq = c.seq
	c.lines++
	fmt.Fprintf(c.w, "%s\t%s\t%s\t%s\n", key, prm, sparse, full)
}

// Hits counts the digests served from the cache since Open.
func (c *Cache) Hits() int64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits
}

// Close writes what is pending; a file grown well past its entries (the same
// files hashed again and again) or past MaxEntries is rewritten with the
// most recent ones. Another onefold writing at the same time can lose its
// last lines then: a cache miss later, nothing else.
func (c *Cache) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	err := c.w.Flush()
	if cerr := c.f.Close(); err == nil {
		err = cerr
	}
	if err != nil || (c.lines <= 2*int64(len(c.m))+10000 && len(c.m) <= MaxEntries) {
		return err
	}
	keys := make([]string, 0, len(c.m))
	for k := range c.m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return c.m[keys[i]].seq > c.m[keys[j]].seq })
	if len(keys) > MaxEntries {
		keys = keys[:MaxEntries]
	}
	tmp := c.path + ".onefold-tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, statedir.FileMode)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(f, 1<<20)
	fmt.Fprintln(w, header)
	for i := len(keys) - 1; i >= 0; i-- { // oldest first: recency survives the next load
		e := c.m[keys[i]]
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", keys[i], e.params, e.sparse, e.full)
	}
	if err = w.Flush(); err == nil {
		err = f.Close()
	} else {
		f.Close()
	}
	if err == nil {
		err = os.Rename(tmp, c.path) // our own file: replacing it is the point
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// KeyOf is the identity of the file version fi describes ("" = none known:
// the file is not cached).
func KeyOf(path string, fi fs.FileInfo) string {
	if fi == nil || !fi.Mode().IsRegular() {
		return ""
	}
	return keyOf(path, fi)
}

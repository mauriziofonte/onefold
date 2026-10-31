// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package backup is the cold-storage profile: a tree (typically the golden)
// mirrored onto a disk that is then put away, with the same relative paths,
// and audited later. A run only adds files (never replaces or deletes), each
// copy is verified like a merge copy, and the run writes a manifest of the
// content hash of every file into <dst>/.onefold; Check reads every file of
// the copy again against those manifests (bit rot, missing or changed files).
package backup

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mauriziofonte/onefold/internal/fsx"
	"github.com/mauriziofonte/onefold/internal/merge"
	"github.com/mauriziofonte/onefold/internal/runctl"
	"github.com/mauriziofonte/onefold/internal/sparsehash"
)

// StateDir is the folder of the manifests inside the copy.
const StateDir = ".onefold"

const manifestPrefix = "backup-"

// Entry is one file of a backup manifest.
type Entry struct {
	Rel     string `json:"rel"` // slash-separated path inside the tree
	Size    int64  `json:"size"`
	Content string `json:"content"` // full content hash (XXH128)
}

// Result of a Run.
type Result struct {
	Copied      int      `json:"copied"`
	CopiedBytes int64    `json:"copiedBytes"`
	Present     int      `json:"present"`             // same size already in the copy: kept, hashed from the source
	Conflicts   []string `json:"conflicts,omitempty"` // a different file under the same path in the copy: never replaced
	Failed      []string `json:"failed,omitempty"`
	Manifest    string   `json:"manifest,omitempty"`
	Pending     int      `json:"pending,omitempty"` // not done: stopped
}

// Options of Run.
type Options struct {
	Execute bool // false = dry run: nothing written
	Workers int
	Gate    *runctl.Gate
}

type file struct {
	rel  string
	path string
	size int64
}

// Run mirrors src into dst. Read-only on src; in dst it only adds files and
// its manifest.
func Run(src, dst string, o Options) (*Result, error) {
	src, dst = filepath.Clean(src), filepath.Clean(dst)
	if within(dst, src) || within(src, dst) {
		return nil, fmt.Errorf("the copy and the tree must not contain each other")
	}
	if o.Workers <= 0 {
		o.Workers = max(1, runtime.NumCPU()/2)
	}
	var files []file
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == StateDir && p != src {
				return filepath.SkipDir // onefold state of the tree itself
			}
			return nil
		}
		if !d.Type().IsRegular() || fsx.IsTemp(d.Name()) {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		files = append(files, file{rel: filepath.ToSlash(rel), path: p, size: fi.Size()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].rel < files[j].rel })
	res := &Result{}
	var entries []Entry
	var mu sync.Mutex
	var wg sync.WaitGroup
	jobs := make(chan file)
	for w := 0; w < o.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 4<<20)
			for f := range jobs {
				if !o.Gate.Wait() {
					mu.Lock()
					res.Pending++
					mu.Unlock()
					continue
				}
				e, state, err := one(f, dst, o.Execute, buf)
				mu.Lock()
				switch {
				case err != nil:
					res.Failed = append(res.Failed, f.rel+": "+err.Error())
				case state == "conflict":
					res.Conflicts = append(res.Conflicts, f.rel)
				case state == "present":
					res.Present++
					entries = append(entries, e)
				default:
					res.Copied++
					res.CopiedBytes += f.size
					entries = append(entries, e)
				}
				mu.Unlock()
			}
		}()
	}
	for _, f := range files {
		jobs <- f
	}
	close(jobs)
	wg.Wait()
	sort.Strings(res.Conflicts)
	sort.Strings(res.Failed)
	if !o.Execute {
		return res, nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Rel < entries[j].Rel })
	p, err := writeManifest(dst, entries)
	if err != nil {
		return res, err
	}
	res.Manifest = p
	return res, nil
}

// one copies (or, dry, classifies) one file.
func one(f file, dst string, execute bool, buf []byte) (Entry, string, error) {
	to := filepath.Join(dst, filepath.FromSlash(f.rel))
	if fi, err := os.Lstat(to); err == nil {
		if !fi.Mode().IsRegular() || fi.Size() != f.size {
			return Entry{}, "conflict", nil
		}
		if !execute {
			return Entry{Rel: f.rel, Size: f.size}, "present", nil
		}
		h, err := sparsehash.FullFile(f.path)
		return Entry{Rel: f.rel, Size: f.size, Content: h}, "present", err
	}
	if !execute {
		return Entry{Rel: f.rel, Size: f.size}, "copy", nil
	}
	h, err := merge.CopyVerified(f.path, to, f.size, buf)
	if errors.Is(err, fsx.ErrExists) {
		return Entry{}, "conflict", nil
	}
	return Entry{Rel: f.rel, Size: f.size, Content: h}, "copy", err
}

func writeManifest(dst string, es []Entry) (string, error) {
	dir := filepath.Join(dst, StateDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, manifestPrefix+time.Now().Format("20060102-150405.000000000")+".jsonl")
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	for i := range es {
		if err := enc.Encode(&es[i]); err != nil {
			return "", err
		}
	}
	tmp := fsx.TempFor(p, false)
	if err := os.WriteFile(tmp, b.Bytes(), 0o644); err != nil {
		return "", err
	}
	f, err := os.OpenFile(tmp, os.O_RDWR, 0)
	if err == nil {
		err = f.Sync()
		f.Close()
	}
	if err == nil {
		err = fsx.RenameNoReplace(tmp, p)
	}
	if err != nil {
		os.Remove(tmp)
		return "", err
	}
	return p, nil
}

// Audit of a Check.
type Audit struct {
	Manifests int      `json:"manifests"`
	OK        int      `json:"ok"`
	Bytes     int64    `json:"bytes"`
	Missing   []string `json:"missing,omitempty"`
	Changed   []string `json:"changed,omitempty"` // content differs from the manifest: bit rot or an edit
	Failed    []string `json:"failed,omitempty"`  // unreadable
	Untracked []string `json:"untracked,omitempty"`
}

// Clean reports whether nothing is missing, changed or unreadable.
func (a *Audit) Clean() bool { return len(a.Missing)+len(a.Changed)+len(a.Failed) == 0 }

// Check reads every file of the copy at dst again and compares it with the
// manifests of every run (a later run's entry wins). Read-only.
func Check(dst string, workers int, gate *runctl.Gate) (*Audit, error) {
	dst = filepath.Clean(dst)
	want, n, err := loadManifests(dst)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, fmt.Errorf("%s: no backup manifest in %s", dst, StateDir)
	}
	if workers <= 0 {
		workers = max(1, runtime.NumCPU()/2)
	}
	a := &Audit{Manifests: n}
	rels := make([]string, 0, len(want))
	for r := range want {
		rels = append(rels, r)
	}
	sort.Strings(rels)
	var mu sync.Mutex
	var wg sync.WaitGroup
	jobs := make(chan string)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for rel := range jobs {
				if !gate.Wait() {
					continue
				}
				e := want[rel]
				p := filepath.Join(dst, filepath.FromSlash(rel))
				var verdict *[]string
				h, err := sparsehash.FullFile(p)
				switch {
				case errors.Is(err, fs.ErrNotExist):
					verdict = &a.Missing
				case err != nil:
					verdict = &a.Failed
				case h != e.Content:
					verdict = &a.Changed
				}
				mu.Lock()
				if verdict != nil {
					*verdict = append(*verdict, rel)
				} else {
					a.OK++
					a.Bytes += e.Size
				}
				mu.Unlock()
			}
		}()
	}
	for _, r := range rels {
		jobs <- r
	}
	close(jobs)
	wg.Wait()
	if gate.Stopped() {
		return nil, runctl.ErrStopped
	}
	_ = filepath.WalkDir(dst, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == StateDir {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(dst, p)
		if _, ok := want[filepath.ToSlash(rel)]; !ok && d.Type().IsRegular() {
			a.Untracked = append(a.Untracked, filepath.ToSlash(rel))
		}
		return nil
	})
	for _, l := range []*[]string{&a.Missing, &a.Changed, &a.Failed, &a.Untracked} {
		sort.Strings(*l)
	}
	return a, nil
}

// loadManifests merges the manifests of dst in run order.
func loadManifests(dst string) (map[string]Entry, int, error) {
	dir := filepath.Join(dst, StateDir)
	ents, err := os.ReadDir(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, 0, err
	}
	var names []string
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), manifestPrefix) && !fsx.IsTemp(e.Name()) && !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names) // the time stamp in the name orders the runs
	want := map[string]Entry{}
	for _, n := range names {
		f, err := os.Open(filepath.Join(dir, n))
		if err != nil {
			return nil, 0, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for line := 1; sc.Scan(); line++ {
			b := bytes.TrimSpace(sc.Bytes())
			if len(b) == 0 {
				continue
			}
			var e Entry
			if err := json.Unmarshal(b, &e); err != nil || e.Rel == "" || e.Content == "" {
				f.Close()
				return nil, 0, fmt.Errorf("%s line %d: not a backup entry", n, line)
			}
			want[e.Rel] = e
		}
		err = sc.Err()
		f.Close()
		if err != nil {
			return nil, 0, err
		}
	}
	return want, len(names), nil
}

// within reports whether p is dir or inside it.
func within(p, dir string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

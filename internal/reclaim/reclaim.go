// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package reclaim helps a person free the space of the drops of an applied
// plan without deleting anything itself: every exact-duplicate drop is read
// again in full and compared with its winner, the golden copy must exist,
// and what passes is written into a script the person runs (or not). Other
// drops (lower resolution, smaller, variants) are distinct content: they are
// listed for review, never put in the script.
package reclaim

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/mauriziofonte/onefold/internal/plan"
	"github.com/mauriziofonte/onefold/internal/runctl"
	"github.com/mauriziofonte/onefold/internal/sparsehash"
)

// Item is one drop.
type Item struct {
	Host   string `json:"host"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Reason string `json:"reason"` // drop reason of the plan
	Winner string `json:"winner"` // source of the golden asset
	Golden string `json:"golden"` // canonRel
	Why    string `json:"why,omitempty"`
}

// Result of a Check.
type Result struct {
	Safe      []Item `json:"safe"`      // exact duplicates verified now: the script deletes them
	SafeBytes int64  `json:"safeBytes"` // their size
	Review    []Item `json:"review"`    // other drops: distinct content, a person decides
	Skipped   []Item `json:"skipped"`   // exact duplicates not verified (Why)
}

// Options of Check.
type Options struct {
	Target   string // golden tree root: the asset must be there
	OnlyHost string // drops of this host only (the files must be readable here)
	Workers  int
	Gate     *runctl.Gate
}

// Check verifies the drops of acts. Read-only on every tree.
func Check(acts []plan.Action, o Options) (*Result, error) {
	if o.Target == "" {
		return nil, fmt.Errorf("the golden tree root is required")
	}
	if o.Workers <= 0 {
		o.Workers = runtime.NumCPU()
	}
	res := &Result{}
	type job struct {
		a plan.Action
		d plan.Drop
	}
	var jobs []job
	for _, a := range acts {
		for _, d := range a.Drops {
			if o.OnlyHost != "" && d.Host != o.OnlyHost {
				continue
			}
			it := Item{Host: d.Host, Path: d.Path, Size: d.Size, Reason: d.Reason, Winner: a.SrcPath, Golden: a.CanonRel}
			if d.Reason != "exact-dupe" {
				res.Review = append(res.Review, it)
				continue
			}
			jobs = append(jobs, job{a, d})
		}
	}
	var mu sync.Mutex
	var wg sync.WaitGroup
	ch := make(chan job)
	full := map[string]string{} // winner path -> full hash (shared by its drops)
	winnerHash := func(p string) (string, error) {
		mu.Lock()
		h, ok := full[p]
		mu.Unlock()
		if ok {
			return h, nil
		}
		h, err := sparsehash.FullFile(p)
		if err == nil {
			mu.Lock()
			full[p] = h
			mu.Unlock()
		}
		return h, err
	}
	for w := 0; w < o.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				it := Item{Host: j.d.Host, Path: j.d.Path, Size: j.d.Size, Reason: j.d.Reason, Winner: j.a.SrcPath, Golden: j.a.CanonRel}
				if !o.Gate.Wait() {
					it.Why = "stopped"
				} else {
					it.Why = verify(j.a, j.d, o.Target, winnerHash)
				}
				mu.Lock()
				if it.Why == "" {
					res.Safe = append(res.Safe, it)
					res.SafeBytes += it.Size
				} else {
					res.Skipped = append(res.Skipped, it)
				}
				mu.Unlock()
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()
	for _, l := range [][]Item{res.Safe, res.Review, res.Skipped} {
		sort.Slice(l, func(i, k int) bool { return l[i].Host+l[i].Path < l[k].Host+l[k].Path })
	}
	return res, nil
}

// verify returns "" when the drop is safe to delete, else why not.
func verify(a plan.Action, d plan.Drop, target string, winnerHash func(string) (string, error)) string {
	if d.Size == 0 {
		return "empty file: identified by its name, never by content"
	}
	if d.Host != a.SrcHost {
		return "its winner is on another host (" + a.SrcHost + "): run where both are readable"
	}
	if filepath.Clean(d.Path) == filepath.Clean(a.SrcPath) {
		return "same file as its winner"
	}
	if _, err := os.Stat(filepath.Join(target, filepath.FromSlash(a.CanonRel))); err != nil {
		return "the golden copy is not in the target"
	}
	fi, err := os.Lstat(d.Path)
	if err != nil {
		return "unreadable: " + err.Error()
	}
	if !fi.Mode().IsRegular() {
		return "not a regular file"
	}
	if fi.Size() != d.Size {
		return "size changed since the plan"
	}
	wh, err := winnerHash(a.SrcPath)
	if err != nil {
		return "winner unreadable: " + err.Error()
	}
	dh, err := sparsehash.FullFile(d.Path)
	if err != nil {
		return "unreadable: " + err.Error()
	}
	if dh != wh {
		return "content differs from its winner: NOT a duplicate"
	}
	return ""
}

// Script writes the commands that delete the safe drops: PowerShell when
// ps is true (Remove-Item -LiteralPath), else POSIX sh (rm --). Every path
// is quoted for the shell; the script stops at the first failure.
func Script(w io.Writer, safe []Item, ps bool) error {
	var b strings.Builder
	if ps {
		b.WriteString("# onefold reclaim: exact duplicates verified against their winner; review before running\n$ErrorActionPreference = 'Stop'\n")
		for _, it := range safe {
			fmt.Fprintf(&b, "Remove-Item -LiteralPath '%s'  # = %s\n", strings.ReplaceAll(it.Path, "'", "''"), oneLine(it.Golden))
		}
	} else {
		b.WriteString("#!/bin/sh\n# onefold reclaim: exact duplicates verified against their winner; review before running\nset -e\n")
		for _, it := range safe {
			fmt.Fprintf(&b, "rm -- '%s'  # = %s\n", strings.ReplaceAll(it.Path, "'", `'\''`), oneLine(it.Golden))
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// oneLine keeps a comment on its line whatever the name holds.
func oneLine(s string) string { return strings.NewReplacer("\n", " ", "\r", " ").Replace(s) }

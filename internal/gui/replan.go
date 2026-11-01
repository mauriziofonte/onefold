// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package gui

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/merge"
	"github.com/mauriziofonte/onefold/internal/rearrange"
)

// ExcludeReq sets the user exclusions of a previewed merge or rearrange job
// : the whole new sets of exact paths and patterns; ContentOf adds every
// file holding the same content as that path (its copy and its duplicates).
type ExcludeReq struct {
	Paths     []string `json:"paths"`
	Globs     []string `json:"globs"`
	ContentOf string   `json:"contentOf,omitempty"`
}

// previewExcl is the user exclusions a preview walked with: what they left
// out was never cataloged, so removing one of them needs a new preview.
type previewExcl struct {
	Paths []string `json:"paths,omitempty"`
	Globs []string `json:"globs,omitempty"`
}

// errNeedsPreview: the change cannot be re-planned on the cataloged records.
var errNeedsPreview = errors.New("these files were never scanned: run a new preview")

// excludedBy counts excluded entries per reason ("rule" or "rule/dir").
func excludedBy(items []Item) map[string]int {
	out := map[string]int{}
	for _, it := range items {
		out[strings.TrimSuffix(it.Note, "/dir")]++
	}
	return out
}

func uniqStrings(in []string, clean func(string) string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		if clean != nil {
			s = clean(s)
		}
		if k := foldPath(s); !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}
	return out
}

// foldPath compares paths the way the OS does (case-insensitive on Windows).
func foldPath(s string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(filepath.Clean(s))
	}
	return filepath.Clean(s)
}

// contentMembers returns the paths holding the content of p in a preview's
// lists: the file to copy (or the keeper) and every duplicate of it.
func contentMembers(lists map[string][]Item, p string, abs func(string) string) []string {
	winner := ""
	for _, l := range []string{"copies", "inputDupes", "deletes"} {
		for _, it := range lists[l] {
			src := abs(it.Src)
			if foldPath(src) != foldPath(p) {
				continue
			}
			winner = src
			if l != "copies" {
				winner = abs(it.Dst)
			}
		}
	}
	if winner == "" {
		return []string{p}
	}
	out := []string{winner}
	for _, l := range []string{"inputDupes", "deletes"} {
		for _, it := range lists[l] {
			if foldPath(abs(it.Dst)) == foldPath(winner) {
				out = append(out, abs(it.Src))
			}
		}
	}
	return out
}

// Exclude applies new user exclusions to a previewed merge or rearrange job
// and re-plans it at once on the records its preview cataloged: no
// walk, no hashing. The new plan becomes the confirmed one.
func (m *Manager) Exclude(id string, q ExcludeReq) (*Job, error) {
	m.mu.Lock()
	j, ok := m.jobs[id]
	switch {
	case !ok:
		m.mu.Unlock()
		return nil, errNotFound
	case m.active != nil:
		m.mu.Unlock()
		return nil, errBusy
	case (j.Kind != "merge" && j.Kind != "rearrange") || j.Status != stPreviewed || j.Stage != "preview":
		m.mu.Unlock()
		return nil, errBadStatus
	}
	m.active = j // no other job starts while the plan changes
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.active = nil
		m.mu.Unlock()
	}()

	var prev result
	if err := m.st.loadResult(id, &prev); err != nil {
		return nil, errNeedsPreview
	}
	recs, err := catalog.LoadManifest(m.st.cachePath(id))
	if err != nil {
		return nil, errNeedsPreview
	}
	t0 := time.Now()
	var f *catalog.Filters
	var mp MergeParams
	var rp RearrangeParams
	abs := func(s string) string { return s }
	if j.Kind == "merge" {
		if err := json.Unmarshal(j.Params, &mp); err != nil {
			return nil, err
		}
		f = &mp.Filters
	} else {
		if err := json.Unmarshal(j.Params, &rp); err != nil {
			return nil, err
		}
		f = &rp.Filters
		root, _ := filepath.Abs(rp.Root)
		abs = func(s string) string {
			if filepath.IsAbs(s) {
				return s
			}
			return filepath.Join(root, filepath.FromSlash(s))
		}
	}
	paths := q.Paths
	if q.ContentOf != "" {
		paths = append(paths, contentMembers(prev.Lists, canonicalFile(abs(q.ContentOf)), abs)...)
	}
	paths = uniqStrings(paths, filepath.Clean)
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			return nil, fmt.Errorf("exclusion %q is not an absolute path", p)
		}
	}
	globs := uniqStrings(q.Globs, nil)
	if _, err := catalog.CompileGlobs(globs); err != nil {
		return nil, err
	}
	// an exclusion the preview walked with hid files that were never scanned
	keep := map[string]bool{}
	for _, p := range paths {
		keep["p"+foldPath(p)] = true
	}
	for _, g := range globs {
		keep["g"+g] = true
	}
	for _, p := range prev.PreviewExcl.Paths {
		if !keep["p"+foldPath(p)] {
			return nil, errNeedsPreview
		}
	}
	for _, g := range prev.PreviewExcl.Globs {
		if !keep["g"+g] {
			return nil, errNeedsPreview
		}
	}
	f.ExcludePaths, f.ExcludeGlobs = paths, globs

	var sum map[string]any
	var lists map[string][]Item
	var fp []string
	if j.Kind == "merge" {
		o, err := mp.Options(false)
		if err != nil {
			return nil, err
		}
		o.Records, o.StateCache, o.Packages = recs, m.st.cachePath(id), prev.WalkPackages
		r, err := merge.Run(o)
		if err != nil {
			return nil, err
		}
		sum, lists, fp = mergeSummary(r), mergeLists(r), mergeFingerprint(r)
	} else {
		o, err := rp.Options(false)
		if err != nil {
			return nil, err
		}
		o.Records, o.StateCache, o.Packages = recs, m.st.cachePath(id), prev.WalkPackages
		r, err := rearrange.Run(o)
		if err != nil {
			return nil, err
		}
		sum, lists, fp = rearrangeSummary(r), rearrangeItems(r), rearrangeFingerprint(r)
	}
	lists["excluded"] = append(append([]Item(nil), prev.WalkExcluded...), lists["excluded"]...)
	sort.SliceStable(lists["excluded"], func(a, b int) bool { return lists["excluded"][a].Src < lists["excluded"][b].Src })
	for _, k := range []string{"skippedByFilters", "emptyInputs", "skipped"} {
		if v, ok := j.Summary[k]; ok {
			sum[k] = v
		}
	}
	sum["excluded"], sum["excludedBy"] = len(lists["excluded"]), excludedBy(lists["excluded"])
	prev.Fingerprint, prev.Lists = fp, lists
	if err := m.st.saveResult(id, prev); err != nil {
		return nil, err
	}
	params := any(mp)
	if j.Kind == "rearrange" {
		params = rp
	}
	b, _ := json.Marshal(params)
	m.mu.Lock()
	j.Params, j.Summary, j.Updated = b, sum, time.Now()
	m.st.saveJob(j)
	snap := j.snapshot()
	m.mu.Unlock()
	m.log(j, LogEntry{Level: "info", Key: "log.replanned", Args: map[string]any{"paths": len(paths), "globs": len(globs),
		"ms": time.Since(t0).Milliseconds()}})
	m.broadcastJob(j)
	return snap, nil
}

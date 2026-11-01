// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package naming

import (
	"path"
	"sort"

	"github.com/mauriziofonte/onefold/internal/names"
)

// Item is one file to place: its current relative path (slash, "" when it
// comes from outside the tree, e.g. a merge copy), its rendered target and a
// deterministic tie-breaker (content hash + path).
type Item struct {
	Current string
	Target  string
	Tie     string
}

// Assign resolves collisions with the onefold convention: <stem>_NN<.ext>.
// Keys are compared case-insensitively (NTFS). taken holds lowercased
// relative paths that are NOT available (files that stay but are not items,
// directories). Idempotency: an item whose current path is its target, or
// "target_NN" of its own target, keeps it; everyone else gets the first free
// name (target, target_01, target_02, ...) in deterministic order.
func Assign(items []Item, taken map[string]bool) []string {
	out := make([]string, len(items))
	reserved := map[string]bool{}
	for k := range taken {
		reserved[k] = true
	}
	order := make([]int, len(items))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		ia, ib := items[order[a]], items[order[b]]
		ta, tb := names.FoldKey(ia.Target), names.FoldKey(ib.Target)
		if ta != tb {
			return ta < tb
		}
		return ia.Tie < ib.Tie
	})
	// 1. keepers
	done := make([]bool, len(items))
	for _, i := range order {
		it := items[i]
		if it.Current == "" || it.Target == "" {
			continue
		}
		if reserved[names.FoldKey(it.Current)] {
			continue
		}
		// exact (case-sensitive) match: a case-only difference is a rename
		if it.Current == it.Target || names.IsNNOf(it.Current, it.Target) {
			out[i], done[i] = it.Current, true
			reserve(reserved, it.Current)
		}
	}
	// 2. case-only differences keep their slot when it is free: "X.PDF" ->
	// "X.pdf", "X_01.PDF" -> "X_01.pdf" (never swap the two).
	for _, i := range order {
		it := items[i]
		if done[i] || it.Current == "" || it.Target == "" {
			continue
		}
		slot := ""
		if names.FoldKey(it.Current) == names.FoldKey(it.Target) {
			slot = it.Target
		} else if n := names.NNOf(names.FoldKey(it.Current), names.FoldKey(it.Target)); n > 0 {
			slot = names.WithNN(it.Target, n)
		}
		if slot != "" && !reserved[names.FoldKey(slot)] {
			out[i], done[i] = slot, true
			reserve(reserved, slot)
		}
	}
	// 3. everyone else: first free name
	for _, i := range order {
		if done[i] || items[i].Target == "" {
			continue
		}
		t := items[i].Target
		cand := t
		for n := 1; reserved[names.FoldKey(cand)]; n++ {
			cand = names.WithNN(t, n)
		}
		out[i] = cand
		reserve(reserved, cand)
	}
	return out
}

// reserve marks a file path and all its parent directories as taken (a file
// can never be placed where a directory must exist, and vice versa).
func reserve(m map[string]bool, rel string) {
	m[names.FoldKey(rel)] = true
	for d := path.Dir(rel); d != "." && d != "/"; d = path.Dir(d) {
		m[names.FoldKey(d)] = true
	}
}

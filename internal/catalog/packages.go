// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package catalog

import (
	"path/filepath"
	"sort"
	"sync"

	"github.com/mauriziofonte/onefold/internal/kinds"
)

// Package is a folder that looks like a driver or installer package:
// flagged to the user, never excluded on its own. Files and Bytes count the
// cataloged files directly in it.
type Package struct {
	Dir   string `json:"dir"`
	Kind  string `json:"kind"` // driver | installer
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
}

type dirMarks struct {
	markers, files, planned int
	bytes                   int64
}

// markDir records one file name in the markers of its folder (every file
// seen, also the ones the rules skip: an .inf next to excluded .sys files
// still marks a driver package).
func markDir(mu *sync.Mutex, dirs map[string]*dirMarks, path, base string) *dirMarks {
	mu.Lock()
	defer mu.Unlock()
	d := filepath.Dir(path)
	dm := dirs[d]
	if dm == nil {
		dm = &dirMarks{}
		dirs[d] = dm
	}
	dm.markers |= kinds.PackageMarkers(base)
	dm.files++
	return dm
}

func packagesOf(dirs map[string]*dirMarks) []Package {
	var out []Package
	for d, dm := range dirs {
		if k := kinds.PackageKind(dm.markers, dm.files); k != "" && dm.planned > 0 {
			out = append(out, Package{Dir: d, Kind: k, Files: dm.planned, Bytes: dm.bytes})
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Dir < out[b].Dir })
	return out
}

// RecountPackages counts the records left directly in each package folder
// (a re-plan) and drops the folders left empty.
func RecountPackages(pkgs []Package, recs []Record) []Package {
	idx := map[string]int{}
	out := make([]Package, len(pkgs))
	for i, p := range pkgs {
		out[i] = Package{Dir: p.Dir, Kind: p.Kind}
		idx[foldCase(filepath.Clean(p.Dir))] = i
	}
	for _, r := range recs {
		if i, ok := idx[foldCase(filepath.Dir(r.Path))]; ok {
			out[i].Files++
			out[i].Bytes += r.Size
		}
	}
	kept := out[:0]
	for _, p := range out {
		if p.Files > 0 {
			kept = append(kept, p)
		}
	}
	return kept
}

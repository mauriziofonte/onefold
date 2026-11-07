// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package sidecar pairs the files that belong to a photo or a video with it:
// the edits and metadata next to it (.AAE, .xmp, .THM), its Google Takeout
// .json, and the video half of a Live Photo. A paired file follows its owner:
// it takes the owner's new name and is never deduplicated on its own. The
// name rules are the registry's (kinds.SidecarRest); this package only
// groups files by folder and settles who owns what.
package sidecar

import (
	"sort"
	"strings"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/kinds"
	"github.com/mauriziofonte/onefold/internal/names"
)

// LiveMaxSec is the longest video taken as the moving half of a Live Photo
// (they last about three seconds).
const LiveMaxSec = 6

// File is one candidate: its path (any separator, every path of one call in
// the same form) and, for a video, its duration in seconds (0 = unknown,
// never a Live Photo half).
type File struct {
	Path     string
	Duration float64
}

// Pair returns, for every file, the index of the file it belongs to, or -1.
// Owners are the photos and the videos that are not themselves the half of
// a Live Photo. A file two owners could claim is left alone, except a
// sidecar of a RAW and its JPEG (the RAW owns it: editors write .xmp for the
// RAW). Deterministic: the result does not depend on the order of files.
func Pair(files []File) []int {
	owner := make([]int, len(files))
	for i := range owner {
		owner[i] = -1
	}
	byDir := map[string][]int{}
	for i, f := range files {
		ext := kinds.Ext(f.Path)
		if !kinds.IsSidecarExt(ext) && !kinds.IsMedia(kinds.KindOf(ext)) {
			continue
		}
		d, _ := split(f.Path)
		byDir[d] = append(byDir[d], i)
	}
	for _, idx := range byDir {
		pairDir(files, idx, owner)
	}
	return owner
}

func pairDir(files []File, idx []int, owner []int) {
	sort.Slice(idx, func(a, b int) bool { return files[idx[a]].Path < files[idx[b]].Path })
	var photos, media, others []int
	for _, i := range idx {
		k := kinds.KindOf(kinds.Ext(files[i].Path))
		switch {
		case k == kinds.Photo:
			photos = append(photos, i)
			media = append(media, i)
		case kinds.IsMedia(k):
			media = append(media, i)
		default:
			others = append(others, i)
		}
	}
	// 1. Live Photo halves: a short video whose stem is one still's
	live := map[int]bool{}
	for _, v := range media {
		_, vb := split(files[v].Path)
		if kinds.KindOf(kinds.Ext(vb)) != kinds.Video || !kinds.IsLive(kinds.Ext(vb)) {
			continue
		}
		if d := files[v].Duration; d <= 0 || d > LiveMaxSec {
			continue
		}
		if o := claim(files, photos, v, live); o >= 0 {
			owner[v], live[v] = o, true
		}
	}
	// 2. sidecars: owned by a photo or by a video that is not a Live half
	var owners []int
	for _, m := range media {
		if !live[m] {
			owners = append(owners, m)
		}
	}
	for _, s := range others {
		if o := claim(files, owners, s, live); o >= 0 {
			owner[s] = o
		}
	}
}

// claim returns the one owner among cands that file s belongs to, -1 when
// none does or when two do (a RAW wins over the others then).
func claim(files []File, cands []int, s int, skip map[int]bool) int {
	_, sb := split(files[s].Path)
	var hits []int
	for _, c := range cands {
		if c == s || skip[c] {
			continue
		}
		_, cb := split(files[c].Path)
		if _, _, ok := kinds.SidecarRest(cb, sb); ok {
			hits = append(hits, c)
		}
	}
	switch len(hits) {
	case 0:
		return -1
	case 1:
		return hits[0]
	}
	raw := -1
	for _, h := range hits {
		_, hb := split(files[h].Path)
		if kinds.IsRAW(kinds.Ext(hb)) {
			if raw >= 0 {
				return -1
			}
			raw = h
		}
	}
	return raw
}

// Target returns the new path of the sidecar at side whose owner moves from
// ownerOld to ownerNew: in ownerNew's folder, under ownerNew's name. The
// sidecar keeps its own extension as written ("" when side does not belong
// to ownerOld).
func Target(ownerOld, ownerNew, side string) string {
	_, ob := split(ownerOld)
	_, sb := split(side)
	rest, byName, ok := kinds.SidecarRest(ob, sb)
	if !ok {
		return ""
	}
	nd, nb := split(ownerNew)
	if !byName {
		if i := strings.LastIndexByte(nb, '.'); i > 0 {
			nb = nb[:i]
		}
	}
	return nd + names.NFC(nb+rest)
}

// split cuts p after its last separator: the folder with its trailing
// separator ("" for a bare name) and the base name.
func split(p string) (dir, base string) {
	i := strings.LastIndexAny(p, `/\`)
	return p[:i+1], p[i+1:]
}

// Records pairs catalog records (Pair over host and path): the owner index
// of every record, or -1.
func Records(recs []catalog.Record) []int {
	files := make([]File, len(recs))
	for i := range recs {
		files[i] = File{Path: recs[i].Host + "\x00" + recs[i].Path, Duration: recs[i].Meta.DurationSec}
	}
	return Pair(files)
}

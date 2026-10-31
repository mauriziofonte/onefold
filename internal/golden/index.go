// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package golden knows what a golden tree already holds without reading it:
// an index built from the plans that were applied (and, for plans written
// before they carried the winner's metadata, the manifests they came from),
// and the incremental match of new files against it. The tree itself is
// never cataloged again: silvering changed its bytes, the identity of a
// golden asset is the hash of its original.
package golden

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/kinds"
	"github.com/mauriziofonte/onefold/internal/plan"
)

// Entry is one golden asset of the index.
type Entry struct {
	ID          string   `json:"id"` // sparse hash of the original winner
	Kind        string   `json:"kind"`
	CanonRel    string   `json:"canonRel"`
	Time        string   `json:"time,omitempty"`
	SubSecNs    int64    `json:"subSecNs,omitempty"`
	DurationSec float64  `json:"durationSec,omitempty"`
	Width       int      `json:"width,omitempty"`
	Height      int      `json:"height,omitempty"`
	Size        int64    `json:"size"`
	CanonDevice string   `json:"canonDevice,omitempty"`
	IsEdit      bool     `json:"isEdit,omitempty"`
	SrcPath     string   `json:"srcPath"`          // the original winner (its extension is part of the shot key)
	Hashes      []string `json:"hashes,omitempty"` // other members known from the manifests (drops): byteKey of each
}

// Shot is the capture identity of the entry ("" = by bytes only).
func (e Entry) Shot() string {
	return plan.ShotKey(e.Kind, e.Time, e.SubSecNs, e.DurationSec, e.SrcPath)
}

// Pixels is the resolution used to tell an upgrade from a duplicate.
func (e Entry) Pixels() int64 { return int64(e.Width) * int64(e.Height) }

// Stats of a Build.
type Stats struct {
	Entries  int // golden assets
	Joined   int // entries whose metadata came from a manifest (plan without it)
	Unjoined int // entries without metadata: matched by bytes only
	Repeated int // actions of a later plan already in the index (same id): kept once
}

// Build indexes the actions of the applied plans, in order. Actions missing
// the winner's metadata (plans written before it was part of the plan) get it
// from recs by host and path; recs also give the hashes of the drops.
func Build(plans [][]plan.Action, recs []catalog.Record) ([]Entry, Stats) {
	byMember := make(map[string]catalog.Record, len(recs))
	for _, r := range recs {
		byMember[r.Host+"\x00"+r.Path] = r
	}
	var st Stats
	seen := map[string]bool{}
	var out []Entry
	for _, acts := range plans {
		for _, a := range acts {
			if seen[a.ID] {
				st.Repeated++
				continue
			}
			seen[a.ID] = true
			e := Entry{ID: a.ID, Kind: a.Kind, CanonRel: a.CanonRel, Time: a.Time, SubSecNs: a.SubSecNs,
				DurationSec: a.DurationSec, Width: a.Width, Height: a.Height, Size: a.SrcSize, SrcPath: a.SrcPath,
				CanonDevice: a.CanonDevice, IsEdit: a.IsEdit}
			if r, ok := byMember[a.SrcHost+"\x00"+a.SrcPath]; ok && e.Width == 0 && e.Height == 0 && e.SubSecNs == 0 && e.DurationSec == 0 {
				e.SubSecNs, e.DurationSec, e.Width, e.Height = r.Meta.SubSecNs, r.Meta.DurationSec, r.Meta.Width, r.Meta.Height
				st.Joined++
			} else if !ok && e.Width == 0 && e.Height == 0 && e.SubSecNs == 0 && e.DurationSec == 0 && a.IsMediaAction() {
				st.Unjoined++
			}
			for _, d := range a.Drops {
				if r, ok := byMember[d.Host+"\x00"+d.Path]; ok {
					if k := byteKey(r.Meta.Kind, r.Hash, r.Path); k != byteKey(a.Kind, a.ID, a.SrcPath) {
						e.Hashes = append(e.Hashes, k)
					}
				}
			}
			sort.Strings(e.Hashes)
			out = append(out, e)
		}
	}
	st.Entries = len(out)
	return out, st
}

// Write writes the index: header (canonical zone), then one entry per line.
func Write(w io.Writer, es []Entry) error {
	bw := bufio.NewWriterSize(w, 1<<20)
	if err := catalog.WriteHeader(bw, catalog.KindIndex); err != nil {
		return err
	}
	enc := json.NewEncoder(bw)
	for i := range es {
		if err := enc.Encode(&es[i]); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// Index is the in-memory view of a golden index.
type Index struct {
	Entries   []Entry
	byHash    map[string]int    // byteKey of the original and of its drops -> entry
	anyFormat map[string]string // photo hash -> one of its byteKeys (generic copies of a photo)
	byShot    map[string][]int  // capture identity -> entries (one per device or edit)
	names     map[string]bool   // lower-case canonRel: names already taken in the tree
	videoAt   map[string][]int  // video start time -> entries (duration tolerance)
}

// New builds the lookups of an index.
func New(es []Entry) *Index {
	ix := &Index{Entries: es, byHash: make(map[string]int, len(es)), anyFormat: map[string]string{}, videoAt: map[string][]int{}, byShot: map[string][]int{}, names: make(map[string]bool, len(es))}
	for i, e := range es {
		for _, k := range append([]string{byteKey(e.Kind, e.ID, e.SrcPath)}, e.Hashes...) {
			if _, ok := ix.byHash[k]; !ok {
				ix.byHash[k] = i
			}
			if h, _, photo := strings.Cut(k, "|"); photo {
				ix.anyFormat[h] = k
			}
		}
		if k := e.Shot(); k != "" {
			ix.byShot[k] = append(ix.byShot[k], i)
		}
		if e.Kind == "video" && e.Time != "" {
			ix.videoAt[e.Time] = append(ix.videoAt[e.Time], i)
		}
		ix.names[strings.ToLower(e.CanonRel)] = true
	}
	return ix
}

// byteKey identifies bytes the way the planner does: a photo by its hash and
// canonical format (the same bytes under two photo formats are two assets),
// anything else by its hash.
func byteKey(kind, hash, p string) string {
	if kind == "photo" {
		return hash + "|" + kinds.NormalizeExt(kinds.Ext(p))
	}
	return hash
}

// ByBytes returns the entry holding the bytes of a file of this kind and path:
// a photo only under the same format, anything else under any (a generic copy
// of a golden photo is a duplicate, as the planner absorbs it).
func (ix *Index) ByBytes(kind, hash, p string) (Entry, bool) {
	if kind == "photo" {
		return ix.lookup(byteKey(kind, hash, p))
	}
	if e, ok := ix.lookup(hash); ok {
		return e, true
	}
	return ix.lookup(ix.anyFormat[hash])
}

func (ix *Index) lookup(k string) (Entry, bool) {
	i, ok := ix.byHash[k]
	if !ok {
		return Entry{}, false
	}
	return ix.Entries[i], true
}

// ByShot returns the entries of this capture identity.
func (ix *Index) ByShot(k string) []Entry {
	if k == "" {
		return nil
	}
	var es []Entry
	for _, i := range ix.byShot[k] {
		es = append(es, ix.Entries[i])
	}
	return es
}

// VideosAt returns the video entries starting at this time whose duration
// differs from d by at most tol seconds.
func (ix *Index) VideosAt(time string, d, tol float64) []Entry {
	var es []Entry
	for _, i := range ix.videoAt[time] {
		if e := ix.Entries[i]; e.DurationSec-d <= tol && d-e.DurationSec <= tol {
			es = append(es, e)
		}
	}
	return es
}

// Taken reports whether a canonical path is already used in the tree
// (case-insensitive, like the collision rule of the plan).
func (ix *Index) Taken(rel string) bool { return ix.names[strings.ToLower(rel)] }

// Load reads an index written by Write in the canonical zone of this run;
// like a plan, any malformed line fails the read.
func Load(path string) (*Index, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var es []Entry
	zone := ""
	for n := 1; sc.Scan(); n++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if h, ok := catalog.ParseHeader(line); ok {
			if h.Kind != catalog.KindIndex {
				return nil, fmt.Errorf("%s: a onefold %s, not a golden index", path, h.Kind)
			}
			zone = h.Zone
			continue
		}
		var e Entry
		if err := json.Unmarshal(line, &e); err != nil || e.ID == "" {
			return nil, fmt.Errorf("%s: line %d is not an index entry: write the index again (golden-index -o)", path, n)
		}
		es = append(es, e)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := catalog.CheckZone(path, catalog.KindIndex, zone); err != nil {
		return nil, err
	}
	return New(es), nil
}

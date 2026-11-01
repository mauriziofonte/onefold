// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package golden

import (
	"path"
	"sort"
	"strings"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/names"
	"github.com/mauriziofonte/onefold/internal/plan"
)

// Known is a new asset the golden tree already holds.
type Known struct {
	SrcHost string `json:"srcHost"`
	SrcPath string `json:"srcPath"`
	Size    int64  `json:"size"`
	Golden  string `json:"golden"` // canonRel of the golden asset
	Reason  string `json:"reason"`
}

// Reasons of Known.
const (
	SameBytes = "same bytes"                    // the winner or a member is a golden original or one of its drops
	SameShot  = "same shot, not more pixels"    // same capture identity, resolution not higher
	MorePix   = "same shot, more pixels"        // an upgrade: reported, never applied
	Collision = "name taken in the golden tree" // reason of a renamed new action (Renamed)
)

// MatchResult splits the assets of new files against a golden index.
type MatchResult struct {
	Plan     *plan.Result  // the plan of the new files alone
	New      []plan.Action // the sub-plan: assets the tree does not hold, names free in the tree
	Dupes    []Known       // already in the tree
	Upgrades []Known       // same capture with more pixels: listed, never applied (the tree only grows)
	Renamed  int           // new actions whose canonical name was taken in the tree (_NN suffix)
}

// Match plans recs (the new files: any manifests) with o and sorts every
// elected asset into dupe, upgrade or new. New assets become a sub-plan
// whose names never collide with the tree: apply never replaces a file, so
// a name already used there would silently skip a new asset.
func Match(ix *Index, recs []catalog.Record, o plan.Options) *MatchResult {
	res := plan.PlanWith(recs, o)
	m := &MatchResult{Plan: res}
	var fresh []plan.Asset
	for _, a := range res.Golden {
		w := a.Winner
		k := Known{SrcHost: w.Host, SrcPath: w.Path, Size: w.Size}
		if e, ok := ix.inTree(a); ok {
			k.Golden, k.Reason = e.CanonRel, SameBytes
			m.Dupes = append(m.Dupes, k)
			continue
		}
		if e, ok := ix.sameShot(w, o.VideoTolerance); ok {
			k.Golden = e.CanonRel
			if int64(w.Meta.Width)*int64(w.Meta.Height) > e.Pixels() {
				k.Reason = MorePix
				m.Upgrades = append(m.Upgrades, k)
			} else {
				k.Reason = SameShot
				m.Dupes = append(m.Dupes, k)
			}
			continue
		}
		fresh = append(fresh, a)
	}
	acts := plan.BuildActionsWith(&plan.Result{Golden: fresh}, o)
	// names already in the tree: the next free _NN, in a stable order
	sort.Slice(acts, func(i, j int) bool {
		if acts[i].CanonRel != acts[j].CanonRel {
			return acts[i].CanonRel < acts[j].CanonRel
		}
		return acts[i].ID < acts[j].ID
	})
	taken := map[string]bool{}
	for _, a := range acts {
		taken[strings.ToLower(a.CanonRel)] = true
	}
	for i := range acts {
		if !ix.Taken(acts[i].CanonRel) {
			continue
		}
		base := acts[i].CanonRel
		for n := 1; ; n++ {
			c := names.WithNN(base, n)
			if !ix.Taken(c) && !taken[strings.ToLower(c)] {
				acts[i].CanonRel, acts[i].CanonName = c, path.Base(c)
				taken[strings.ToLower(c)] = true
				m.Renamed++
				break
			}
		}
	}
	m.New = acts
	sortKnown(m.Dupes)
	sortKnown(m.Upgrades)
	return m
}

// inTree: the winner or any member has bytes the tree already holds.
func (ix *Index) inTree(a plan.Asset) (Entry, bool) {
	for _, mb := range append([]catalog.Record{a.Winner}, a.Members...) {
		if e, ok := ix.ByBytes(mb.Meta.Kind, mb.Hash, mb.Path); ok {
			return e, true
		}
	}
	return Entry{}, false
}

// sameShot: an entry of the same capture identity (videos: also any start at
// the same time within the duration tolerance), the same device (or one
// of the two unknown) and neither an edit, as the planner would have
// clustered them.
func (ix *Index) sameShot(w catalog.Record, videoTol float64) (Entry, bool) {
	if plan.IsEdit(w) {
		return Entry{}, false
	}
	cands := ix.ByShot(plan.ShotKey(w.Meta.Kind, w.Meta.Time, w.Meta.SubSecNs, w.Meta.DurationSec, w.Path))
	if w.Meta.Kind == "video" && w.Meta.Time != "" && videoTol > 0 {
		cands = append(cands, ix.VideosAt(w.Meta.Time, w.Meta.DurationSec, videoTol)...)
	}
	for _, e := range cands {
		if e.IsEdit {
			continue
		}
		if d := w.Meta.CanonDevice; d != "" && e.CanonDevice != "" && d != e.CanonDevice {
			continue
		}
		return e, true
	}
	return Entry{}, false
}

func sortKnown(ks []Known) {
	sort.Slice(ks, func(i, j int) bool {
		if ks[i].SrcHost != ks[j].SrcHost {
			return ks[i].SrcHost < ks[j].SrcHost
		}
		return ks[i].SrcPath < ks[j].SrcPath
	})
}

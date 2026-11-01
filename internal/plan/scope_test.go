// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package plan

import (
	"sort"
	"testing"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/meta"
)

func shot(p, hash string, w, h int, size int64) catalog.Record {
	return catalog.Record{Path: p, Hash: hash, Size: size, Room: "/r",
		Meta: meta.Meta{Kind: "photo", Time: "2019-08-09 23:02:16", SubSecNs: 250000000, Prefix: "DSC", Width: w, Height: h}}
}

// One winner per format: the RAW and the JPG of one shot are two assets,
// the HEIC and its exported JPG too; copies of the same format still collapse.
func TestOneWinnerPerFormat(t *testing.T) {
	recs := []catalog.Record{
		shot("/r/IMG_1234.CR2", "R", 3888, 2592, 9000),
		shot("/r/IMG_1234.JPG", "J1", 3888, 2592, 3000),
		shot("/r/copy/IMG_1234.jpeg", "J2", 1944, 1296, 900), // lower-res copy of the same JPG shot
		shot("/r/IMG_1234.HEIC", "H1", 4032, 3024, 2000),
		shot("/r/again/IMG_1234.heic", "H2", 4032, 3024, 1500), // smaller copy of the same HEIC shot
		shot("/r/scan.tif", "T", 800, 600, 500),
	}
	r := PlanWith(recs, Options{})
	if r.Assets != 4 || r.LowerRes != 1 || r.SmallerVar != 1 {
		t.Fatalf("assets=%d lowerRes=%d smaller=%d", r.Assets, r.LowerRes, r.SmallerVar)
	}
	var rels []string
	for _, a := range BuildActions(r) {
		rels = append(rels, a.CanonRel)
		if !a.IsMediaAction() {
			t.Errorf("%s: not a media action", a.CanonRel)
		}
	}
	sort.Strings(rels)
	want := []string{
		"PICTURES/CR2/2019/DSC_20190809_230216.cr2",
		"PICTURES/HEIC/2019/DSC_20190809_230216.heic",
		"PICTURES/JPG/2019/DSC_20190809_230216.jpg",
		"PICTURES/TIFF/2019/DSC_20190809_230216.tif",
	}
	if len(rels) != len(want) {
		t.Fatalf("rels %v", rels)
	}
	for i := range want {
		if rels[i] != want[i] {
			t.Errorf("rel %s want %s", rels[i], want[i])
		}
	}
}

// Byte-identical photos under two formats (a PNG saved as .jpg) stay two
// assets: the mislabeled copy never swallows the real one.
func TestSameBytesTwoFormats(t *testing.T) {
	png := catalog.Record{Path: "/r/web.png", Hash: "X", Size: 10, Room: "/r", Meta: meta.Meta{Kind: "photo", Width: 4, Height: 3}}
	jpg := catalog.Record{Path: "/r/web.jpg", Hash: "X", Size: 10, Room: "/r", Meta: meta.Meta{Kind: "photo", ExtMismatch: true, SniffExt: "png"}}
	if r := PlanWith([]catalog.Record{png, jpg}, Options{}); r.Assets != 2 || r.ExactDupes != 0 {
		t.Fatalf("assets=%d dupes=%d", r.Assets, r.ExactDupes)
	}
	cp := png
	cp.Path = "/r/copy/web.PNG"
	if r := PlanWith([]catalog.Record{png, cp}, Options{}); r.Assets != 1 || r.ExactDupes != 1 {
		t.Fatalf("same format copies: assets=%d dupes=%d", r.Assets, r.ExactDupes)
	}
}

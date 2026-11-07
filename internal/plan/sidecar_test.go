// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package plan

import (
	"reflect"
	"testing"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/meta"
)

func photo(p, hash string, subSec int64) catalog.Record {
	return catalog.Record{Room: "/r", Path: p, Size: 100, Hash: hash, Host: "h1",
		Meta: meta.Meta{Kind: "photo", Time: "2021-05-06 07:08:09", TimeSource: "metadata", SubSec: true, SubSecNs: subSec, Prefix: "IMG", Width: 40, Height: 30}}
}

func side(p, hash string) catalog.Record {
	return catalog.Record{Room: "/r", Path: p, Size: 5, Hash: hash, Host: "h1", Sidecar: true, Meta: meta.Meta{Kind: "other"}}
}

// Options.Sidecars: an edit, a Takeout .json and the Live Photo video go
// with the winner under its canonical name; an exact copy brings the name
// the winner lacks; a sidecar with no photo never becomes an asset; off, the
// files cataloged as sidecars are left out and nothing else changes.
func TestPlanSidecars(t *testing.T) {
	live := catalog.Record{Room: "/r", Path: "/r/DCIM/IMG_1.MOV", Size: 50, Hash: "LIVE", Host: "h1",
		Meta: meta.Meta{Kind: "video", Time: "2021-05-06 07:08:09", TimeSource: "metadata", DurationSec: 2.8, Prefix: "VID"}}
	recs := []catalog.Record{
		photo("/r/DCIM/IMG_1.HEIC", "P1", 1e8),
		side("/r/DCIM/IMG_1.AAE", "E1"),
		side("/r/DCIM/IMG_1.HEIC.json", "J1"),
		live,
		photo("/r/copy/IMG_1.HEIC", "P1", 1e8), // exact copy, loses on the path
		side("/r/copy/IMG_1.AAE", "E2"),        // the winner has its own .AAE
		side("/r/copy/IMG_1.xmp", "X2"),        // the winner has none: comes along
		side("/r/other/notes.xmp", "N"),        // nobody's
	}
	r := PlanWith(recs, Options{Sidecars: true})
	acts := BuildActionsWith(r, Options{Sidecars: true})
	if len(acts) != 1 {
		t.Fatalf("%d actions, want 1 (the Live Photo video is not an asset): %+v", len(acts), acts)
	}
	a := acts[0]
	if a.SrcPath != "/r/DCIM/IMG_1.HEIC" || a.CanonRel != "PICTURES/HEIC/2021/IMG_20210506_070809.heic" {
		t.Fatalf("winner %s -> %s", a.SrcPath, a.CanonRel)
	}
	want := []Sidecar{
		{"/r/DCIM/IMG_1.AAE", 5, "PICTURES/HEIC/2021/IMG_20210506_070809.AAE"},
		{"/r/DCIM/IMG_1.HEIC.json", 5, "PICTURES/HEIC/2021/IMG_20210506_070809.heic.json"},
		{"/r/DCIM/IMG_1.MOV", 50, "PICTURES/HEIC/2021/IMG_20210506_070809.MOV"},
		{"/r/copy/IMG_1.xmp", 5, "PICTURES/HEIC/2021/IMG_20210506_070809.xmp"},
	}
	if !reflect.DeepEqual(a.Sidecars, want) {
		t.Fatalf("sidecars\n got %+v\nwant %+v", a.Sidecars, want)
	}
	if r.SidecarCount != 5 || r.SidecarsAlone != 1 || r.InputFiles != 2 {
		t.Errorf("counts: kept %d alone %d input %d", r.SidecarCount, r.SidecarsAlone, r.InputFiles)
	}

	// off: the cataloged sidecars are left out, the Live Photo video is a video
	off := BuildActionsWith(PlanWith(recs, Options{}), Options{})
	if len(off) != 2 {
		t.Fatalf("off: %d actions, want 2", len(off))
	}
	for _, a := range off {
		if len(a.Sidecars) > 0 || a.Kind == "other" {
			t.Fatalf("off: %+v", a)
		}
	}
}

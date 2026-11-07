// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package kinds

import (
	"slices"
	"testing"
)

func TestSidecarRest(t *testing.T) {
	for _, c := range []struct {
		owner, other, rest string
		byName, ok         bool
	}{
		{"IMG_1.HEIC", "IMG_1.AAE", ".AAE", false, true},
		{"IMG_1.HEIC", "img_1.aae", ".aae", false, true},
		{"IMG_1.CR2", "IMG_1.CR2.xmp", ".xmp", true, true},
		{"IMG_1.HEIC", "IMG_1.HEIC.json", ".json", true, true},
		{"IMG_1.HEIC", "IMG_1.HEIC.supplemental-metadata.json", ".supplemental-metadata.json", true, true},
		{"IMG_1.HEIC", "IMG_1.HEIC.other.json", "", false, false},
		{"IMG_1.HEIC", "IMG_1.MOV", ".MOV", false, true},
		{"IMG_1.CR2", "IMG_1.MOV", "", false, false}, // a RAW has no Live video
		{"IMG_1.HEIC", "IMG_1.json", "", false, false},
		{"notes.txt", "notes.xmp", "", false, false}, // owners are photos and videos
		{"MVI_1.AVI", "MVI_1.THM", ".THM", false, true},
		{"IMG_1.HEIC", "IMG_2.AAE", "", false, false},
	} {
		rest, byName, ok := SidecarRest(c.owner, c.other)
		if rest != c.rest || byName != c.byName || ok != c.ok {
			t.Errorf("SidecarRest(%s, %s) = %q %v %v, want %q %v %v", c.owner, c.other, rest, byName, ok, c.rest, c.byName, c.ok)
		}
	}
}

func TestTakeoutNames(t *testing.T) {
	if got := TakeoutEdited("IMG_1-edited.jpg"); got != "IMG_1.jpg" {
		t.Errorf("edited: %q", got)
	}
	if got := TakeoutEdited("IMG_1-BEARBEITET.JPG"); got != "IMG_1.JPG" {
		t.Errorf("edited, German, upper case: %q", got)
	}
	if got := TakeoutEdited("edited.jpg"); got != "" {
		t.Errorf("a name that is only the suffix: %q", got)
	}
	n := TakeoutNames("IMG_1(2)-edited.jpg")
	for _, want := range []string{"IMG_1(2)-edited.jpg.json", "IMG_1(2).jpg.json", "IMG_1.jpg(2).json", "IMG_1.jpg.supplemental-metadata(2).json"} {
		if !slices.Contains(n, want) {
			t.Errorf("%s not among %v", want, n)
		}
	}
	long := "Screenshot_20200101-120000_A_rather_long_app.jpg"
	if n := TakeoutNames(long); !slices.Contains(n, (long + ".supplemental-metadata")[:46]+".json") {
		t.Errorf("no 51-character cut: %v", n)
	}
}

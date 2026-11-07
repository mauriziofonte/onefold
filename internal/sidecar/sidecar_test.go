// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package sidecar

import (
	"math/rand"
	"testing"
)

func TestPair(t *testing.T) {
	files := []File{
		{Path: "/a/IMG_1234.HEIC"},
		{Path: "/a/IMG_1234.AAE"},
		{Path: "/a/IMG_1234.MOV", Duration: 2.9}, // Live Photo half
		{Path: "/a/IMG_1234.HEIC.json"},          // Takeout
		{Path: "/a/IMG_2000.JPG"},                // a still
		{Path: "/a/IMG_2000.MOV", Duration: 95},  // a real video: its own file
		{Path: "/a/IMG_2000.MOV.json"},           // ...and its Takeout data
		{Path: "/a/DSC_0001.NEF"},                // RAW + JPEG
		{Path: "/a/DSC_0001.JPG"},
		{Path: "/a/DSC_0001.xmp"},     // the RAW's
		{Path: "/a/DSC_0001.JPG.xmp"}, // the JPEG's (name form)
		{Path: "/a/MVI_0042.AVI"},
		{Path: "/a/MVI_0042.THM"},
		{Path: "/a/config.json"},  // no owner
		{Path: "/a/notes.xmp"},    // no owner
		{Path: "/b/IMG_1234.AAE"}, // another folder: no owner
		{Path: "/a/PXL_1.jpg"},    // HEIC + JPG of one stem: the
		{Path: "/a/PXL_1.heic"},   // stem-form .AAE is ambiguous
		{Path: "/a/PXL_1.AAE"},
		{Path: "/a/IMG_3(1).jpg"},
		{Path: "/a/IMG_3.jpg(1).json"},
		{Path: "/a/IMG_4.jpg"},
		{Path: "/a/IMG_4.jpg.supplemental-metadata.json"},
		{Path: "/a/IMG_5.jpg"},
		{Path: "/a/IMG_5.jpg.supplemental-me.json"},
		{Path: "/a/IMG_6.jpg"},
		{Path: "/a/IMG_6.json"}, // stem form is not a Takeout name
	}
	want := map[string]string{
		"/a/IMG_1234.AAE":                         "/a/IMG_1234.HEIC",
		"/a/IMG_1234.MOV":                         "/a/IMG_1234.HEIC",
		"/a/IMG_1234.HEIC.json":                   "/a/IMG_1234.HEIC",
		"/a/IMG_2000.MOV.json":                    "/a/IMG_2000.MOV",
		"/a/DSC_0001.xmp":                         "/a/DSC_0001.NEF",
		"/a/DSC_0001.JPG.xmp":                     "/a/DSC_0001.JPG",
		"/a/MVI_0042.THM":                         "/a/MVI_0042.AVI",
		"/a/IMG_3.jpg(1).json":                    "/a/IMG_3(1).jpg",
		"/a/IMG_4.jpg.supplemental-metadata.json": "/a/IMG_4.jpg",
		"/a/IMG_5.jpg.supplemental-me.json":       "/a/IMG_5.jpg",
	}
	for round := 0; round < 5; round++ {
		got := Pair(files)
		for i, f := range files {
			w, o := want[f.Path], ""
			if got[i] >= 0 {
				o = files[got[i]].Path
			}
			if o != w {
				t.Errorf("round %d: %s owned by %q, want %q", round, f.Path, o, w)
			}
		}
		rand.Shuffle(len(files), func(i, j int) { files[i], files[j] = files[j], files[i] })
	}
}

func TestTarget(t *testing.T) {
	for _, c := range []struct{ old, nu, side, want string }{
		{"/a/IMG_1234.HEIC", "PICTURES/HEIC/2021/IMG_20210506_070809.heic", "/a/IMG_1234.AAE", "PICTURES/HEIC/2021/IMG_20210506_070809.AAE"},
		{"/a/IMG_1234.HEIC", "PICTURES/HEIC/2021/IMG_20210506_070809.heic", "/a/IMG_1234.MOV", "PICTURES/HEIC/2021/IMG_20210506_070809.MOV"},
		{"/a/IMG_1234.HEIC", "PICTURES/HEIC/2021/IMG_20210506_070809.heic", "/a/IMG_1234.HEIC.json", "PICTURES/HEIC/2021/IMG_20210506_070809.heic.json"},
		{`C:\a\DSC_1.CR2`, `C:\b\2020\DSC_1.CR2`, `C:\a\DSC_1.CR2.xmp`, `C:\b\2020\DSC_1.CR2.xmp`},
		{"x/IMG_3(1).jpg", "y/IMG_3_01.jpg", "x/IMG_3.jpg(1).json", "y/IMG_3_01.jpg.json"},
		{"a.jpg", "b.jpg", "c.xmp", ""},
	} {
		if got := Target(c.old, c.nu, c.side); got != c.want {
			t.Errorf("Target(%s, %s, %s) = %q, want %q", c.old, c.nu, c.side, got, c.want)
		}
	}
}

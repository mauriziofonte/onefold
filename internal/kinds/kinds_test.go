// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package kinds

import (
	"strings"
	"testing"
)

func TestRegistryLoaded(t *testing.T) {
	if len(registry) < 10000 {
		t.Fatalf("registry too small: %d", len(registry))
	}
	for e, ty := range registry {
		if e != strings.ToLower(e) || ty.Kind == "" || ty.Category == "" || ty.Group == "" || ty.Format == "" || ty.Canonical == "" {
			t.Fatalf("incomplete row %q: %+v", e, ty)
		}
		switch ty.Kind {
		case Photo, Video, Image, Audio, Document, Text, Archive, Other:
		default:
			t.Fatalf("%s: unknown kind %q", e, ty.Kind)
		}
		if c, ok := registry[ty.Canonical]; !ok || c.Canonical != ty.Canonical {
			t.Fatalf("%s: canonical %q must be a registry row canonical to itself", e, ty.Canonical)
		}
	}
}

func TestKindsAndFacts(t *testing.T) {
	cases := map[string][4]string{ // kind, category, group, format
		"JPG":  {Photo, "raster_image", "IMAGE", "jpeg"},
		"jpeg": {Photo, "raster_image", "IMAGE", "jpeg"},
		"mp4":  {Video, "video", "VIDEO", "isobmff"},
		"nef":  {Photo, "camera_raw", "RAW", "tiff"},
		"gif":  {Image, "raster_image", "IMAGE", "gif"},
		"mp3":  {Audio, "audio", "AUDIO", "mp3"},
		"pdf":  {Document, "page_layout", "PDF", "pdf"},
		"docx": {Document, "word_processing", "DOC", "ooxml-doc"},
		"xls":  {Document, "spreadsheet", "XLS", "ole-sheet"},
		"odp":  {Document, "presentation", "PPT", "odf-slides"},
		"csv":  {Text, "spreadsheet", "XLS", "text"},
		"md":   {Text, "text", "TXT", "text"},
		"js":   {Other, "source_code", "CODE", "text"},
		"ts":   {Other, "source_code", "CODE", "text"},
		"7z":   {Archive, "compressed", "ARCHIVE", "7z"},
		"iso":  {Archive, "disk_image", "DISK", "iso"},
		"key":  {Other, "certificate", "CERT", "none"},
		"exe":  {Other, "executable", "EXE", "pe"},
	}
	for e, w := range cases {
		ty, ok := Lookup(e)
		if !ok || ty.Kind != w[0] || ty.Category != w[1] || ty.Group != w[2] || ty.Format != w[3] {
			t.Errorf("%s: %+v want %v", e, ty, w)
		}
	}
	if KindOf("qqqzz") != Other || Format("qqqzz") != "none" || Known("qqqzz") {
		t.Fatal("unknown ext defaults")
	}
	for in, want := range map[string]string{"JPEG": "jpg", "Tif": "tiff", "htm": "html", "yml": "yaml", "mpeg": "mpg", "midi": "midi", "aif": "aif", "PDF": "pdf"} {
		if NormalizeExt(in) != want {
			t.Errorf("NormalizeExt(%s)=%s", in, NormalizeExt(in))
		}
	}
	if !IsSystemExt("DLL") || IsSystemExt("pdf") || !IsJunk("thumbs.DB") || IsJunk("photo.jpg") {
		t.Fatal("system/junk")
	}
}

func TestPresets(t *testing.T) {
	m, all, err := PresetExts([]string{"media"})
	if err != nil || all || len(m) != 52 || !m["jpg"] || !m["3gp"] || !m["heic"] || !m["png"] || !m["nef"] || !m["webm"] || m["gif"] || m["nksc"] {
		t.Fatalf("media preset pinned: %v %v %v", m, all, err)
	}
	u, _, _ := PresetExts([]string{"image", "audio"})
	if !u["png"] || !u["nef"] || !u["jpg"] || !u["mp3"] || u["pdf"] {
		t.Fatalf("union: %d", len(u))
	}
	d, _, _ := PresetExts([]string{"document"})
	if !d["docx"] || !d["csv"] || !d["txt"] || !d["epub"] || d["json"] || d["js"] {
		t.Fatal("document preset")
	}
	if a, _, _ := PresetExts([]string{"archive"}); !a["iso"] || !a["7z"] || a["pdf"] {
		t.Fatal("archive preset")
	}
	if p, _, _ := PresetExts([]string{"personal"}); !p["pdf"] || !p["ppk"] || p["dll"] {
		t.Fatal("personal preset")
	}
	if c, _, _ := PresetExts([]string{"common"}); len(c) < 200 || !c["docx"] {
		t.Fatalf("common preset: %d", len(c))
	}
	if _, all, _ := PresetExts([]string{"all"}); !all {
		t.Fatal("all must disable the whitelist")
	}
	if _, _, err := PresetExts([]string{"nope"}); err == nil {
		t.Fatal("unknown preset must error")
	}
	if ex, err := CategoryExts([]string{"camera_raw"}); err != nil || !ex["nef"] || ex["jpg"] {
		t.Fatal("camera_raw category")
	}
	if _, err := CategoryExts([]string{"bogus"}); err == nil {
		t.Fatal("unknown category must error")
	}
}

func TestSniffAndMismatch(t *testing.T) {
	if SniffFamily([]byte("\x89PNG\r\n\x1a\n0000")) != "png" || SniffFamily([]byte("PK\x03\x04rest")) != "zip" ||
		SniffFamily([]byte("\x00\x00\x00\x18ftypisom")) != "isobmff" || SniffFamily([]byte("hi")) != "" {
		t.Fatal("sniff families")
	}
	type c struct {
		ext, fam string
		mm       bool
		fix      string
	}
	for _, x := range []c{
		{"jpg", "png", true, "png"},
		{"docx", "zip", false, ""},     // alias
		{"xlsx", "zip", false, ""},     // alias
		{"heic", "isobmff", false, ""}, // alias
		{"cr3", "isobmff", false, ""},  // alias
		{"nef", "tiff", false, ""},     // alias
		{"mp4", "zip", true, "zip"},
		{"doc", "isobmff", true, ""}, // ambiguous target
		{"dat", "jpg", false, ""},    // no declared family
		{"jpg", "", false, ""},       // unknown content
	} {
		mm, fix := Mismatch(x.ext, x.fam)
		if mm != x.mm || fix != x.fix {
			t.Errorf("Mismatch(%s,%s)=%v,%q want %v,%q", x.ext, x.fam, mm, fix, x.mm, x.fix)
		}
	}
}

// The media pipeline (same-capture clustering, media naming, silvering) is frozen to the
// golden scope; every other type stays on the generic path.
func TestMediaKindsFrozen(t *testing.T) {
	media := map[string]string{}
	for _, e := range []string{"jpg", "jpeg", "heic", "heif", "avif", "png", "webp", "tif", "tiff",
		"3fr", "arw", "bay", "cr2", "cr3", "crw", "cs1", "cxi", "dcr", "dng", "erf", "gpr", "iiq", "j6i", "kc2", "kdc",
		"mef", "mfw", "mos", "mrw", "nef", "nrw", "orf", "pef", "raf", "rw2", "rwl", "rwz", "sr2", "srf", "srw", "x3f"} {
		media[e] = Photo
	}
	for _, e := range []string{"mov", "mp4", "mkv", "avi", "3gp", "webm", "flv", "wmv", "mpg", "mpeg", "m4v"} {
		media[e] = Video
	}
	// RAW-group files that are not images stay generic
	for _, e := range []string{"nksc", "eip"} {
		if KindOf(e) != Image {
			t.Errorf("%s: %s want %s", e, KindOf(e), Image)
		}
	}
	exts, all, err := PresetExts([]string{"media"})
	if err != nil || all || len(exts) != len(media) {
		t.Errorf("preset media: %d exts (all=%v, err=%v), want %d", len(exts), all, err, len(media))
	}
	for e, ty := range registry {
		if IsMedia(ty.Kind) && media[e] != ty.Kind {
			t.Errorf("%s: kind %s outside the frozen media set", e, ty.Kind)
		}
	}
	for e, k := range media {
		if KindOf(e) != k {
			t.Errorf("%s: %s want %s", e, KindOf(e), k)
		}
	}
}

// Canonical extensions name golden files: only the historical aliases fold.
func TestCanonicalFrozen(t *testing.T) {
	old := map[string]string{"jpeg": "jpg", "jpe": "jpg", "tif": "tiff", "htm": "html", "yml": "yaml", "mpeg": "mpg"}
	for e, ty := range registry {
		want := e
		if a, ok := old[e]; ok {
			want = a
		}
		if ty.Canonical != want {
			t.Errorf("%s: canonical %s want %s", e, ty.Canonical, want)
		}
	}
}

func TestPackageMarkers(t *testing.T) {
	m := 0
	for _, f := range []string{"nvdisp.inf", "nvlddmkm.sys", "license.txt"} {
		m |= PackageMarkers(f)
	}
	if PackageKind(m, 3) != "driver" {
		t.Fatalf("driver: %b", m)
	}
	if k := PackageKind(PackageMarkers("README.inf"), 2); k != "" {
		t.Fatalf("an .inf alone: %q", k)
	}
	for _, f := range []string{"Setup.exe", "product.msi", "VC_redist.x64.exe", "dotNetFx45_Full.exe"} {
		if PackageKind(PackageMarkers(f), 2) != "installer" {
			t.Errorf("%s: not an installer", f)
		}
	}
	if PackageKind(PackageMarkers("setup.exe"), 1) != "" {
		t.Error("a lone setup.exe is not a package folder")
	}
	if PackageMarkers("photo.jpg") != 0 || PackageMarkers("lib.dll") != PkgDriverBin {
		t.Error("markers")
	}
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package naming

import (
	"strings"
	"testing"
	"time"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/meta"
)

func rec(path, kind, tm string, m meta.Meta) catalog.Record {
	m.Kind, m.Time = kind, tm
	if tm != "" {
		m.TimeSource = "metadata"
	} else {
		m.TimeSource = "none"
	}
	return catalog.Record{Path: path, Size: 3 << 20, Meta: m, MTimeNs: time.Date(2021, 3, 4, 5, 6, 7, 0, time.Local).UnixNano()}
}

func render(t *testing.T, tpl string, r catalog.Record) string {
	t.Helper()
	n, err := Resolve(tpl)
	if err != nil {
		t.Fatal(err)
	}
	return n.Target(VarsOf(r))
}

func TestGrammar(t *testing.T) {
	r := rec("/x/Downloads/report.PDF", "document", "2019-03-05 10:11:12", meta.Meta{Author: "AC/DC", FileCategory: "page_layout"})
	cases := map[string]string{
		`{EXT}/{year}/{stem}.{ext}`:                    "PDF/2019/report.pdf",
		`{EXT}\{year}\{stem}.{ext}`:                    "PDF/2019/report.pdf", // backslash agnostic
		`{title|stem}<.{ext}>`:                         "report.pdf",
		`{author}/{stem}`:                              "AC_DC/report", // value slash never splits
		`<{title} ({year})|{stem} [{year}]>.{ext}`:     "report [2019].pdf",
		`{device}/{stem}.{ext}`:                        "report.pdf", // empty segment dropped
		`{device|"Unknown"}/{stem}.{ext}`:              "Unknown/report.pdf",
		`<{month} - {month-name-it}>/{stem}`:           "03 - Marzo/report",
		`<{month} - {month-name}>/<{pre-2000}/>{stem}`: "03 - March/report",
		`a:b*c?/{stem}`:                                "a_b_c_/report",
		`CON/{stem}`:                                   "CON_/report",
	}
	for tpl, want := range cases {
		if got := render(t, tpl, r); got != want {
			t.Errorf("%s -> %q want %q", tpl, got, want)
		}
	}
	for _, bad := range []string{`{nope}`, `{stem`, `<{stem}`, `a>b`, `{}`} {
		if _, err := Resolve(bad); err == nil {
			t.Errorf("%q must not parse", bad)
		}
	}
	if _, err := Resolve("no-such-preset"); err == nil {
		t.Error("unknown preset must fail")
	}
}

func TestStemVariants(t *testing.T) {
	r := rec("/FILES/WAV/2015/(20151209_002700) [Registratore Tascam] 151209_0027 (1).wav", "audio", "2015-12-09 00:27:00", meta.Meta{})
	v := VarsOf(r)
	if v["stem"] != "(20151209_002700) [Registratore Tascam] 151209_0027 (1)" || v["stem-norm"] != "[Registratore Tascam] 151209_0027" ||
		v["stem-clean"] != "151209_0027" || v["origin"] != "Registratore Tascam" || v["parent"] != "2015" || v["name-date"] != "20151209_002700" {
		t.Fatalf("%+v", v)
	}
	if got := render(t, `{ext-norm}/{year}/{stem}.{ext-norm}`, r); got != "wav/2015/(20151209_002700) [Registratore Tascam] 151209_0027 (1).wav" {
		t.Errorf("raw stem: %s", got)
	}
	if got := render(t, "ext-year", r); got != "WAV/2015/[Registratore Tascam] 151209_0027.wav" {
		t.Errorf("ext-year keeps the origin prefix: %s", got)
	}
	if got := render(t, "ext-year-clean", r); got != "WAV/2015/151209_0027.wav" {
		t.Errorf("ext-year-clean: %s", got)
	}
	if got := render(t, "ext-era-year-origin", r); got != "WAV/2015/[Registratore Tascam] 151209_0027.wav" {
		t.Errorf("origin preset: %s", got)
	}
	// _NN is a collision marker only when the base file sits next to it
	files := map[string]bool{"/d/prova1.js": true, "/d/prova1_01.js": true, "/d/esame10_09_04.pdf": true, "/d/[x] a.txt": true, "/d/[x] a_01.txt": true}
	ex := func(p string) bool { return files[strings.ReplaceAll(p, "\\", "/")] }
	for p, want := range map[string][3]string{
		"/d/prova1_01.js":      {"prova1_01", "prova1", "prova1"},
		"/d/esame10_09_04.pdf": {"esame10_09_04", "esame10_09_04", "esame10_09_04"},
		"/d/[x] a_01.txt":      {"[x] a_01", "[x] a", "a"},
	} {
		v := VarsOfCtx(rec(p, "other", "", meta.Meta{}), ex)
		if v["stem"] != want[0] || v["stem-norm"] != want[1] || v["stem-clean"] != want[2] {
			t.Errorf("%s: %s | %s | %s", p, v["stem"], v["stem-norm"], v["stem-clean"])
		}
	}
}

func TestPresets(t *testing.T) {
	photo := rec("/p/DSC_1.JPEG", "photo", "1998-07-06 05:04:03", meta.Meta{Prefix: "DSC", CanonDevice: "Nikon D3500", Category: "camera"})
	undated := rec("/p/[Desktop] scan.png", "image", "", meta.Meta{})
	song := rec("/m/x.mp3", "audio", "2019-01-01 00:00:00", meta.Meta{Author: "The Band", Album: "Live", TagYear: "2019", Title: "Intro", Track: 3, Disc: 2})
	vid := rec("/v/a.mp4", "video", "2021-04-04 13:26:42", meta.Meta{Width: 1920, Height: 1080, CanonDevice: "OnePlus 9 Pro"})
	doc := rec("/d/r.docx", "document", "2020-01-02 03:04:05", meta.Meta{Author: "Mario", Title: "Relazione"})
	code := rec("/d/main.go", "text", "2020-01-02 03:04:05", meta.Meta{})
	want := map[string]map[*catalog.Record]string{
		"ext-year":                {&photo: "JPEG/1998/DSC_1.jpeg", &undated: "PNG/UNDATED/[Desktop] scan.png"},
		"ext-year-clean":          {&undated: "PNG/UNDATED/scan.png"},
		"library":                 {&photo: "PICTURES/JPG/1998/DSC_19980706_050403.jpg", &vid: "VIDEOS/2021/VID_20210404_132642.mp4", &undated: "FILES/PNG/0000/[Desktop] scan.png"},
		"library-files-clean":     {&undated: "FILES/PNG/0000/scan.png"},
		"library-files":           {&photo: "FILES/JPG/1998/DSC_1.jpg"},
		"photo-timestamp":         {&photo: "JPG/1998/DSC_19980706_050403.jpg", &undated: "PNG/Uncategorized/[Desktop] scan.png"},
		"music":                   {&song: "The Band/2019 - Live/203. Intro.mp3", &doc: "DOCX/2020/r.docx"},
		"library-music":           {&song: "MUSIC/The Band/2019 - Live/203. Intro.mp3", &doc: "FILES/DOCX/2020/r.docx"},
		"video-tier":              {&vid: "2021/1080p/OnePlus_9_Pro_20210404_132642.mp4"},
		"date-tree":               {&vid: "2021/04 - April/04/VID_132642_a.mp4", &undated: "undated/[Desktop] scan.png"},
		"library-date-tree":       {&vid: "TIMELINE/2021/04 - April/04/VID_132642_a.mp4"},
		"documents":               {&doc: "DOC/2020/Mario/Relazione.docx", &code: "GO/2020/main.go"},
		"library-documents":       {&doc: "DOCUMENTS/DOC/2020/Mario/Relazione.docx", &code: "FILES/GO/2020/main.go"},
		"devices":                 {&photo: "Camera/Nikon D3500/1998/DSC_1.jpeg"},
		"ext-era-year":            {&photo: "JPG/_FORMER/1998/DSC_1.jpg"},
		"ext-era-year-size-month": {&photo: "JPG/_FORMER/1998/0MB+/07 - July/DSC_1.jpg"},
		"photo-month":             {&photo: "JPG/_PHOTOS/1998/07 - July/IMG_19980706_050403.jpg", &undated: "PNG/UNDATED/[Desktop] scan.png"},
		"photo-month-clean":       {&undated: "PNG/UNDATED/scan.png"},
		"ext-era-year-origin":     {&undated: "PNG/UNDATED/[Desktop] scan.png", &song: "MP3/2019/[m] x.mp3"},
	}
	for name := range Presets {
		for _, n := range []string{name, name + CleanSuffix} {
			if _, err := Resolve(n); err != nil {
				t.Errorf("preset %s: %v", n, err)
			}
		}
	}
	for preset, m := range want {
		for r, w := range m {
			if got := render(t, preset, *r); got != w {
				t.Errorf("%s(%s) = %q want %q", preset, r.Path, got, w)
			}
		}
	}
	// names-language it: {month-name} and the month presets in Italian
	SetNamesLanguage(NamesIT)
	got := render(t, "photo-month", photo)
	SetNamesLanguage("")
	if got != "JPG/_PHOTOS/1998/07 - Luglio/IMG_19980706_050403.jpg" {
		t.Errorf("photo-month with names-language it = %q", got)
	}
	for alias := range LayoutAliases {
		if _, err := Layout(alias); err != nil {
			t.Errorf("alias %q: %v", alias, err)
		}
	}
	kinded, err := Resolve(`photo=P/{stem};PDF,DOC=D/{stem};document=O/{stem};*=X/{stem}`)
	if err != nil || kinded.Target(VarsOf(photo)) != "P/DSC_1" || kinded.Target(VarsOf(song)) != "X/x" || kinded.Target(VarsOf(doc)) != "D/r" {
		t.Fatalf("selector map: %v", err)
	}
	if _, err := Resolve(`photo=P/{stem}`); err == nil || !strings.Contains(err.Error(), "default") {
		t.Fatal("selector map without * must fail")
	}
	if _, err := Resolve(`nope=P/{stem};*=x`); err == nil || !strings.Contains(err.Error(), "selector") {
		t.Fatal("unknown selector must fail")
	}
}

func TestAssign(t *testing.T) {
	items := []Item{
		{Current: "TXT/2020/a.txt", Target: "TXT/2020/a.txt", Tie: "1"},    // stays
		{Current: "TXT/2020/a_01.txt", Target: "TXT/2020/a.txt", Tie: "2"}, // keeps its _01
		{Current: "old/A.txt", Target: "TXT/2020/a.txt", Tie: "3"},         // new -> _02
		{Current: "", Target: "TXT/2020/b.txt", Tie: "4"},                  // merge copy, free
		{Current: "", Target: "TXT/2020/c.txt", Tie: "5"},                  // blocked by a dir
		{Current: "x/c -- frev.1.txt", Target: "TXT/2020/c.txt", Tie: "6"},
		{Current: "D/E.ASP", Target: "D/E.asp", Tie: "9"},    // case-only: keeps its slot
		{Current: "D/E_01.ASP", Target: "D/E.asp", Tie: "0"}, // even with a lower tie
	}
	taken := map[string]bool{"txt/2020/c.txt": true}
	got := Assign(items, taken)
	want := []string{"TXT/2020/a.txt", "TXT/2020/a_01.txt", "TXT/2020/a_02.txt", "TXT/2020/b.txt", "TXT/2020/c_01.txt", "TXT/2020/c_02.txt",
		"D/E.asp", "D/E_01.asp"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("item %d: %s want %s", i, got[i], want[i])
		}
	}
	// idempotent: feed the result back as current paths
	for i := range items {
		items[i].Current = got[i]
	}
	again := Assign(items, taken)
	for i := range got {
		if again[i] != got[i] {
			t.Fatalf("not idempotent: %v vs %v", again, got)
		}
	}
}

// Extension selectors match the harmonized form: a spelling that can never
// match is rejected instead of silently ignored.
func TestExtSelectorHarmonized(t *testing.T) {
	for _, bad := range []string{".jpeg=X/{stem};*={stem}", ".tif=X/{stem};*={stem}", ".PDF=X/{stem};*={stem}"} {
		if _, err := Resolve(bad); err == nil || !strings.Contains(err.Error(), "never matches") {
			t.Errorf("%s: want rejection, got %v", bad, err)
		}
	}
	n, err := Resolve(".jpg=X/{stem};*=Y/{stem}")
	if err != nil {
		t.Fatal(err)
	}
	if got := n.Target(VarsOf(rec("/r/a.JPEG", "photo", "", meta.Meta{}))); got != "X/a" {
		t.Fatalf(".jpg selector must match a .JPEG file: %q", got)
	}
}

// -layout aliases resolve to the golden tree; -naming names presets.
func TestGoldenTreeAndLayoutName(t *testing.T) {
	for spec, golden := range map[string]bool{"library": true, "library-files": true, "library-music-clean": true,
		"music": false, "documents": false, "ext-year": false, "{EXT}/{stem}": false} {
		if GoldenTree(spec) != golden || (CheckTemplate(spec) != nil) != golden {
			t.Errorf("%s: golden=%v", spec, GoldenTree(spec))
		}
	}
	if LayoutName("music") != "library-music" || LayoutName("dedupe") != "library-files" || LayoutName("ext-size") != "ext-size" {
		t.Fatal("LayoutName")
	}
	if n, err := Resolve("music"); err != nil || n.Name != "music" {
		t.Fatalf("Resolve(music) = %v %v, want the music preset", n, err)
	}
}

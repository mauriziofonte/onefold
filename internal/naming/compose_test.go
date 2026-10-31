// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package naming

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mauriziofonte/onefold/internal/meta"
)

// Every preset template survives a parse -> print round trip unchanged in
// meaning (the printed source parses to the same rendering).
func TestTemplateRoundTrip(t *testing.T) {
	r := rec("/x/Camera/[Trip] DSC_0001 (1).jpg", "photo", "2019-03-05 10:11:12", meta.Meta{Prefix: "DSC"})
	for name, p := range Presets {
		for _, part := range strings.Split(p.Spec, ";") {
			_, src, _ := strings.Cut(part, "=")
			a, err := Parse(src)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			b, err := Parse(a.root.String())
			if err != nil {
				t.Fatalf("%s: reprint %q: %v", name, a.root.String(), err)
			}
			if a.Render(VarsOf(r)) != b.Render(VarsOf(r)) || b.root.String() != a.root.String() {
				t.Errorf("%s: %q -> %q", name, src, b.root.String())
			}
		}
	}
}

func TestStemModes(t *testing.T) {
	r := rec(filepath.FromSlash("/a/MAILBOXES (mbox)/merge/(20200101_000000) posta (1).mbox"), "other", "2020-01-02 03:04:05", meta.Meta{})
	cases := map[string]string{
		"norm":     "MBOX/2020/posta.mbox",
		"clean":    "MBOX/2020/posta.mbox",
		"raw":      "MBOX/2020/(20200101_000000) posta (1).mbox",
		"prefix":   "MBOX/2020/[merge] posta.mbox",
		"prefix:2": "MBOX/2020/[MAILBOXES (mbox)] posta.mbox",
		"prefix:3": "MBOX/2020/[a] posta.mbox",
		"prefix:4": "MBOX/2020/posta.mbox", // above the top: no prefix
	}
	for mode, want := range cases {
		m, l, _ := strings.Cut(mode, ":")
		level := 0
		if l != "" {
			level = int(l[0] - '0')
		}
		tpl, err := StemTemplate(templateOf(Presets["ext-year"].Spec, "*"), m, level)
		if err != nil {
			t.Fatal(err)
		}
		if got := render(t, tpl, r); got != want {
			t.Errorf("%s: %s (template %s) want %s", mode, got, tpl, want)
		}
	}
	// an existing "[origin] " prefix wins over the folder: re-running is idempotent
	r2 := rec(filepath.FromSlash("/x/MBOX/2020/[merge] posta.mbox"), "other", "2020-01-02 03:04:05", meta.Meta{})
	tpl, _ := StemTemplate(templateOf(Presets["ext-year"].Spec, "*"), StemPrefix, 2)
	if got := render(t, tpl, r2); got != "MBOX/2020/[merge] posta.mbox" {
		t.Errorf("idempotent prefix: %s", got)
	}
	// the name chosen inside a group with separators gets the prefix in each alternative
	tpl, err := StemTemplate(templateOf(Presets["photo-timestamp"].Spec, "photo,image"), StemPrefix, 1)
	if err != nil {
		t.Fatal(err)
	}
	ph := rec(filepath.FromSlash("/p/Trip/DSC_0001.jpg"), "photo", "2019-03-05 10:11:12", meta.Meta{Prefix: "DSC", Device: "Nikon"})
	if got := render(t, tpl, ph); got != "JPG/2019/[Trip] DSC_20190305_101112.jpg" {
		t.Errorf("photo-timestamp dated: %s (%s)", got, tpl)
	}
	ph = rec(filepath.FromSlash("/p/Trip/DSC_0001.jpg"), "photo", "", meta.Meta{})
	if got := render(t, tpl, ph); got != "JPG/Uncategorized/[Trip] DSC_0001.jpg" {
		t.Errorf("photo-timestamp undated: %s (%s)", got, tpl)
	}
	tpl, _ = StemTemplate(templateOf(Presets["date-tree"].Spec, "*"), StemPrefix, 1)
	if got := render(t, tpl, ph); got != "undated/[Trip] DSC_0001.jpg" {
		t.Errorf("date-tree: %s (%s)", got, tpl)
	}
	for _, bad := range [][2]any{{"prefix", 10}, {"bogus", 0}} {
		if _, err := StemTemplate("{stem}", bad[0].(string), bad[1].(int)); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

// Windows paths: a drive root counts as a folder named after its letter.
func TestAncestorWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("drive letters exist only on Windows")
	}
	p := `D:\MAILBOXES (mbox)\merge\posta.mbox`
	for n, want := range map[int]string{1: "merge", 2: "MAILBOXES (mbox)", 3: "D", 4: ""} {
		if got := Ancestor(p, n); got != want {
			t.Errorf("level %d: %q want %q", n, got, want)
		}
	}
	if got := Ancestor(`\\nas\share\x.txt`, 1); got != "share" {
		t.Errorf("UNC root: %q", got)
	}
}

func TestAncestor(t *testing.T) {
	p := filepath.FromSlash("/a/b/c.txt")
	for n, want := range map[int]string{1: "b", 2: "a", 3: "", 9: ""} {
		if runtime.GOOS == "windows" {
			break
		}
		if got := Ancestor(p, n); got != want {
			t.Errorf("level %d: %q want %q", n, got, want)
		}
	}
}

// Compose reproduces the composition of the GUI forms (formerly regime.js):
// a preset name when one preset covers every family, a selector map otherwise.
func TestCompose(t *testing.T) {
	ey := Choice{Preset: "ext-year"}
	all := func(c Choice) map[string]Choice {
		m := map[string]Choice{}
		for _, f := range Families {
			m[f.Key] = c
		}
		return m
	}
	extYear := templateOf(Presets["ext-year"].Spec, "*")
	docs := templateOf(Presets["documents"].Spec, docGroups)
	photo := templateOf(Presets["photo-timestamp"].Spec, "photo,image")
	pre2, _ := StemTemplate(docs, StemPrefix, 2)
	cases := []struct {
		name string
		fams map[string]Choice
		want string
	}{
		{"none", map[string]Choice{}, "ext-year"},
		{"all ext-year", all(ey), "ext-year"},
		{"all ext-year clean", all(Choice{Preset: "ext-year", Stem: "clean"}), "ext-year-clean"},
		{"all date-tree", all(Choice{Preset: "date-tree"}), "date-tree"},
		{"one family preset", map[string]Choice{"img": {Preset: "photo-timestamp"}, "doc": ey, "rest": ey}, "photo-timestamp"},
		{"only that family", map[string]Choice{"img": {Preset: "photo-timestamp"}}, "photo-timestamp"},
		{"two family presets", map[string]Choice{"img": {Preset: "photo-timestamp"}, "doc": {Preset: "documents"}, "rest": ey},
			"photo,image=" + photo + ";" + docGroups + "=" + docs + ";*=" + extYear},
		{"prefix on one family", map[string]Choice{"doc": {Preset: "documents", Stem: "prefix", Level: 2}, "rest": ey},
			docGroups + "=" + pre2 + ";*=" + extYear},
		{"everything else flat", map[string]Choice{"img": ey, "rest": {Preset: "ext-flat"}},
			"photo,image=" + extYear + ";*=" + templateOf(Presets["ext-flat"].Spec, "*")},
		{"all prefix", all(Choice{Preset: "ext-year", Stem: "prefix"}), mustStem(t, extYear, StemPrefix, 1)},
	}
	for _, c := range cases {
		got, err := Compose(c.fams)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s:\n got  %s\n want %s", c.name, got, c.want)
		}
		if _, err := Resolve(got); err != nil {
			t.Errorf("%s: composed spec does not resolve: %v", c.name, err)
		}
	}
	for name, bad := range map[string]map[string]Choice{
		"preset of another family": {"video": {Preset: "photo-timestamp"}},
		"unknown family":           {"pics": ey},
		"unknown stem":             {"img": {Preset: "ext-year", Stem: "upper"}},
		"level out of range":       {"img": {Preset: "ext-year", Stem: "prefix", Level: 12}},
	} {
		if _, err := Compose(bad); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if got := CatalogPreset(map[string]Choice{"doc": ey, "img": ey}); got != "image,document" {
		t.Errorf("catalog preset: %s", got)
	}
	if got := CatalogPreset(map[string]Choice{"doc": ey, "rest": ey}); got != "all" {
		t.Errorf("catalog preset with rest: %s", got)
	}
}

func mustStem(t *testing.T, tpl, mode string, level int) string {
	t.Helper()
	s, err := StemTemplate(tpl, mode, level)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

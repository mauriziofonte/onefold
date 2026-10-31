// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package gui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mauriziofonte/onefold/internal/testfix"
)

func wizardTree(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	old := time.Date(2020, 3, 4, 5, 6, 7, 0, time.Local)
	mk := func(rel string, f func(string) error) {
		p := filepath.Join(d, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := f(p); err != nil {
			t.Fatal(err)
		}
	}
	png := func(seed uint8) func(string) error {
		return func(p string) error { return testfix.PNG(p, 40, 30, seed) }
	}
	cp := func(src string) func(string) error {
		return func(p string) error {
			b, err := os.ReadFile(filepath.Join("..", "meta", "testdata", "photo", src))
			if err != nil {
				return err
			}
			return os.WriteFile(p, b, 0o644)
		}
	}
	mk("DCIM/IMG_0001.heic", cp("capture.heic"))
	mk("DCIM/IMG_0001 (1).heic", cp("capture.heic")) // copy name, same bytes
	mk("Shots/screen.png", png(1))
	mk("Shots/2021-06-04 meme.gif", func(p string) error { return testfix.File(p, "GIF89a fake", old) })
	mk("Docs/report.docx", func(p string) error { return testfix.Docx(p, "Report", "Ann", old, 2, "body") })
	mk("OneDrive/Foto/synced.png", png(2))
	mk("app/node_modules/logo.png", png(3))
	mk("Thumbs.db", func(p string) error { return testfix.File(p, "junk", old) })
	mk("tools/setup.exe", func(p string) error { return testfix.File(p, "MZ", old) })
	return d
}

func TestWizardScan(t *testing.T) {
	d := wizardTree(t)
	s := scanFolders([]string{d}, "merge")
	count := func(fam string, mask int) int64 { return s.Counts[fam+"|"+itoa(mask)][0] }
	// heic x2, screen.png, gif = img with no rule; synced.png (cloud=2), logo.png (dev=4)
	if count("img", 0) != 4 || count("img", 2) != 1 || count("img", 4) != 1 || count("doc", 0) != 1 {
		t.Fatalf("counts %v", s.Counts)
	}
	// Thumbs.db counts under the "junk" rule; setup.exe (system ext) never counts
	if s.Files != 8 || s.Rules["cloud"] != 1 || s.Rules["dev"] != 1 || s.Rules["junk"] != 1 || s.Cases["copies"] != 1 || s.Cases["dated"] < 1 {
		t.Fatalf("scan %+v", s)
	}
	if s.Golden["in|0"][0] != 3 || s.Golden["gifbmp|0"][0] != 1 {
		t.Fatalf("golden %v", s.Golden)
	}
	var rels []string
	for _, x := range s.Sample {
		rels = append(rels, x.Rel)
	}
	for _, want := range []string{"DCIM/IMG_0001 (1).heic", "DCIM/IMG_0001.heic", "OneDrive/Foto/synced.png", "Docs/report.docx"} {
		if !strings.Contains(strings.Join(rels, "\n"), want) {
			t.Errorf("sample misses %s: %v", want, rels)
		}
	}
}

func TestWizardPreview(t *testing.T) {
	d := wizardTree(t)
	s := scanFolders([]string{d}, "merge")
	rules := map[string]bool{"system": true, "cloud": true, "dev": true, "junk": true, "big": true}
	byRel := func(rows []wizardRow) map[string]wizardRow {
		m := map[string]wizardRow{}
		for _, r := range rows {
			m[r.Src] = r
		}
		return m
	}
	tag := func(r wizardRow, k string) bool {
		for _, x := range r.Tags {
			if x.K == k {
				return true
			}
		}
		return false
	}

	rows, err := previewSample(wizardPreviewReq{Goal: "golden", Sample: s.Sample, Rules: rules})
	if err != nil {
		t.Fatal(err)
	}
	g := byRel(rows)
	// identical copies: one winner (which one is the frozen media rule's choice), one recorded drop
	h, c := g["DCIM/IMG_0001.heic"], g["DCIM/IMG_0001 (1).heic"]
	if h.Dup {
		h, c = c, h
	}
	if h.Dst != filepath.FromSlash("PICTURES/HEIC/2023/IMG_20230514_101233.heic") || !tag(h, "canon") || !c.Dup || !tag(c, "sameShot") {
		t.Errorf("golden heic pair: %+v %+v", h, c)
	}
	if x := g["Docs/report.docx"]; !x.Skip || !tag(x, "outGolden") {
		t.Errorf("golden docx: %+v", x)
	}
	if x := g["OneDrive/Foto/synced.png"]; !x.Skip || !tag(x, "excl") {
		t.Errorf("golden cloud: %+v", x)
	}

	rows, err = previewSample(wizardPreviewReq{Goal: "merge", Sample: s.Sample, Rules: rules, Fams: []string{"img"},
		Naming: "photo,image={EXT/{stem-norm}"})
	if err == nil {
		t.Fatal("an invalid selector map must be reported")
	}
	rows, err = previewSample(wizardPreviewReq{Goal: "merge", Sample: s.Sample, Rules: rules, Fams: []string{"img"}, Naming: "ext-year"})
	if err != nil {
		t.Fatal(err)
	}
	m := byRel(rows)
	if h := m["DCIM/IMG_0001.heic"]; h.Dst != filepath.FromSlash("HEIC/2023/IMG_0001.heic") {
		t.Errorf("merge heic: %+v", h)
	}
	if c := m["DCIM/IMG_0001 (1).heic"]; !c.Dup || !tag(c, "dupSkip") {
		t.Errorf("merge copy: %+v", c)
	}
	if x := m["Docs/report.docx"]; !x.Skip || !tag(x, "notSelected") {
		t.Errorf("merge docx: %+v", x)
	}
	if _, err := previewSample(wizardPreviewReq{Goal: "merge", Sample: s.Sample, Naming: "library-files"}); err == nil {
		t.Error("golden tree preset accepted in merge")
	}

	rows, _ = previewSample(wizardPreviewReq{Goal: "rearrange", Sample: s.Sample, Rules: rules, Fams: []string{"rest"}, Naming: "ext-year", Dupes: "delete"})
	if c := byRel(rows)["DCIM/IMG_0001 (1).heic"]; !c.Del || !tag(c, "dupDelete") {
		t.Errorf("rearrange copy: %+v", c)
	}
}

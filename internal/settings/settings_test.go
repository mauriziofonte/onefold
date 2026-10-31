// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package settings

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/naming"
)

// Jobs of older GUI versions (naming + clean + filters.preset) keep working;
// families drive scope and naming; everything else includes unknown and
// extension-less files unless a rule names them.
func TestMergeSettings(t *testing.T) {
	d := t.TempDir()
	old := `{"inputs":["` + jsonPath(d) + `"],"output":"` + jsonPath(d) + `","naming":"ext-flat","clean":true,
		"filters":{"preset":"image,document","maxSize":"0","skipDevDirs":false,"keepJunk":true,"exclude":"Backup"},"workers":3}`
	var m Merge
	if err := json.Unmarshal([]byte(old), &m); err != nil {
		t.Fatal(err)
	}
	o, err := m.Options(false)
	if err != nil {
		t.Fatal(err)
	}
	if o.Naming != "ext-flat-clean" || o.Catalog.Exts == nil || o.Catalog.SkipDevDirs || !o.Catalog.KeepJunk || o.Catalog.MaxSize != 0 ||
		o.Catalog.Workers != 3 || len(o.Catalog.Exclude) != 1 {
		t.Fatalf("legacy job: %+v", o)
	}

	m = Merge{Inputs: []string{d}, Output: d, Naming: Naming{Families: map[string]naming.Choice{
		"img": {Preset: "photo-timestamp"}, "rest": {Preset: "ext-year", Stem: "prefix", Level: 2}}}}
	o, err = m.Options(false)
	if err != nil {
		t.Fatal(err)
	}
	if o.Catalog.Exts != nil || o.Catalog.SkipUnknownExt || o.Catalog.SkipNoExt || !o.Catalog.SkipSystemExt || !o.Catalog.SkipTransient {
		t.Fatalf("everything else: %+v", o.Catalog)
	}
	if !strings.Contains(o.Naming, "photo,image=") || !strings.Contains(o.Naming, "origin-2") {
		t.Fatalf("composed naming: %s", o.Naming)
	}
	m.Filters.Rules = map[string]bool{catalog.RuleUnknownExt: true}
	if o, _ = m.Options(false); !o.Catalog.SkipUnknownExt || o.Catalog.SkipNoExt {
		t.Fatalf("explicit rule: %+v", o.Catalog)
	}
	m.Families = map[string]naming.Choice{"doc": {Preset: "documents"}}
	m.Filters.Rules = nil
	if o, _ = m.Options(false); o.Catalog.Exts == nil || !o.Catalog.Exts["pdf"] || o.Catalog.Exts["jpg"] {
		t.Fatalf("documents only: %d exts", len(o.Catalog.Exts))
	}
	// free template keeps the families' scope
	m.Template = "{EXT}/{stem}<.{ext}>"
	if o, _ = m.Options(false); o.Naming != m.Template || o.Catalog.Exts["jpg"] {
		t.Fatalf("template: %s", o.Naming)
	}

	// the regime and the family checks
	refused := []Merge{
		{Inputs: []string{d}, Output: d, Filters: catalog.Filters{Preset: "media"}},
		{Inputs: []string{d}, Output: d, Naming: Naming{Spec: "library-files"}},
		{Inputs: []string{d}, Output: d, Naming: Naming{Layout: "dedupe"}},
	}
	for i, r := range refused {
		if _, err := r.Options(false); !errors.Is(err, catalog.ErrMixed) {
			t.Errorf("refused %d: %v", i, err)
		}
		if _, err := r.Options(true); err != nil {
			t.Errorf("allow-mixed %d: %v", i, err)
		}
	}
	bad := []Merge{
		{Inputs: []string{d}, Output: d, Naming: Naming{Families: map[string]naming.Choice{"img": {Preset: "music"}}}},
		{Inputs: []string{d}, Output: d, Filters: catalog.Filters{Preset: "image"}, Naming: Naming{Families: map[string]naming.Choice{"img": {Preset: "ext-year"}}}},
		{Inputs: []string{d}, Output: d, Filters: catalog.Filters{Rules: map[string]bool{"bogus": true}}},
		{Inputs: []string{d}, Output: d, Naming: Naming{Spec: "{nope}"}},
		{Output: d},
	}
	for i, b := range bad {
		if _, err := b.Options(false); err == nil {
			t.Errorf("bad %d accepted", i)
		}
	}
}

func TestLoad(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "s.json")
	os.WriteFile(p, []byte(`{"kind":"merge","inputs":["/a"],"output":"/o","families":{"doc":{"preset":"documents","stem":"prefix"}},"filters":{"rules":{"shortcuts":false}}}`), 0o644)
	var m Merge
	if err := Load(p, "merge", &m); err != nil {
		t.Fatal(err)
	}
	if m.Families["doc"].Stem != "prefix" || m.Filters.Rules["shortcuts"] {
		t.Fatalf("%+v", m)
	}
	var r Rearrange
	if err := Load(p, "rearrange", &r); err == nil {
		t.Fatal("a merge file loaded as rearrange")
	}
	os.WriteFile(p, []byte(`{"inputs":["/a"],"outptu":"/o"}`), 0o644)
	if err := Load(p, "merge", &m); err == nil {
		t.Fatal("unknown key accepted")
	}
}

func TestRearrangeSettings(t *testing.T) {
	d := t.TempDir()
	r := Rearrange{Root: d, ReportDupes: true, Naming: Naming{Families: map[string]naming.Choice{"music": {Preset: "music", Stem: "raw"}}}}
	o, err := r.Options(false)
	if err != nil {
		t.Fatal(err)
	}
	if o.DeleteDupes || o.Catalog.Exts == nil || !strings.Contains(o.Naming, "{title|stem}") {
		t.Fatalf("%+v", o)
	}
}

func jsonPath(p string) string { return strings.ReplaceAll(p, `\`, `\\`) }

func TestRearrangeRefusesKeepDotfiles(t *testing.T) {
	r := Rearrange{Root: t.TempDir()}
	r.Filters.KeepDotfiles = true
	if _, err := r.Options(false); err == nil {
		t.Fatal("keepDotfiles accepted for rearrange")
	}
}

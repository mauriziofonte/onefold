// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package catalog

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/mauriziofonte/onefold/internal/testfix"
)

func TestGlob(t *testing.T) {
	cases := []struct {
		pat  string
		rel  string
		want bool
	}{
		{"*.data", "COMP1/DRIVERS/x/nvdata.data", true},
		{"*.data", "COMP1/nvdata.database", false},
		{"DRIVERS", "COMP1/DRIVERS", true}, // a name: any folder so named
		{"DRIVERS", "COMP1/DRIVERS-old", false},
		{"**/DRIVERS/**", "COMP1/DRIVERS", true},
		{"**/DRIVERS/**", "COMP1/DRIVERS/a/b.txt", true},
		{"**/DRIVERS/**", "DRIVERS", true},
		{"COMP1/DRIVERS", "COMP1/DRIVERS", true},
		{"COMP1/DRIVERS", "X/COMP1/DRIVERS", false},
		{`COMP1\**\*.pb`, `COMP1\DRIVERS\Display\nvinfo.pb`, true},
		{"COMP1/**/*.pb", "COMP2/a.pb", false},
		{"~*", "dir/~$lock.docx", true},
		{"a?c/[xy].txt", "abc/x.txt", true},
		{"a?c/[xy].txt", "abc/z.txt", false},
	}
	for _, c := range cases {
		g, err := CompileGlob(c.pat)
		if err != nil {
			t.Fatalf("%s: %v", c.pat, err)
		}
		if got := g.Match(c.rel); got != c.want {
			t.Errorf("%s ~ %s = %v, want %v", c.pat, c.rel, got, c.want)
		}
	}
	if g, _ := CompileGlob("*.PDF"); g.Match("a.pdf") != (runtime.GOOS == "windows") {
		t.Error("case folding must follow the OS")
	}
	for _, bad := range []string{"", "  ", "a/[b", "/"} {
		if _, err := CompileGlob(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// Exact paths and patterns leave files and whole folders out at walk time,
// with their own reason; a folder path stops at a component boundary.
func TestExcludePathsAndGlobs(t *testing.T) {
	room := filepath.Join(t.TempDir(), "IN")
	for _, p := range []string{"COMP1/DRIVERS/Display/nvdata.data", "COMP1/DRIVERS/Display/license.txt", "COMP1/DRIVERS-old/keep.txt",
		"COMP1/doc.pdf", "COMP1/one.txt", "two.data", "notes/a.txt"} {
		if err := testfix.File(filepath.Join(room, filepath.FromSlash(p)), "content "+p, t0); err != nil {
			t.Fatal(err)
		}
	}
	f := Filters{ExcludePaths: []string{filepath.Join(room, "COMP1", "DRIVERS"), filepath.Join(room, "COMP1", "one.txt")},
		ExcludeGlobs: []string{"*.data", "notes/**"}}
	o, err := f.Options("all")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	o.OnSkip = func(s Skip) {
		rel, _ := filepath.Rel(room, s.Path)
		got[filepath.ToSlash(rel)] = s.Rule
	}
	recs, _ := run(t, []string{room}, o)
	eq(t, rels(room, recs), []string{"COMP1/DRIVERS-old/keep.txt", "COMP1/doc.pdf"})
	want := map[string]string{"COMP1/DRIVERS": ReasonExcludePath, "COMP1/one.txt": ReasonExcludePath, "two.data": ReasonExcludeGlob,
		"notes": ReasonExcludeGlob}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %q want %q (all: %v)", k, got[k], v, got)
		}
	}
	if _, err := (Filters{ExcludeGlobs: []string{"a/[b"}}).Options("all"); err == nil {
		t.Fatal("bad pattern accepted")
	}
	if !o.HasUserExclusions() || o.Unfiltered().HasUserExclusions() {
		t.Fatal("HasUserExclusions / Unfiltered")
	}
	if r := o.UserExclusion(room, filepath.Join(room, "COMP1", "DRIVERS", "x", "y.bin")); r != ReasonExcludePath {
		t.Fatalf("UserExclusion under a folder: %q", r)
	}
}

// Driver and installer package folders are flagged: the markers count
// every file seen, also the ones the rules skip; only folders with cataloged
// files are reported, never excluded.
func TestPackages(t *testing.T) {
	room := filepath.Join(t.TempDir(), "IN")
	for _, p := range []string{"DRIVERS/Display/nvdisp.inf", "DRIVERS/Display/nvlddmkm.sys", "DRIVERS/Display/license.txt",
		"DRIVERS/Display/nvdata.data", "Tools/App/Setup.exe", "Tools/App/readme.txt", "Docs/a.txt", "Lone/setup.exe"} {
		if err := testfix.File(filepath.Join(room, filepath.FromSlash(p)), "content "+p, t0); err != nil {
			t.Fatal(err)
		}
	}
	o, err := Filters{}.Options("all")
	if err != nil {
		t.Fatal(err)
	}
	_, sum := run(t, []string{room}, o)
	if len(sum.Packages) != 2 {
		t.Fatalf("packages %+v", sum.Packages)
	}
	d, i := sum.Packages[0], sum.Packages[1]
	if d.Kind != "driver" || d.Dir != filepath.Join(room, "DRIVERS", "Display") || d.Files != 2 { // license.txt, nvdata.data: the .inf and the .sys are system-ext
		t.Fatalf("driver %+v", d)
	}
	if i.Kind != "installer" || i.Files != 1 { // Setup.exe skipped (system-ext), readme.txt cataloged
		t.Fatalf("installer %+v", i)
	}
	recs := []Record{{Path: filepath.Join(room, "DRIVERS", "Display", "license.txt"), Size: 7}}
	if r := RecountPackages(sum.Packages, recs); len(r) != 1 || r[0].Files != 1 || r[0].Bytes != 7 {
		t.Fatalf("recount %+v", r)
	}
}

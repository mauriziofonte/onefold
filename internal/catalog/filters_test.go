// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package catalog

import (
	"testing"

	"github.com/mauriziofonte/onefold/internal/kinds"
)

func TestFiltersDefaults(t *testing.T) {
	off := false
	cases := []struct {
		name    string
		f       Filters
		maxSize int64
		pruned  int
		skipExt bool
	}{
		{"defaults", Filters{}, 50 << 30, len(kinds.SystemDirs) + len(kinds.CloudDirs) + len(kinds.DevDirs), true},
		{"one rule off", Filters{SkipDevDirs: &off}, 50 << 30, len(kinds.SystemDirs) + len(kinds.CloudDirs), true},
		{"no sweep", Filters{NoSweep: true}, 0, 0, false},
		{"no sweep keeps an explicit cap", Filters{NoSweep: true, MaxSize: "1M"}, 1 << 20, 0, false},
		{"no cap", Filters{MaxSize: "0"}, 0, len(kinds.SystemDirs) + len(kinds.CloudDirs) + len(kinds.DevDirs), true},
	}
	for _, c := range cases {
		o, err := c.f.Options("all")
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if o.MaxSize != c.maxSize || len(o.pruneRules()) != c.pruned || o.SkipSystemExt != c.skipExt || o.SkipNoExt != c.skipExt ||
			o.SkipTransient != c.skipExt || o.SkipShortcuts != c.skipExt {
			t.Errorf("%s: max %d prune %d skipExt %v/%v transient %v shortcuts %v", c.name, o.MaxSize, len(o.pruneRules()),
				o.SkipSystemExt, o.SkipNoExt, o.SkipTransient, o.SkipShortcuts)
		}
		if o.WholeMax != 320<<10 || o.Exts != nil {
			t.Errorf("%s: wholeMax %d exts %v", c.name, o.WholeMax, o.Exts)
		}
	}
	// Rules by name win over the historical fields and over NoSweep
	o, err := Filters{SkipDevDirs: &off, NoSweep: true, Rules: map[string]bool{RuleDevDirs: true, RuleJunk: false, RuleBig: true}}.Options("all")
	if err != nil || !o.SkipDevDirs || o.SkipCloudDirs || !o.KeepJunk || o.MaxSize != 50<<30 || o.SkipTransient {
		t.Fatalf("rules by name: %v %+v", err, o)
	}
	if _, err := (Filters{Rules: map[string]bool{"nope": true}}).Options("all"); err == nil {
		t.Fatal("unknown rule accepted")
	}
	set := map[string]bool{}
	if err := ParseRules("transient, Shortcuts", true, set); err != nil || !set[RuleTransient] || !set[RuleShortcuts] || len(set) != 2 {
		t.Fatalf("ParseRules: %v %v", err, set)
	}
	if err := ParseRules("all", false, set); err != nil || len(set) != len(Rules) || set[RuleJunk] {
		t.Fatalf("ParseRules all: %v %v", err, set)
	}
	if err := ParseRules("junk,bogus", true, set); err == nil {
		t.Fatal("ParseRules accepted an unknown rule")
	}
	o, err = Filters{Ext: ".PDF, txt"}.Options("media")
	if err != nil || len(o.Exts) != 2 || !o.Exts["pdf"] || !o.Exts["txt"] {
		t.Fatalf("explicit ext: %v %v", err, o.Exts)
	}
	if _, err := (Filters{Preset: "nope"}).Options("all"); err == nil {
		t.Fatal("unknown preset accepted")
	}
}

func TestParseSize(t *testing.T) {
	for in, want := range map[string]int64{"": 0, "0": 0, "512": 512, "2k": 2 << 10, "2KB": 2 << 10, "3M": 3 << 20, "50G": 50 << 30, "1T": 1 << 40, "7B": 7} {
		if got, err := ParseSize(in); err != nil || got != want {
			t.Errorf("ParseSize(%q) = %d, %v want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"x", "-1", "G", "1.5G"} {
		if _, err := ParseSize(in); err == nil {
			t.Errorf("ParseSize(%q) accepted", in)
		}
	}
}

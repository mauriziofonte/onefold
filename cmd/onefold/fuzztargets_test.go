// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TestFuzzTargetsBuilt: .clusterfuzzlite/build.sh lists every native fuzz
// target of the repository (package and function), and nothing else, so a
// new target is fuzzed by ClusterFuzzLite as soon as it is written.
func TestFuzzTargetsBuilt(t *testing.T) {
	root := filepath.Join("..", "..")
	fuzzFn := regexp.MustCompile(`(?m)^func (Fuzz\w+)\(f \*testing\.F\)`)
	var have []string
	err := filepath.WalkDir(filepath.Join(root, "internal"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, "_test.go") {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(filepath.Join(root, "internal"), filepath.Dir(p))
		for _, m := range fuzzFn.FindAllStringSubmatch(string(b), -1) {
			have = append(have, filepath.ToSlash(rel)+" "+m[1])
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, ".clusterfuzzlite", "build.sh"))
	if err != nil {
		t.Fatal(err)
	}
	_, list, ok := strings.Cut(string(b), "<<'TARGETS'\n")
	list, _, _ = strings.Cut(list, "\nTARGETS")
	if !ok {
		t.Fatal("build.sh has no TARGETS list")
	}
	var built []string
	for _, l := range strings.Split(list, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			built = append(built, l)
		}
	}
	sort.Strings(have)
	sort.Strings(built)
	if strings.Join(have, "\n") != strings.Join(built, "\n") {
		t.Errorf("fuzz targets:\n%s\nbuild.sh lists:\n%s", strings.Join(have, "\n"), strings.Join(built, "\n"))
	}
	if len(have) < 20 {
		t.Errorf("only %d fuzz targets found", len(have))
	}
}

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
	"strings"
	"testing"
)

// privateRef matches references to the maintainers' private planning notes
// (decision and contract ids, ADR numbers): they mean nothing to a reader of
// this repository, so comments explain the reason instead.
var privateRef = regexp.MustCompile(`\(D\d{1,3}[),;]|, D\d{1,3}\)|ADR[ -]?\d{4}|[Cc]ontract [A-Z]\d|^\s*(//|\*)\s*[KMRWNTSAXUV]\d{1,2}(\s*[+/]\s*[A-Z]?\d+)*:`)

// TestSourceHeaders: every source file of the repository carries the project
// header (author, copyright, license), and no comment cites a private note.
// Generated output, fixtures and dependencies are not sources.
func TestSourceHeaders(t *testing.T) {
	root := filepath.Join("..", "..")
	want := []string{"onefold", "Author: Maurizio Fonte <fonte.maurizio@gmail.com>",
		"Copyright (c) 2026 Maurizio Fonte", "SPDX-License-Identifier: AGPL-3.0-or-later"}
	skipDirs := map[string]bool{".git": true, ".claude": true, "node_modules": true, "dist": true, "testdata": true,
		"test-results": true, "playwright-report": true, "census-final": true}
	sources := map[string]bool{".go": true, ".js": true, ".mjs": true, ".svelte": true, ".html": true, ".css": true,
		".sh": true, ".yml": true, ".yaml": true}
	n := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !sources[filepath.Ext(p)] && d.Name() != "Makefile" {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		head := string(b)
		if len(head) > 600 {
			head = head[:600]
		}
		for _, w := range want {
			if !strings.Contains(head, w) {
				t.Errorf("%s: header line %q missing", p, w)
			}
		}
		for i, line := range strings.Split(string(b), "\n") {
			c := strings.Index(line, "//")
			if c < 0 {
				continue
			}
			if privateRef.MatchString(line[c:]) {
				t.Errorf("%s:%d: comment cites a private note: %s", p, i+1, strings.TrimSpace(line))
			}
		}
		n++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("no source files found")
	}
}

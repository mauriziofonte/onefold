// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// reSemVer: MAJOR.MINOR.PATCH with an optional pre-release (no build
// metadata: that is added to development builds by the Makefile).
var reSemVer = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)

// VERSION holds one SemVer version and CHANGELOG.md has its section: the
// release workflow refuses a tag otherwise, this catches it before tagging.
func TestVersionFile(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	v := strings.TrimSpace(string(b))
	if !reSemVer.MatchString(v) || string(b) != v+"\n" {
		t.Fatalf("VERSION %q: one SemVer version without a v and one newline (0.1.0, 1.0.0-rc.1)", b)
	}
	cl, err := os.ReadFile(filepath.Join("..", "..", "CHANGELOG.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cl), "\n## ["+v+"]") {
		t.Fatalf("CHANGELOG.md has no section ## [%s]", v)
	}
}

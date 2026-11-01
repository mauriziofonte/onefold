// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A Mac program started from Finder has no Homebrew on PATH: tools installed
// with brew are still found.
func TestHomebrewWithoutPath(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS only")
	}
	var brewed string
	for _, d := range []string{"/opt/homebrew/bin", "/usr/local/bin"} {
		if _, err := os.Stat(filepath.Join(d, "exiftool")); err == nil {
			brewed = filepath.Join(d, "exiftool")
			break
		}
	}
	if brewed == "" {
		t.Skip("exiftool not installed with Homebrew")
	}
	t.Setenv("PATH", "/usr/bin:/bin")
	if got := resolve("exiftool"); got != brewed {
		t.Fatalf("resolve(exiftool) = %q, want %q", got, brewed)
	}
}

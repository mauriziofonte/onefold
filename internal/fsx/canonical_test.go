// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package fsx

import (
	"os"
	"path/filepath"
	"testing"
)

// A folder reached through a symbolic link and through its real name gets one
// spelling, also for a path below it that does not exist yet.
func TestCanonical(t *testing.T) {
	real, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(real, "data", "in"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(real, "link")
	if err := os.Symlink(filepath.Join(real, "data"), link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	for _, c := range []struct{ in, want string }{
		{filepath.Join(link, "in"), filepath.Join(real, "data", "in")},
		{filepath.Join(link, "out", "new"), filepath.Join(real, "data", "out", "new")}, // not created yet
		{filepath.Join(real, "data", ".", "in", ".."), filepath.Join(real, "data")},
	} {
		got, err := Canonical(c.in)
		if err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("Canonical(%s) = %s, want %s", c.in, got, c.want)
		}
	}
	if _, err := os.Stat(filepath.Join(real, "data", "out")); !os.IsNotExist(err) {
		t.Fatal("Canonical created a folder")
	}
}

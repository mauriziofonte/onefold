// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package statedir

import (
	"path/filepath"
	"runtime"
	"testing"
)

// Each OS keeps the state where its users and backup tools expect it; the
// override always wins.
func TestDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("XDG_STATE_HOME", "")
	t.Setenv("ONEFOLD_STATE_DIR", "")
	want := map[string]string{
		"windows": filepath.Join(home, "AppData", "Local", "onefold"),
		"darwin":  filepath.Join(home, "Library", "Application Support", "onefold"),
	}[runtime.GOOS]
	if want == "" {
		want = filepath.Join(home, ".local", "state", "onefold")
	}
	if d, err := Dir(); err != nil || d != want {
		t.Fatalf("Dir() = %q, %v; want %q", d, err, want)
	}
	over := filepath.Join(home, "elsewhere")
	t.Setenv("ONEFOLD_STATE_DIR", over)
	if d, err := Dir(); err != nil || d != over {
		t.Fatalf("with ONEFOLD_STATE_DIR: %q, %v; want %q", d, err, over)
	}
}

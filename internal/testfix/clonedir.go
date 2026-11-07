// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package testfix

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// CloneDir is a fresh folder on a file system that clones files, or "" when
// none is at hand: $ONEFOLD_CLONE_DIR (CI mounts a Btrfs image there on
// Linux), else the temp dir on macOS (APFS). Removed after the test.
func CloneDir(t *testing.T) string {
	t.Helper()
	if d := os.Getenv("ONEFOLD_CLONE_DIR"); d != "" {
		dir, err := os.MkdirTemp(d, "clone-test")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(dir) })
		if c, err := filepath.EvalSymlinks(dir); err == nil {
			return c
		}
		return dir
	}
	if runtime.GOOS == "darwin" {
		return TempDir(t)
	}
	return ""
}

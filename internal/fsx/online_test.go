// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package fsx

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeInfo is a FileInfo with the platform data a test chooses.
type fakeInfo struct{ sys any }

func (fakeInfo) Name() string       { return "x" }
func (fakeInfo) Size() int64        { return 1 }
func (fakeInfo) Mode() fs.FileMode  { return 0 }
func (fakeInfo) ModTime() time.Time { return time.Time{} }
func (fakeInfo) IsDir() bool        { return false }
func (f fakeInfo) Sys() any         { return f.sys }

func TestOnlineOnlyLocalFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(p, []byte("here"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	if OnlineOnly(fi) || OnlineOnly(nil) || OnlineOnly(fakeInfo{}) {
		t.Fatal("a local file reported online-only")
	}
}

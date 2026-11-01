// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package fsx

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// RenameNoReplace never replaces a file: an existing target is ErrExists and
// both files keep their bytes; a free target receives the file.
func TestRenameNoReplace(t *testing.T) {
	d := t.TempDir()
	src, dst := filepath.Join(d, "a"), filepath.Join(d, "b")
	if err := os.WriteFile(src, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := RenameNoReplace(src, dst); !errors.Is(err, ErrExists) {
		t.Fatalf("existing target: %v, want ErrExists", err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "old" {
		t.Fatalf("target changed: %q", b)
	}
	if b, _ := os.ReadFile(src); string(b) != "new" {
		t.Fatalf("source changed: %q", b)
	}
	free := filepath.Join(d, "c")
	if err := RenameNoReplace(src, free); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(src); !os.IsNotExist(err) {
		t.Fatalf("source still there: %v", err)
	}
	if b, _ := os.ReadFile(free); string(b) != "new" {
		t.Fatalf("moved file: %q", b)
	}
}

// On the systems onefold ships for, the temp folder's file system takes the
// one-call native rename (no link-and-remove fallback).
func TestRenameNoReplaceNative(t *testing.T) {
	switch runtime.GOOS {
	case "linux", "darwin", "windows":
	default:
		t.Skip("no native no-replace rename here")
	}
	d := t.TempDir()
	src, dst := filepath.Join(d, "a"), filepath.Join(d, "b")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := renameNoReplace(src, dst); !errors.Is(err, ErrExists) {
		t.Fatalf("native rename onto a file: %v, want ErrExists", err)
	}
	if err := renameNoReplace(src, filepath.Join(d, "c")); err != nil {
		t.Fatalf("native rename: %v", err)
	}
}

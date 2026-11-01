// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package fsx

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLinkNoReplace(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.jpg")
	if err := os.WriteFile(src, []byte("original bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "golden.jpg")
	if err := LinkNoReplace(src, dst); err != nil {
		t.Fatal(err)
	}
	si, _ := os.Stat(src)
	di, _ := os.Stat(dst)
	if !os.SameFile(si, di) {
		t.Fatal("dst is not a hard link of src")
	}

	// an existing destination is never replaced, whatever it holds
	other := filepath.Join(dir, "other.jpg")
	if err := os.WriteFile(other, []byte("someone else"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LinkNoReplace(src, other); !errors.Is(err, ErrExists) {
		t.Fatalf("existing dst: err = %v, want ErrExists", err)
	}
	if b, _ := os.ReadFile(other); string(b) != "someone else" {
		t.Fatalf("existing dst changed: %q", b)
	}
	if b, _ := os.ReadFile(src); string(b) != "original bytes" {
		t.Fatalf("src changed: %q", b)
	}

	// a missing source is an error, not ErrExists, and creates nothing
	gone := filepath.Join(dir, "nope.jpg")
	if err := LinkNoReplace(filepath.Join(dir, "missing"), gone); err == nil || errors.Is(err, ErrExists) {
		t.Fatalf("missing src: err = %v", err)
	}
	if _, err := os.Lstat(gone); !os.IsNotExist(err) {
		t.Fatal("a failed link left a name behind")
	}
}

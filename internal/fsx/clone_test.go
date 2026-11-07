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

	"github.com/mauriziofonte/onefold/internal/testfix"
)

func TestClone(t *testing.T) {
	dir := testfix.CloneDir(t)
	if dir == "" {
		// no clone-capable file system here: the fallback signal is the contract
		src, dst := filepath.Join(t.TempDir(), "a"), filepath.Join(t.TempDir(), "b")
		if err := os.WriteFile(src, []byte("bytes"), 0o644); err != nil {
			t.Fatal(err)
		}
		err := Clone(src, dst)
		if err != nil && !errors.Is(err, ErrNoClone) {
			t.Fatalf("Clone on a file system without clones: %v, want ErrNoClone", err)
		}
		if err != nil {
			if _, serr := os.Lstat(dst); serr == nil {
				t.Fatal("a failed clone left its destination")
			}
		}
		t.Skip("no clone-capable file system (set ONEFOLD_CLONE_DIR to a Btrfs/XFS folder)")
	}
	src, dst := filepath.Join(dir, "src.bin"), filepath.Join(dir, "dst.bin")
	body := make([]byte, 3<<20+17)
	for i := range body {
		body[i] = byte(i * 7)
	}
	if err := os.WriteFile(src, body, 0o444); err != nil {
		t.Fatal(err)
	}
	if err := Clone(src, dst); err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if err := Clone(src, dst); err == nil {
		t.Fatal("Clone replaced an existing file")
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != string(body) {
		t.Fatalf("clone content differs (%v)", err)
	}
	// a separate file: writing it leaves the source as it was
	if err := os.WriteFile(dst, []byte("changed"), 0o644); err != nil {
		t.Fatalf("the clone is not writable: %v", err)
	}
	if b, _ := os.ReadFile(src); string(b) != string(body) {
		t.Fatal("writing the clone changed the source")
	}
}

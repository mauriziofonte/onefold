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

func TestSyncFileAndDir(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SyncFile(p); err != nil {
		t.Fatalf("SyncFile: %v", err)
	}
	if err := SyncDir(dir); err != nil {
		t.Fatalf("SyncDir: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "x" {
		t.Fatalf("SyncFile changed the file: %q", b)
	}
	if SyncFile(filepath.Join(dir, "missing")) == nil {
		t.Error("SyncFile of a missing file: no error")
	}
	if SyncDir(filepath.Join(dir, "missing")) == nil {
		t.Error("SyncDir of a missing folder: no error")
	}
}

func TestDurableOncePerDir(t *testing.T) {
	dir := t.TempDir()
	var seen DirSet
	for _, n := range []string{"a", "b"} {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, []byte(n), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := Durable(p, &seen); err != nil {
			t.Fatalf("Durable(%s): %v", n, err)
		}
	}
	if len(seen.dirs) != 1 || !seen.dirs[dir] {
		t.Fatalf("seen = %v, want only %s", seen.dirs, dir)
	}
	if Durable(filepath.Join(dir, "missing"), &seen) == nil {
		t.Error("Durable of a missing file: no error")
	}
	if err := seen.SyncAll(); err != nil {
		t.Fatalf("SyncAll: %v", err)
	}
	if seen.dirs != nil {
		t.Error("SyncAll did not empty the set")
	}
	seen.Add(filepath.Join(dir, "gone", "x"))
	if err := seen.SyncAll(); err != nil {
		t.Errorf("SyncAll of a folder that no longer exists: %v", err)
	}
}

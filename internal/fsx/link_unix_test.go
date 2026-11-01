// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux || darwin

package fsx

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestLinkNoReplaceCrossVolume needs two writable directories on different
// devices: the test temp dir and /dev/shm (Linux tmpfs) usually are.
func TestLinkNoReplaceCrossVolume(t *testing.T) {
	a := t.TempDir()
	b, err := os.MkdirTemp("/dev/shm", "onefold-xdev-")
	if err != nil {
		t.Skip("no /dev/shm to link across volumes")
	}
	defer os.RemoveAll(b)
	var sa, sb syscall.Stat_t
	if syscall.Stat(a, &sa) != nil || syscall.Stat(b, &sb) != nil || sa.Dev == sb.Dev {
		t.Skip("temp dir and /dev/shm are on the same device")
	}
	src := filepath.Join(a, "src")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(b, "dst")
	if err := LinkNoReplace(src, dst); !errors.Is(err, ErrCrossVolume) {
		t.Fatalf("cross-volume link: err = %v, want ErrCrossVolume", err)
	}
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Fatal("a refused link left a name behind")
	}
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package fsx

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/mauriziofonte/onefold/internal/testfix"
)

func TestOnlineOnlyAttributes(t *testing.T) {
	for attr, want := range map[uint32]bool{0x20: false, 0x1000: true, 0x40000: true, 0x400000: true, 0x400020: true} {
		if got := OnlineOnly(fakeInfo{&syscall.Win32FileAttributeData{FileAttributes: attr}}); got != want {
			t.Errorf("attributes %#x: %v, want %v", attr, got, want)
		}
	}
	// FILE_ATTRIBUTE_OFFLINE is the one of these an application may set: a
	// real file carrying it is seen through both Lstat and a directory listing
	dir := t.TempDir()
	p := filepath.Join(dir, "away.bin")
	if err := os.WriteFile(p, []byte("elsewhere"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := testfix.SetOffline(p); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(p)
	if err != nil || !OnlineOnly(fi) {
		t.Fatalf("Lstat of an offline file: %v, %v", OnlineOnly(fi), err)
	}
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fi, err := es[0].Info(); err != nil || !OnlineOnly(fi) {
		t.Fatalf("listing of an offline file: %v", err)
	}
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package purge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mauriziofonte/onefold/internal/testfix"
)

// A file sent to the Trash leaves its folder and lands, whole, in a Trash
// folder; the test then removes it from there. A file that does not exist
// is an error, not a silent success.
func TestTrashItem(t *testing.T) {
	if os.Getenv("ONEFOLD_TEST_TRASH") == "" {
		t.Skip("writes into the Trash: set ONEFOLD_TEST_TRASH=1")
	}
	dir := testfix.TempDir(t)
	src := filepath.Join(dir, "onefold trash test é.txt")
	if err := os.WriteFile(src, []byte("synthetic"), 0o644); err != nil {
		t.Fatal(err)
	}
	landed, err := trashItem(src)
	if err != nil {
		t.Fatal(err)
	}
	if landed != "" {
		defer os.Remove(landed)
	}
	if _, err := os.Lstat(src); !os.IsNotExist(err) {
		t.Fatalf("still in place after the Trash: %v", err)
	}
	if !strings.Contains(landed, "Trash") {
		t.Fatalf("landed in %q, not in a Trash folder", landed)
	}
	if b, err := os.ReadFile(landed); err != nil || string(b) != "synthetic" {
		t.Fatalf("trashed copy: %q %v", b, err)
	}
	if _, err := trashItem(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("a missing file went to the Trash without an error")
	}
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package purge

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mauriziofonte/onefold/internal/testfix"
)

func readInfo(t *testing.T, trash, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(trash, "info", name+".trashinfo"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if !strings.HasPrefix(s, "[Trash Info]\nPath=") || !strings.Contains(s, "\nDeletionDate=") {
		t.Fatalf("info file: %q", s)
	}
	return s
}

// The home Trash: the file moves whole into files/, its info file holds the
// escaped absolute path, and a second file of the same name gets a new one.
func TestTrashItem(t *testing.T) {
	root := testfix.TempDir(t)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	trash := filepath.Join(root, "data", "Trash")
	var landed []string
	for i := 0; i < 2; i++ {
		dir := filepath.Join(root, "in", strconv.Itoa(i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		src := filepath.Join(dir, "a b%é.txt")
		if err := os.WriteFile(src, []byte("synthetic"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := trashItem(src)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Lstat(src); !os.IsNotExist(err) {
			t.Fatalf("still in place: %v", err)
		}
		if b, err := os.ReadFile(got); err != nil || string(b) != "synthetic" {
			t.Fatalf("trashed copy %s: %q %v", got, b, err)
		}
		landed = append(landed, filepath.Base(got))
		info := readInfo(t, trash, filepath.Base(got))
		if want := "Path=" + strings.ReplaceAll(filepath.ToSlash(dir), " ", "%20") + "/a%20b%25%C3%A9.txt\n"; !strings.Contains(info, want) {
			t.Fatalf("info %q lacks %q", info, want)
		}
	}
	if landed[0] != "a b%é.txt" || landed[1] != "a b%é.2.txt" {
		t.Fatalf("names in the Trash: %v", landed)
	}
	if _, err := trashItem(filepath.Join(root, "missing")); err == nil {
		t.Fatal("a missing file went to the Trash without an error")
	}
	if _, err := os.Stat(filepath.Join(trash, "info", "missing.trashinfo")); !os.IsNotExist(err) {
		t.Fatal("info file left for a missing file")
	}
}

// A volume Trash: $topdir/.Trash/$uid when .Trash is a sticky folder,
// else $topdir/.Trash-$uid; paths in its info files are relative to topdir.
// A Trash that is a symbolic link is refused and the file kept.
func TestTrashTopDir(t *testing.T) {
	uid := strconv.Itoa(os.Getuid())
	top := testfix.TempDir(t)
	src := filepath.Join(top, "sub", "f.txt")
	put := func() {
		if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	put()
	tr, err := topTrash(top)
	if err != nil || tr != filepath.Join(top, ".Trash-"+uid) {
		t.Fatalf("no shared .Trash: %s %v", tr, err)
	}
	if _, err := trashInto(src, tr, top); err != nil {
		t.Fatal(err)
	}
	if info := readInfo(t, tr, "f.txt"); !strings.Contains(info, "Path=sub/f.txt\n") {
		t.Fatalf("relative path: %q", info)
	}

	shared := filepath.Join(top, ".Trash")
	if err := os.Mkdir(shared, 0o777); err != nil {
		t.Fatal(err)
	}
	if tr, _ := topTrash(top); tr != filepath.Join(top, ".Trash-"+uid) {
		t.Fatalf("a .Trash without the sticky bit was used: %s", tr)
	}
	if err := os.Chmod(shared, 0o777|os.ModeSticky); err != nil {
		t.Fatal(err)
	}
	if tr, err := topTrash(top); err != nil || tr != filepath.Join(shared, uid) {
		t.Fatalf("sticky .Trash: %s %v", tr, err)
	}

	other := testfix.TempDir(t)
	top2 := testfix.TempDir(t)
	if err := os.Symlink(other, filepath.Join(top2, ".Trash-"+uid)); err != nil {
		t.Fatal(err)
	}
	if _, err := topTrash(top2); err == nil {
		t.Fatal("a Trash that is a symbolic link was used")
	}
}

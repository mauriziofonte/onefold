// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package hashcache

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mauriziofonte/onefold/internal/sparsehash"
)

func key(t *testing.T, p string) string {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	k := KeyOf(p, fi)
	if k == "" {
		t.Fatalf("no key for %s", p)
	}
	return k
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cp := filepath.Join(dir, "state", FileName)
	f := filepath.Join(dir, "a.bin")
	if err := os.WriteFile(f, []byte("first version"), 0o644); err != nil {
		t.Fatal(err)
	}
	k := key(t, f)
	c, err := OpenAt(cp)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok := c.Sparse(k, 0, 0); ok {
		t.Fatal("hit in an empty cache")
	}
	c.PutSparse(k, 0, 0, sparsehash.Result{Hex: "s1"})
	c.PutFull(k, "f1")
	c.PutSparse("", 0, 0, sparsehash.Result{Hex: "ignored"})
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(cp); fi.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Errorf("cache file mode %o", fi.Mode().Perm())
	}
	c, err = OpenAt(cp)
	if err != nil {
		t.Fatal(err)
	}
	if h, full, ok := c.Sparse(k, 0, 0); !ok || h != "s1" || full {
		t.Fatalf("sparse after reopen: %q %v %v", h, full, ok)
	}
	if _, _, ok := c.Sparse(k, 3, 4096); ok {
		t.Fatal("hit with other sampling parameters")
	}
	if h, ok := c.Full(k); !ok || h != "f1" || c.Hits() != 2 {
		t.Fatalf("full after reopen: %q %v, hits %d", h, ok, c.Hits())
	}
	c.Close()

	// a changed file is another version: another key
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(f, []byte("second version"), 0o644); err != nil {
		t.Fatal(err)
	}
	if key(t, f) == k {
		t.Fatal("a rewritten file kept its key")
	}
	// other bytes of the same size, the old modification time put back (a
	// sync tool, touch -r): the status-change time (Unix) still tells it apart
	old, _ := os.Stat(f)
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(f, []byte("second VERSION"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(f, old.ModTime(), old.ModTime()); err != nil {
		t.Fatal(err)
	}
	if os.PathSeparator == '/' && key(t, f) == KeyOf(f, old) {
		t.Fatal("rewritten bytes with the old mtime kept the key")
	}
}

func TestNilAndBrokenLines(t *testing.T) {
	var c *Cache
	c.PutSparse("k", 0, 0, sparsehash.Result{Hex: "x"})
	if _, _, ok := c.Sparse("k", 0, 0); ok || c.Close() != nil || c.Hits() != 0 {
		t.Fatal("a nil cache knows something")
	}
	dir := t.TempDir()
	cp := filepath.Join(dir, FileName)
	body := header + "\nk1\t5/65536/" + strconv.Itoa(sparsehash.MaxBlocks) + "\ts1\t\nbroken line\n\t\t\t\nk2\t\t\tf2\n"
	if err := os.WriteFile(cp, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := OpenAt(cp)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if h, _, ok := c.Sparse("k1", 5, 65536); !ok || h != "s1" {
		t.Fatalf("k1: %q %v", h, ok)
	}
	if h, ok := c.Full("k2"); !ok || h != "f2" {
		t.Fatalf("k2: %q %v", h, ok)
	}
	if len(c.m) != 2 {
		t.Fatalf("%d entries, want 2", len(c.m))
	}
}

func TestOtherFormatStartsOver(t *testing.T) {
	cp := filepath.Join(t.TempDir(), FileName)
	if err := os.WriteFile(cp, []byte("something else\nk\tp\ts\tf\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := OpenAt(cp)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.m) != 0 {
		t.Fatal("entries read from another format")
	}
	c.PutFull("k", "f")
	c.Close()
	b, _ := os.ReadFile(cp)
	if !strings.HasPrefix(string(b), header+"\n") || strings.Contains(string(b), "something else") {
		t.Fatalf("cache file:\n%s", b)
	}
}

func TestCompaction(t *testing.T) {
	defer func(n int) { MaxEntries = n }(MaxEntries)
	MaxEntries = 3
	cp := filepath.Join(t.TempDir(), FileName)
	c, err := OpenAt(cp)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"a", "b", "c", "d", "e"} {
		c.PutFull(k, "h"+k)
	}
	c.Full("a") // used last: kept
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	c, err = OpenAt(cp)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if len(c.m) != 3 {
		t.Fatalf("%d entries after compaction, want 3", len(c.m))
	}
	for _, k := range []string{"a", "d", "e"} {
		if _, ok := c.Full(k); !ok {
			t.Errorf("%s dropped", k)
		}
	}
}

func TestOpenTurnedOff(t *testing.T) {
	t.Setenv("ONEFOLD_STATE_DIR", t.TempDir())
	t.Setenv("ONEFOLD_NO_HASH_CACHE", "1")
	if Open() != nil {
		t.Fatal("ONEFOLD_NO_HASH_CACHE=1 still opened a cache")
	}
	t.Setenv("ONEFOLD_NO_HASH_CACHE", "")
	c := Open()
	if c == nil {
		t.Fatal("no cache in a writable state dir")
	}
	c.Close()
}

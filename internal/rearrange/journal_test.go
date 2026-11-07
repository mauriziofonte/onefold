// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package rearrange

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const journalHead = `{"op":"header","root":"/r","naming":"ext-year","coverage":1,"sample":1,"blocks":4}` + "\n"

// A journal is read whole; malformed lines are skipped as they always were.
func TestReadJournal(t *testing.T) {
	j := journalHead +
		`{"op":"delete","id":1,"src":"a/dup.jpg","keeper":"a/keep.jpg","content":"c"}` + "\n" +
		"not json\n" +
		`{"op":"move","id":2,"src":"x.pdf","tmp":"pdf/2024/x.pdf.2.onefold-mv","dst":"pdf/2024/x.pdf","size":3,"hash":"h"}` + "\n"
	hdr, moves, dels, err := readJournal(strings.NewReader(j))
	if err != nil {
		t.Fatal(err)
	}
	if hdr.Root != "/r" || hdr.Blocks != 4 || len(moves) != 1 || len(dels) != 1 {
		t.Fatalf("hdr %+v, moves %+v, dels %+v", hdr, moves, dels)
	}
	if moves[0].Dst != "pdf/2024/x.pdf" || dels[0].Keeper != "a/keep.jpg" {
		t.Fatalf("moves %+v, dels %+v", moves, dels)
	}
	if _, _, _, err := readJournal(strings.NewReader(`{"op":"move","src":"a","tmp":"b","dst":"c"}`)); err == nil {
		t.Fatal("a journal without header was read")
	}
}

// A resume acts on every path it reads: a journal whose paths leave the root
// is refused before anything is touched.
func TestJournalPathsStayInRoot(t *testing.T) {
	bad := []string{"../x", "a/../../x", "/etc/passwd", ".", "", "a/.."}
	if filepath.Separator == '\\' {
		bad = append(bad, `C:\x`, `C:x`, `\\host\share\x`)
	}
	for _, p := range bad {
		for _, line := range []string{
			`{"op":"move","id":1,"src":"a","tmp":"t.1.onefold-mv","dst":` + quote(p) + `}`,
			`{"op":"move","id":1,"src":` + quote(p) + `,"tmp":"t.1.onefold-mv","dst":"d"}`,
			`{"op":"move","id":1,"src":"a","tmp":` + quote(p) + `,"dst":"d"}`,
			`{"op":"delete","id":1,"src":` + quote(p) + `,"keeper":"k"}`,
			`{"op":"delete","id":1,"src":"s","keeper":` + quote(p) + `}`,
		} {
			if _, _, _, err := readJournal(strings.NewReader(journalHead + line + "\n")); err == nil {
				t.Errorf("accepted %s", line)
			}
		}
	}
	// End to end: the file outside the root stays where it is.
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	outside := filepath.Join(dir, "outside.txt")
	mustWrite(t, outside, "keep me")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	jp := filepath.Join(dir, "j.jsonl")
	j := `{"op":"header","root":` + quote(root) + `}` + "\n" +
		`{"op":"move","id":1,"src":"../outside.txt","tmp":"o.1.onefold-mv","dst":"o.txt","size":7,"hash":"x"}` + "\n"
	mustWrite(t, jp, j)
	if _, err := RecoverWith(jp, false, RecoverOptions{}); err == nil {
		t.Fatal("the journal was accepted")
	}
	if b, err := os.ReadFile(outside); err != nil || string(b) != "keep me" {
		t.Fatalf("outside file: %q, %v", b, err)
	}
}

func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func mustWrite(t *testing.T, p, s string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The journal reader never panics and never returns a path outside the root.
func FuzzReadJournal(f *testing.F) {
	f.Add([]byte(journalHead + `{"op":"move","id":2,"src":"x.pdf","tmp":"y.2.onefold-mv","dst":"y.pdf","size":3,"hash":"h"}` + "\n"))
	f.Add([]byte(journalHead + `{"op":"delete","id":1,"src":"a/dup.jpg","keeper":"a/keep.jpg"}` + "\n"))
	f.Add([]byte(journalHead + `{"op":"move","src":"../a","tmp":"b","dst":"c"}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		hdr, moves, dels, err := readJournal(strings.NewReader(string(b)))
		if err != nil {
			return
		}
		if hdr.Root == "" {
			t.Fatal("no root")
		}
		for _, m := range moves {
			for _, p := range []string{m.Src, m.Tmp, m.Dst} {
				if !filepath.IsLocal(filepath.FromSlash(p)) {
					t.Fatalf("move path %q", p)
				}
			}
		}
		for _, d := range dels {
			if !filepath.IsLocal(filepath.FromSlash(d.Src)) || !filepath.IsLocal(filepath.FromSlash(d.Keeper)) {
				t.Fatalf("delete paths %q %q", d.Src, d.Keeper)
			}
		}
	})
}

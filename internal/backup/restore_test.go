// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package backup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mauriziofonte/onefold/internal/fsx"
	"github.com/mauriziofonte/onefold/internal/sparsehash"
)

func repairs(h *Health) string {
	var out []string
	for _, r := range h.Repairs {
		out = append(out, r.To+":"+r.Why+":"+r.Rel)
	}
	return strings.Join(out, ",")
}

// The tree is checked against the hashes the backup recorded, and each side
// is repaired from the other: bit rot and a missing file in the tree from
// the copy, bit rot in the copy from the tree; a file damaged on both sides
// is lost, a renamed one is not restored, a new one is "not backed up". The
// dry run changes nothing; the execute needs the count, keeps every damaged
// file aside and leaves both sides clean.
func TestCompareAndRestore(t *testing.T) {
	d := t.TempDir()
	tree, cold := filepath.Join(d, "golden"), filepath.Join(d, "cold")
	files := map[string]string{
		"PICTURES/2020/a.jpg": "photo a", "PICTURES/2020/b.jpg": "photo b", "FILES/c.pdf": "doc c",
		"FILES/d.pdf": "doc d", "VIDEOS/e.mp4": "video e", "FILES/ok.txt": "fine",
	}
	for rel, body := range files {
		put(t, filepath.Join(tree, filepath.FromSlash(rel)), body)
	}
	if _, err := Run(tree, cold, Options{Execute: true}); err != nil {
		t.Fatal(err)
	}
	at := func(root, rel string) string { return filepath.Join(root, filepath.FromSlash(rel)) }
	put(t, at(tree, "PICTURES/2020/a.jpg"), "photo A")             // bit rot in the tree
	os.Remove(at(tree, "PICTURES/2020/b.jpg"))                     // lost from the tree
	put(t, at(cold, "FILES/c.pdf"), "doc C")                       // bit rot in the copy
	put(t, at(tree, "FILES/d.pdf"), "doc D")                       // damaged on both sides
	put(t, at(cold, "FILES/d.pdf"), "dOc d")                       //
	os.Rename(at(tree, "VIDEOS/e.mp4"), at(tree, "VIDEOS/e2.mp4")) // renamed after the backup
	put(t, at(tree, "PICTURES/2021/new.jpg"), "not backed up yet") //

	h, err := Compare(tree, cold, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := repairs(h); got != "copy:changed:FILES/c.pdf,tree:changed:PICTURES/2020/a.jpg,tree:missing:PICTURES/2020/b.jpg" {
		t.Fatalf("repairs %s", got)
	}
	if strings.Join(h.Lost, ",") != "FILES/d.pdf" || h.Moved["VIDEOS/e.mp4"] != "VIDEOS/e2.mp4" || strings.Join(h.Tree.Untracked, ",") != "PICTURES/2021/new.jpg" {
		t.Fatalf("lost %v moved %v untracked %v", h.Lost, h.Moved, h.Tree.Untracked)
	}

	// dry run, then a wrong count: nothing changes
	if r, err := Restore(tree, cold, RestoreOptions{}); err != nil || r.Done != 0 || read(t, at(tree, "PICTURES/2020/a.jpg")) != "photo A" {
		t.Fatalf("dry run: %+v %v", r, err)
	}
	jd := t.TempDir()
	if _, err := Restore(tree, cold, RestoreOptions{Execute: true, Expect: 2, JournalDir: jd}); err == nil {
		t.Fatal("a wrong count accepted")
	}
	if ents, _ := os.ReadDir(jd); len(ents) != 0 || read(t, at(cold, "FILES/c.pdf")) != "doc C" {
		t.Fatal("a refused restore wrote something")
	}
	r, err := Restore(tree, cold, RestoreOptions{Execute: true, Expect: 3, JournalDir: jd})
	if err != nil || r.Done != 3 || len(r.Failed) != 0 || r.Journal == "" {
		t.Fatalf("restore: %+v %v", r, err)
	}
	for _, c := range []struct{ root, rel, want string }{
		{tree, "PICTURES/2020/a.jpg", "photo a"}, {tree, "PICTURES/2020/b.jpg", "photo b"}, {cold, "FILES/c.pdf", "doc c"},
		{tree, "FILES/d.pdf", "doc D"}, {cold, "FILES/d.pdf", "dOc d"},
	} {
		if got := read(t, at(c.root, c.rel)); got != c.want {
			t.Errorf("%s %s = %q, want %q", c.root, c.rel, got, c.want)
		}
	}
	// the damaged bytes are kept, never deleted
	var asides []string
	for _, rp := range r.Repairs {
		if rp.Aside != "" {
			asides = append(asides, read(t, rp.Aside))
			if !strings.Contains(filepath.ToSlash(rp.Aside), "/.onefold/damaged/") {
				t.Errorf("aside %s", rp.Aside)
			}
		}
	}
	sort.Strings(asides)
	if strings.Join(asides, ",") != "doc C,photo A" {
		t.Fatalf("kept aside %v", asides)
	}
	h, err = Compare(tree, cold, 2, nil)
	if err != nil || len(h.Repairs) != 0 || strings.Join(h.Lost, ",") != "FILES/d.pdf" {
		t.Fatalf("after: %s lost %v %v", repairs(h), h.Lost, err)
	}
	// a check of the copy alone does not see the kept files as untracked
	if a, err := Check(cold, 1, nil); err != nil || len(a.Untracked) != 0 {
		t.Fatalf("check after restore: %+v %v", a, err)
	}
	if _, err := Compare(tree, filepath.Join(tree, "x"), 1, nil); err == nil {
		t.Error("a copy inside the tree accepted")
	}
}

// A restore interrupted between its steps is finished or undone from its
// journal: the name never stays without a whole file.
func TestResumeRestore(t *testing.T) {
	d := t.TempDir()
	content := func(p string) string {
		h, err := sparsehash.FullFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	put(t, filepath.Join(d, "good"), "the recorded bytes")
	rec := content(filepath.Join(d, "good"))
	journal := func(es ...rentry) string {
		p := filepath.Join(d, "j"+string(rune('a'+len(es)))+".jsonl")
		f, _ := os.Create(p)
		for _, e := range es {
			b, _ := json.Marshal(e)
			f.Write(append(b, '\n'))
		}
		f.Close()
		return p
	}
	// 1. stopped after the damaged file stepped aside: the staged copy takes the name
	n1, a1 := filepath.Join(d, "one"), filepath.Join(d, "aside1")
	put(t, fsx.TempFor(n1, false), "the recorded bytes")
	put(t, a1, "damaged")
	// 2. the same, without a staged copy: the damaged file is put back
	n2, a2 := filepath.Join(d, "two"), filepath.Join(d, "aside2")
	put(t, a2, "damaged two")
	// 3. stopped before the step aside: reported, the temp removed
	n3 := filepath.Join(d, "three")
	put(t, n3, "still damaged")
	put(t, fsx.TempFor(n3, false), "the recorded bytes")
	// 4. finished, its done line lost: counted as finished
	n4 := filepath.Join(d, "four")
	put(t, n4, "the recorded bytes")
	// 5. a done one is not touched
	n5 := filepath.Join(d, "five")
	j := journal(
		rentry{Op: "replace", Path: n1, Tmp: fsx.TempFor(n1, false), Aside: a1, Content: rec},
		rentry{Op: "replace", Path: n2, Tmp: fsx.TempFor(n2, false), Aside: a2, Content: rec},
		rentry{Op: "replace", Path: n3, Tmp: fsx.TempFor(n3, false), Aside: filepath.Join(d, "aside3"), Content: rec},
		rentry{Op: "add", Path: n4, Tmp: fsx.TempFor(n4, false), Content: rec},
		rentry{Op: "add", Path: n5, Tmp: fsx.TempFor(n5, false), Content: rec},
		rentry{Op: "done", Path: n5},
	)
	fin, back, probs, err := ResumeRestore(j)
	if err != nil || fin != 2 || back != 1 || len(probs) != 1 || !strings.Contains(probs[0], "three: not replaced") {
		t.Fatalf("resume: finished %d put back %d problems %v %v", fin, back, probs, err)
	}
	if read(t, n1) != "the recorded bytes" || read(t, a1) != "damaged" || read(t, n2) != "damaged two" || read(t, n3) != "still damaged" {
		t.Fatal("resume left the wrong files")
	}
	for _, p := range []string{fsx.TempFor(n1, false), fsx.TempFor(n3, false), a2} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("%s left behind", p)
		}
	}
	if _, err := os.Lstat(n5); !os.IsNotExist(err) {
		t.Error("a finished entry was replayed")
	}
}

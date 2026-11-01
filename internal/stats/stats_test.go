// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package stats

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mauriziofonte/onefold/internal/kinds"
	"github.com/mauriziofonte/onefold/internal/testfix"
)

func put(t *testing.T, root, rel, body string, mt time.Time) string {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}
	return p
}

func bucket(bs []Bucket, key string) Bucket {
	for _, b := range bs {
		if b.Key == key {
			return b
		}
	}
	return Bucket{}
}

// A synthetic tree: two folders with the same photos, a document, an unknown
// extension, a same-size file with other content, an empty file and junk.
func tree(t *testing.T) (root string, photo string) {
	root = testfix.TempDir(t)
	y20, y22 := time.Date(2020, 5, 1, 0, 0, 0, 0, time.UTC), time.Date(2022, 7, 1, 0, 0, 0, 0, time.UTC)
	big := strings.Repeat("P", 300<<10) // 300 KiB
	photo = put(t, root, "Pictures/2020/beach.jpg", big, y20)
	put(t, root, "Backup/old/beach copy.jpg", big, y22)
	put(t, root, "Backup/old/beach (2).jpg", big, y22)
	put(t, root, "Pictures/2020/other.jpg", strings.Repeat("Q", 300<<10), y20) // same size, other content
	put(t, root, "Docs/report.pdf", "%PDF-1.4 synthetic", y22)
	put(t, root, "Docs/notes.zzqq", "unknown type", y22)
	put(t, root, "Docs/empty.txt", "", y22)
	put(t, root, "Docs/.DS_Store", "junk", y22)
	return root, photo
}

func TestRun(t *testing.T) {
	root, _ := tree(t)
	var snaps int
	var mu sync.Mutex
	r, err := Run(Options{Roots: []string{root}, Workers: 4, OneVolume: true, Dups: DupsFull,
		OnSnapshot: func(*Result) { mu.Lock(); snaps++; mu.Unlock() }, SnapshotEvery: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if r.Files != 8 || r.Empty != 1 || r.Junk.Files != 1 {
		t.Fatalf("files %d empty %d junk %d", r.Files, r.Empty, r.Junk.Files)
	}
	wantBytes := int64(4*(300<<10) + len("%PDF-1.4 synthetic") + len("unknown type") + len("junk"))
	if r.Bytes != wantBytes {
		t.Fatalf("bytes %d, want %d", r.Bytes, wantBytes)
	}
	if b := bucket(r.Families, kinds.FamPhotos); b.Files != 4 || b.Bytes != 4*(300<<10) {
		t.Fatalf("photos: %+v", b)
	}
	if b := bucket(r.Exts, "jpg"); b.Files != 4 || b.Family != kinds.FamPhotos {
		t.Fatalf("jpg: %+v", b)
	}
	if b := bucket(r.Unknown, "zzqq"); b.Files != 1 {
		t.Fatalf("unknown: %+v", r.Unknown)
	}
	years := map[int]int64{}
	for _, y := range r.Years {
		years[y.Year] = y.Files
	}
	if years[2020] != 2 || years[2022] != 6 {
		t.Fatalf("years: %v", years)
	}
	if len(r.Largest) == 0 || r.Largest[0].Size != 300<<10 {
		t.Fatalf("largest: %+v", r.Largest)
	}
	if len(r.Tree) != 1 || r.Tree[0].Files != 8 {
		t.Fatalf("tree: %+v", r.Tree)
	}
	var backup *Node
	for _, c := range r.Tree[0].Children {
		if c.Name == "Backup" {
			backup = c
		}
	}
	if backup == nil || backup.Bytes != 2*(300<<10) || backup.Families[kinds.FamPhotos] != 2*(300<<10) {
		t.Fatalf("Backup node: %+v", backup)
	}
	d := r.Dups
	if d == nil || d.Groups != 1 || d.Files != 3 || d.Extra != 2 || d.Wasted != 2*(300<<10) || d.Candidates != 4 {
		t.Fatalf("dups: %+v", d)
	}
	if g := d.Top[0]; !g.Verified || g.Count != 3 || len(g.Paths) != 3 {
		t.Fatalf("group: %+v", g)
	}
	var cross, inside bool
	for _, p := range d.Pairs {
		if p.A != p.B && strings.HasSuffix(p.A, filepath.Join("Backup", "old")) != strings.HasSuffix(p.B, filepath.Join("Backup", "old")) {
			cross = true
		}
		if p.A == p.B && strings.HasSuffix(p.A, filepath.Join("Backup", "old")) && p.Files == 1 {
			inside = true
		}
	}
	if !cross || !inside {
		t.Fatalf("pairs: %+v", d.Pairs)
	}
	if r.Phase != "done" || r.Partial {
		t.Fatalf("phase %q partial %v", r.Phase, r.Partial)
	}
}

// fast mode groups on the sample digest; off skips the pass.
func TestDupModes(t *testing.T) {
	root, _ := tree(t)
	r, err := Run(Options{Roots: []string{root}, Dups: DupsFast})
	if err != nil || r.Dups == nil || r.Dups.Groups != 1 || r.Dups.Top[0].Verified {
		t.Fatalf("fast: %+v %v", r.Dups, err)
	}
	r, err = Run(Options{Roots: []string{root}, Dups: DupsOff})
	if err != nil || r.Dups != nil {
		t.Fatalf("off: %+v %v", r.Dups, err)
	}
	if _, err := Run(Options{Roots: []string{root}, Dups: "maybe"}); err == nil {
		t.Fatal("an unknown mode must fail")
	}
}

// Two roots, one inside the other, are measured once; a hard link is one
// file, not a duplicate.
func TestRootsAndHardLinks(t *testing.T) {
	root, photo := tree(t)
	r, err := Run(Options{Roots: []string{root, filepath.Join(root, "Docs")}, Dups: DupsFull})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Roots) != 1 || r.Files != 8 {
		t.Fatalf("roots %v files %d", r.Roots, r.Files)
	}
	if runtime.GOOS == "windows" {
		return // a directory listing carries no link count (documented)
	}
	if err := os.Link(photo, filepath.Join(root, "Pictures", "linked.jpg")); err != nil {
		t.Skip(err)
	}
	r, err = Run(Options{Roots: []string{root}, Dups: DupsFull})
	if err != nil {
		t.Fatal(err)
	}
	if r.Files != 8 || r.HardLinks.Files != 1 || r.Dups.Files != 3 {
		t.Fatalf("files %d links %+v dup files %d", r.Files, r.HardLinks, r.Dups.Files)
	}
	if r.Allocated <= 0 {
		t.Fatalf("allocated %d", r.Allocated)
	}
}

// The folder tree keeps Depth levels; deeper folders count in their ancestor.
func TestDepth(t *testing.T) {
	root := testfix.TempDir(t)
	put(t, root, "a/b/c/d/e.txt", "x", time.Now())
	r, err := Run(Options{Roots: []string{root}, Depth: 2, Dups: DupsOff})
	if err != nil {
		t.Fatal(err)
	}
	a := r.Tree[0].Children[0]
	b := a.Children[0]
	if a.Name != "a" || b.Name != "b" || len(b.Children) != 0 || !b.Deeper || b.Files != 1 {
		t.Fatalf("tree: %+v / %+v", a, b)
	}
}

func TestMounts(t *testing.T) {
	ms := Mounts()
	if len(ms) == 0 {
		if runtime.GOOS == "linux" || runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
			t.Fatal("no volume listed")
		}
		t.Skip("no volume listing on this OS")
	}
	for _, m := range ms {
		if m.Path == "" || m.Total <= 0 || m.Free < 0 || m.Free > m.Total {
			t.Errorf("mount %+v", m)
		}
	}
	v := VolumeOf(testfix.TempDir(t))
	if v.Mount == "" || v.Total <= 0 {
		t.Fatalf("volume of a temp dir: %+v", v)
	}
}

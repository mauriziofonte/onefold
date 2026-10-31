// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package golden

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/kinds"
	"github.com/mauriziofonte/onefold/internal/meta"
	"github.com/mauriziofonte/onefold/internal/plan"
)

// synth catalogs one room of testdata/media-synth (pure-Go extractors, UTC,
// fixed file times) and returns its records with paths relative to the corpus.
func synth(t *testing.T, room string) []catalog.Record {
	t.Helper()
	t.Setenv("ONEFOLD_NO_EXTERNAL", "1")
	prev := meta.Zone()
	meta.SetZone(time.UTC)
	t.Cleanup(func() { meta.SetZone(prev) })
	root := t.TempDir()
	mtime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	src := filepath.Join("..", "..", "testdata", "media-synth", room)
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		dst := filepath.Join(root, room, d.Name())
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return err
		}
		return os.Chtimes(dst, mtime, mtime)
	})
	if err != nil {
		t.Fatal(err)
	}
	exts, _, _ := kinds.PresetExts([]string{"media"})
	recs, _, err := catalog.Collect([]string{filepath.Join(root, room)}, catalog.Options{Workers: 2, Exts: exts, Host: "synth"})
	if err != nil {
		t.Fatal(err)
	}
	for i := range recs {
		rel, _ := filepath.Rel(root, recs[i].Path)
		recs[i].Path, recs[i].Room = filepath.ToSlash(rel), room
	}
	return recs
}

// goldenOf plans recs as a first golden run and indexes the plan.
func goldenOf(t *testing.T, recs []catalog.Record, o plan.Options) *Index {
	t.Helper()
	acts := plan.BuildActionsWith(plan.PlanWith(recs, o), o)
	es, st := Build([][]plan.Action{acts}, recs)
	if st.Entries != len(acts) {
		t.Fatalf("index stats %+v for %d actions", st, len(acts))
	}
	// through the file: the index a later run reads
	var buf bytes.Buffer
	if err := Write(&buf, es); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "golden-index.jsonl")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	ix, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return ix
}

func lines(m *MatchResult) []string {
	var out []string
	for _, k := range m.Dupes {
		out = append(out, "dupe "+k.SrcPath+" = "+k.Golden+" ("+k.Reason+")")
	}
	for _, k := range m.Upgrades {
		out = append(out, "upgrade "+k.SrcPath+" > "+k.Golden)
	}
	for _, a := range m.New {
		out = append(out, "new "+a.SrcPath+" -> "+a.CanonRel)
	}
	sort.Strings(out)
	return out
}

func check(t *testing.T, got []string, want []string) {
	t.Helper()
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// The golden tree holds room A; room B arrives later: what A already holds
// (same bytes, or the same shot without more pixels) is a duplicate, the
// rest is a sub-plan whose names never collide with the tree.
func TestMatchAgainstGolden(t *testing.T) {
	o := plan.Options{SelfContained: true}
	ix := goldenOf(t, synth(t, "A"), o)
	m := Match(ix, synth(t, "B"), o)
	check(t, lines(m), []string{
		"dupe B/IMG_0001 (1).jpg = PICTURES/JPG/2021/IMG_20210506_070809.jpg (same bytes)",
		"dupe B/IMG_0010_small.jpg = PICTURES/JPG/2021/IMG_20210607_101112.jpg (same shot, not more pixels)",
		"dupe B/IMG_0011.jpg = PICTURES/JPG/2021/IMG_20210607_101113.jpg (same shot, not more pixels)",
		"dupe B/IMG_0012.jpg = PICTURES/JPG/2021/IMG_20210607_101114.jpg (same shot, not more pixels)",
		"dupe B/VID_0001_lq.mp4 = VIDEOS/2023/VID_20230708_091009.mp4 (same shot, not more pixels)",
		// another device in the same sub-second: new
		"new B/DSC_0010.jpg -> PICTURES/JPG/2021/DSC_20210607_101112.jpg",
		// no sub-second, other bytes: new
		"new B/IMG_0001_small.jpg -> PICTURES/JPG/2021/IMG_20210506_070809_01.jpg",
		"new B/IMG_20190102_030405.jpg -> PICTURES/JPG/2019/IMG_20190102_030405.jpg",
		// same second as the golden IMG_0001 and as IMG_0001_small: next free suffix
		"new B/IMG_9001.jpg -> PICTURES/JPG/2021/IMG_20210506_070809_02.jpg",
		"new B/VID_0002.mp4 -> VIDEOS/2023/VID_20230708_091005.mp4",
		// the bytes of the golden logo.png under another format: new (one asset per format)
		"new B/logo.jpg -> PICTURES/JPG/2020/IMG_20200101_000000.jpg",
		"new B/undated.png -> PICTURES/PNG/2020/IMG_20200101_000000_01.png",
	})
	// the two new IMG_20210506_070809 got the bare name and _01 among
	// themselves; the bare one is taken in the tree -> _02; undated.png -> _01
	if m.Renamed != 2 {
		t.Errorf("renamed %d, want 2", m.Renamed)
	}
}

// The other way round: a sharper copy of a golden shot is an upgrade,
// listed and never put in the sub-plan.
func TestMatchUpgrade(t *testing.T) {
	o := plan.Options{SelfContained: true}
	ix := goldenOf(t, synth(t, "B"), o)
	m := Match(ix, synth(t, "A"), o)
	var up []string
	for _, k := range m.Upgrades {
		up = append(up, k.SrcPath+" > "+k.Golden)
	}
	check(t, up, []string{
		"A/IMG_0010.jpg > PICTURES/JPG/2021/IMG_20210607_101112.jpg",
		"A/VID_0001.mp4 > VIDEOS/2023/VID_20230708_091009.mp4",
	})
	for _, a := range m.New {
		if a.SrcPath == "A/IMG_0010.jpg" || a.SrcPath == "A/VID_0001.mp4" {
			t.Errorf("upgrade in the sub-plan: %s", a.SrcPath)
		}
	}
}

// A plan written before it carried the winner's metadata: Build takes it
// from the manifests (and the drops' hashes too), so the match is the same.
func TestBuildJoinsManifests(t *testing.T) {
	recsA := synth(t, "A")
	acts := plan.BuildActions(plan.Plan(recsA)) // historical plan: no metadata fields
	es, st := Build([][]plan.Action{acts}, recsA)
	if st.Joined == 0 || st.Unjoined != 0 {
		t.Fatalf("stats %+v", st)
	}
	o := plan.Options{SelfContained: true}
	check(t, lines(Match(New(es), synth(t, "B"), o)), lines(Match(goldenOf(t, recsA, o), synth(t, "B"), o)))
	// without the manifests the shots are unknown: only bytes match
	es, st = Build([][]plan.Action{acts}, nil)
	if st.Unjoined == 0 {
		t.Fatalf("stats without manifests %+v", st)
	}
	for _, k := range Match(New(es), synth(t, "B"), o).Dupes {
		if k.Reason != SameBytes {
			t.Errorf("shot match without metadata: %+v", k)
		}
	}
}

// An index refuses another zone and malformed lines.
func TestLoadStrict(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "i.jsonl")
	os.WriteFile(p, []byte(`{"onefold":"golden-index","zone":"Mars/Base"}`+"\n"), 0o644)
	if _, err := Load(p); err == nil {
		t.Error("other zone accepted")
	}
	var buf bytes.Buffer
	Write(&buf, []Entry{{ID: "a", Kind: "photo", CanonRel: "x"}})
	buf.WriteString("{broken\n")
	os.WriteFile(p, buf.Bytes(), 0o644)
	if _, err := Load(p); err == nil {
		t.Error("malformed line accepted")
	}
}

// Bytes: a photo matches only under its own format; a generic file with the
// bytes of a golden photo is a duplicate (the planner absorbs it too).
func TestByBytes(t *testing.T) {
	ix := New([]Entry{{ID: "h1", Kind: "photo", CanonRel: "PICTURES/JPG/2020/a.jpg", SrcPath: "/s/a.jpg"},
		{ID: "h2", Kind: "document", CanonRel: "FILES/PDF/0000/b.pdf", SrcPath: "/s/b.pdf"}})
	for _, c := range []struct {
		kind, hash, path string
		want             bool
	}{
		{"photo", "h1", "/n/x.jpeg", true}, {"photo", "h1", "/n/x.png", false}, {"other", "h1", "/n/x.bak", true},
		{"document", "h2", "/n/y.pdf", true}, {"photo", "h2", "/n/y.jpg", false}, {"other", "h3", "/n/z", false},
	} {
		if _, ok := ix.ByBytes(c.kind, c.hash, c.path); ok != c.want {
			t.Errorf("%+v: found %v", c, ok)
		}
	}
}

// With a video tolerance a shorter or longer re-encode of a golden video is
// the same shot; without it the rounded-duration rule keeps it apart.
func TestMatchVideoTolerance(t *testing.T) {
	ix := New([]Entry{{ID: "hq", Kind: "video", CanonRel: "VIDEOS/2023/VID_20230708_091011.mp4", Time: "2023-07-08 09:10:11",
		DurationSec: 10.4, Width: 1920, Height: 1080, SrcPath: "/g/hq.mp4"}})
	lq := catalog.Record{Path: "/n/lq.mp4", Hash: "lq", Size: 5, Room: "/n", Host: "h",
		Meta: meta.Meta{Kind: "video", Time: "2023-07-08 09:10:11", DurationSec: 10.6, Width: 640, Height: 360, Prefix: "VID"}}
	if m := Match(ix, []catalog.Record{lq}, plan.Options{}); len(m.New) != 1 {
		t.Errorf("without tolerance: %+v", lines(m))
	}
	if m := Match(ix, []catalog.Record{lq}, plan.Options{VideoTolerance: 0.5}); len(m.Dupes) != 1 || m.Dupes[0].Reason != SameShot {
		t.Errorf("with tolerance: %+v", lines(m))
	}
}

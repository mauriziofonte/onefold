// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package plan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/meta"
)

func gen(room, p, hash, kind string, mtime int64) catalog.Record {
	return catalog.Record{Room: room, Path: p, Size: 10, Hash: hash, Host: "h1", MTimeNs: mtime,
		Meta: meta.Meta{Kind: kind, Time: "2020-01-02 03:04:05"}}
}

func onlyAction(t *testing.T, acts []Action, id string) Action {
	t.Helper()
	var hit []Action
	for _, a := range acts {
		if a.ID == id {
			hit = append(hit, a)
		}
	}
	if len(hit) != 1 {
		t.Fatalf("want 1 action for %s, got %d", id, len(hit))
	}
	return hit[0]
}

func TestGenericWinnerOrder(t *testing.T) {
	recs := []catalog.Record{
		gen("/r1", "/r1/a (1).txt", "H1", "text", 5),      // dirty name
		gen("/r1", "/r1/deep/dir/a.txt", "H1", "text", 9), // clean, newer
		gen("/r1", "/r1/b/a.txt", "H1", "text", 9),        // clean, same mtime, shorter path -> WIN
		gen("/r1", "/r1/copy/a - Copy.txt", "H1", "text", 1),
		gen("/r2", "/r2/a.txt", "H1", "text", 1), // room seen later loses even if older
	}
	r := PlanWith(recs, Options{})
	a := onlyAction(t, BuildActionsWith(r, Options{}), "H1")
	if a.SrcPath != "/r1/b/a.txt" || len(a.Drops) != 4 || r.ExactDupes != 4 || a.Artist != "" || a.SrcHost != "h1" {
		t.Fatalf("winner %+v (dupes %d)", a, r.ExactDupes)
	}
	for _, d := range a.Drops {
		if d.Reason != "exact-dupe" {
			t.Fatalf("drop reason %q", d.Reason)
		}
	}
	// oldest mtime wins among clean names in the same room
	r2 := PlanWith([]catalog.Record{gen("/r", "/r/x/new.txt", "H", "text", 9), gen("/r", "/r/y/old.txt", "H", "text", 2)}, Options{})
	if w := onlyAction(t, BuildActions(r2), "H"); w.SrcPath != "/r/y/old.txt" {
		t.Fatalf("mtime tiebreak: %s", w.SrcPath)
	}
}

func TestDistinctContentNeverDropped(t *testing.T) {
	// same name, same size, different hash: both kept (a name+size skip would lose one)
	recs := []catalog.Record{gen("/a", "/a/report.txt", "X", "text", 1), gen("/b", "/b/report.txt", "Y", "text", 1)}
	acts := BuildActions(PlanWith(recs, Options{}))
	if len(acts) != 2 {
		t.Fatalf("want 2 actions, got %d", len(acts))
	}
	rels := []string{acts[0].CanonRel, acts[1].CanonRel}
	joined := strings.Join(rels, " ")
	if !strings.Contains(joined, "FILES/TXT/2020/report.txt") || !strings.Contains(joined, "FILES/TXT/2020/report_01.txt") {
		t.Fatalf("collision naming: %v", rels)
	}
}

func TestCaseInsensitiveCollision(t *testing.T) {
	recs := []catalog.Record{gen("/a", "/a/Readme.TXT", "X", "text", 1), gen("/a", "/a/readme.txt", "Y", "text", 1)}
	acts := BuildActions(PlanWith(recs, Options{}))
	if strings.EqualFold(acts[0].CanonRel, acts[1].CanonRel) {
		t.Fatalf("case-only collision not resolved: %s / %s", acts[0].CanonRel, acts[1].CanonRel)
	}
}

func TestGenericCopyOfMediaAbsorbed(t *testing.T) {
	photo := catalog.Record{Room: "/r", Path: "/r/IMG_1.jpg", Size: 10, Hash: "P", Meta: meta.Meta{Kind: "photo", Time: "2020-01-02 03:04:05", Prefix: "IMG"}}
	bak := gen("/r", "/r/IMG_1.jpg.bak", "P", "other", 1)
	r := PlanWith([]catalog.Record{photo, bak}, Options{})
	acts := BuildActions(r)
	if len(acts) != 1 || acts[0].Kind != "photo" || len(acts[0].Drops) != 1 || acts[0].Drops[0].Path != "/r/IMG_1.jpg.bak" {
		t.Fatalf("absorb: %+v", acts)
	}
}

func TestMislabeledMediaDoesNotAbsorb(t *testing.T) {
	fake := catalog.Record{Room: "/r", Path: "/r/wrong.jpg", Size: 10, Hash: "P", Meta: meta.Meta{Kind: "photo", ExtMismatch: true, SniffExt: "png"}}
	real := gen("/r", "/r/real.png", "P", "image", 1)
	acts := BuildActions(PlanWith([]catalog.Record{fake, real}, Options{}))
	if len(acts) != 2 {
		t.Fatalf("correctly named copy must survive: %+v", acts)
	}
}

func TestLayouts(t *testing.T) {
	doc := gen("/r", "/r/x/Relazione finale.docx", "D", "document", 1)
	doc.Meta.Title, doc.Meta.Author = "Relazione: Q1/2020", "Mario Rossi"
	song := gen("/r", "/r/m/track.MP3", "S", "audio", 1)
	song.Meta.Author, song.Meta.Album, song.Meta.Title, song.Meta.TagYear, song.Meta.Track, song.Meta.Disc = "Band", "Live", "Intro", "2019", 3, 2
	img := gen("/r", "/r/p/shot.PNG", "I", "image", 1)
	img.Meta.CanonDevice, img.Meta.Category = "Nikon D3500", "camera"
	zipf := gen("/r", "/r/z/backup.zip", "Z", "archive", 1)
	undated := gen("/r", "/r/u/notes.txt", "U", "text", 1)
	undated.Meta.Time = ""
	marked := gen("/r", "/r/u/[Desktop] memo (1).txt", "M", "text", 1) // golden names keep the raw stem
	recs := []catalog.Record{doc, song, img, zipf, undated, marked}
	want := map[string]map[string]string{
		"dedupe": {"D": "FILES/DOCX/2020/Relazione finale.docx", "S": "FILES/MP3/2020/track.mp3", "I": "FILES/PNG/2020/shot.png",
			"Z": "FILES/ZIP/2020/backup.zip", "U": "FILES/TXT/0000/notes.txt", "M": "FILES/TXT/2020/[Desktop] memo (1).txt"},
		"timeline": {"D": "TIMELINE/2020/01 - January/02/DOC_030405_Relazione finale.docx", "S": "TIMELINE/2020/01 - January/02/AUD_030405_track.mp3",
			"Z": "TIMELINE/2020/01 - January/02/ARC_030405_backup.zip", "U": "TIMELINE/undated/notes.txt"},
		"document": {"D": "DOCUMENTS/DOC/2020/Mario Rossi/Relazione_ Q1_2020.docx", "Z": "FILES/ZIP/2020/backup.zip", "U": "DOCUMENTS/TXT/0000/Unknown/notes.txt"},
		"music":    {"S": "MUSIC/Band/2019 - Live/203. Intro.mp3", "D": "FILES/DOCX/2020/Relazione finale.docx"},
		"device":   {"I": "DEVICES/Camera/Nikon D3500/2020/shot.png", "S": "FILES/MP3/2020/track.mp3"},
	}
	for layout, exp := range want {
		o := Options{Layout: layout}
		for _, a := range BuildActionsWith(PlanWith(append([]catalog.Record(nil), recs...), o), o) {
			if w, ok := exp[a.ID]; ok && a.CanonRel != w {
				t.Errorf("%s/%s: %s want %s", layout, a.ID, a.CanonRel, w)
			}
		}
	}
	if !ValidLayout("music") || ValidLayout("bogus") || !ValidLayout("") || !ValidLayout("ext-year") || !ValidLayout(`{EXT}/{stem}`) {
		t.Fatal("ValidLayout")
	}
}

func TestExtHarmonizationAndFix(t *testing.T) {
	a := gen("/r", "/r/pic.JPEG.bak", "A", "other", 1)
	b := gen("/r", "/r/scan.TIF", "B", "image", 1)
	c := gen("/r", "/r/really-png.dat", "C", "other", 1)
	c.Meta.SniffExt = "png"
	d := gen("/r", "/r/photo.jpeg", "E", "photo", 1) // media: untouched
	d.Meta.Prefix = "IMG"
	d.Meta.SniffExt = "png"
	o := Options{FixExt: true}
	got := map[string]string{}
	for _, x := range BuildActionsWith(PlanWith([]catalog.Record{a, b, c, d}, o), o) {
		got[x.ID] = x.CanonRel
	}
	for id, w := range map[string]string{"A": "FILES/BAK/2020/pic.JPEG.bak", "B": "FILES/TIFF/2020/scan.tiff", "C": "FILES/PNG/2020/really-png.png",
		"E": "PICTURES/JPG/2020/IMG_20200102_030405.jpeg"} {
		if got[id] != w {
			t.Errorf("%s: %s want %s", id, got[id], w)
		}
	}
}

func TestVideoTier(t *testing.T) {
	v := catalog.Record{Path: "/v/a.mp4", Hash: "V", Meta: meta.Meta{Kind: "video", Time: "2021-02-03 04:05:06", Prefix: "VID", Width: 1080, Height: 1920}}
	o := Options{VideoTier: true}
	if a := BuildActionsWith(PlanWith([]catalog.Record{v}, o), o)[0]; a.CanonRel != "VIDEOS/2021/1080p/VID_20210203_040506.mp4" {
		t.Fatalf("tier: %s", a.CanonRel)
	}
	if a := BuildActions(Plan([]catalog.Record{v}))[0]; a.CanonRel != "VIDEOS/2021/VID_20210203_040506.mp4" {
		t.Fatalf("no tier: %s", a.CanonRel)
	}
}

func TestVerifyFull(t *testing.T) {
	d := t.TempDir()
	// same sparse hash (forged) but different content -> must split
	p1, p2, p3 := filepath.Join(d, "1.bin"), filepath.Join(d, "2.bin"), filepath.Join(d, "3.bin")
	os.WriteFile(p1, []byte("AAAA-same"), 0o644)
	os.WriteFile(p2, []byte("AAAA-same"), 0o644)
	os.WriteFile(p3, []byte("AAAA-diff"), 0o644)
	missing := filepath.Join(d, "gone.bin")
	recs := []catalog.Record{gen("/r", p1, "S", "other", 1), gen("/r", p2, "S", "other", 2), gen("/r", p3, "S", "other", 3), gen("/r", missing, "S", "other", 4)}
	// without verification: one asset, 3 drops
	if r := PlanWith(append([]catalog.Record(nil), recs...), Options{}); r.Assets != 1 {
		t.Fatalf("unverified assets %d", r.Assets)
	}
	r := PlanWith(recs, Options{VerifyFull: true})
	if r.Assets != 3 || r.ExactDupes != 1 || r.VerifiedGroups != 1 || r.VerifySplits != 1 || r.VerifyUnreadable != 1 {
		t.Fatalf("verify: assets=%d dupes=%d groups=%d splits=%d unreadable=%d", r.Assets, r.ExactDupes, r.VerifiedGroups, r.VerifySplits, r.VerifyUnreadable)
	}
	for _, a := range r.Golden {
		for _, m := range a.Members[1:] {
			if m.Path == missing || m.Path == p3 {
				t.Fatalf("%s dropped despite differing/unverifiable content", m.Path)
			}
		}
	}
	// identical group: verified, not split
	ok := PlanWith([]catalog.Record{gen("/r", p1, "T", "other", 1), gen("/r", p2, "T", "other", 2)}, Options{VerifyFull: true})
	if ok.Assets != 1 || ok.VerifySplits != 0 || ok.VerifiedGroups != 1 {
		t.Fatalf("identical group: %+v", ok)
	}
}

// Two unreadable members of one group stay two assets (no exact-dupe drop).
func TestVerifyUnreadableNeverDropped(t *testing.T) {
	recs := []catalog.Record{gen("/r", "/nonexistent/a.bin", "S", "other", 1), gen("/r", "/nonexistent/b.bin", "S", "other", 2)}
	r := PlanWith(recs, Options{VerifyFull: true})
	if r.Assets != 2 || r.ExactDupes != 0 || r.VerifyUnreadable != 2 {
		t.Fatalf("unreadable members: assets=%d dupes=%d unreadable=%d", r.Assets, r.ExactDupes, r.VerifyUnreadable)
	}
}

// A plan refuses manifests whose records carry no host label.
func TestLoadManifestsNeedHost(t *testing.T) {
	d := t.TempDir()
	hdr := `{"onefold":"manifest","zone":"` + meta.ZoneName() + `"}` + "\n"
	write := func(name string, r catalog.Record) string {
		b, _ := json.Marshal(r)
		p := filepath.Join(d, name)
		os.WriteFile(p, []byte(hdr+string(b)+"\n"), 0o644)
		return p
	}
	ok := write("a.jsonl", catalog.Record{Path: "/r/a.txt", Size: 1, Host: "pc"})
	bad := write("b.jsonl", catalog.Record{Path: "/r/b.txt", Size: 1})
	if recs, err := LoadManifests([]string{ok}); err != nil || len(recs) != 1 || recs[0].Host != "pc" {
		t.Fatalf("labeled manifest: %v %v", err, recs)
	}
	if _, err := LoadManifests([]string{ok, bad}); err == nil || !strings.Contains(err.Error(), "no host label") {
		t.Fatalf("unlabeled record must be refused: %v", err)
	}
}

// A known whole-content digest (cache) is used instead of re-reading the file.
func TestVerifyUsesContentHash(t *testing.T) {
	a := gen("/r", "/nonexistent/a.bin", "S", "other", 1)
	b := gen("/r", "/nonexistent/b.bin", "S", "other", 2)
	a.ContentHash, b.ContentHash = "c1", "c1"
	out, st := VerifyGroups([]catalog.Record{a, b}, 2)
	if st.Groups != 1 || st.Unreadable != 0 || st.Splits != 0 || out[0].Hash != "S" {
		t.Fatalf("same content hash: %+v", st)
	}
	b.ContentHash = "c2"
	out, st = VerifyGroups([]catalog.Record{a, b}, 2)
	if st.Splits != 1 || out[0].Hash == out[1].Hash {
		t.Fatalf("different content hash must split: %+v", st)
	}
}

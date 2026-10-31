// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package similar

import (
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mauriziofonte/onefold/internal/plan"
	"github.com/mauriziofonte/onefold/internal/testfix"
)

// scene draws a made-up picture: a diagonal gradient with a block whose
// place depends on shift (another scene = another place).
func scene(w, h, shift int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := uint8((x*255/w + y*255/h) / 2)
			if (x*8/w+shift)%4 == 0 && y*8/h%3 == 0 {
				v = 255 - v
			}
			img.Set(x, y, color.RGBA{v, v / 2, 255 - v, 255})
		}
	}
	return img
}

func save(t *testing.T, p string, img image.Image) {
	t.Helper()
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if filepath.Ext(p) == ".png" {
		err = png.Encode(f, img)
	} else {
		err = jpeg.Encode(f, img, &jpeg.Options{Quality: 60})
	}
	if err != nil {
		t.Fatal(err)
	}
}

// A smaller, recompressed copy of a picture is a pair; another picture is
// not; formats without a pure-Go decoder are skipped, unreadable files listed.
func TestFind(t *testing.T) {
	d := t.TempDir()
	save(t, filepath.Join(d, "a.png"), scene(640, 480, 0))
	save(t, filepath.Join(d, "a_small.jpg"), scene(160, 120, 0))
	save(t, filepath.Join(d, "b.jpg"), scene(640, 480, 2))
	os.WriteFile(filepath.Join(d, "broken.jpg"), []byte("not a jpeg"), 0o644)
	plain := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for i := range plain.Pix {
		plain.Pix[i] = 200
	}
	save(t, filepath.Join(d, "plain1.png"), plain)
	save(t, filepath.Join(d, "plain2.png"), plain)
	act := func(name string) plan.Action {
		return plan.Action{Kind: "photo", SrcHost: "h", SrcPath: filepath.Join(d, name), CanonRel: "PICTURES/" + name}
	}
	acts := []plan.Action{act("a.png"), act("a_small.jpg"), act("b.jpg"), act("broken.jpg"), act("c.heic"), act("plain1.png"), act("plain2.png"),
		{Kind: "video", SrcPath: filepath.Join(d, "v.mp4")}}
	r, err := Find(acts, Options{MaxDistance: 6})
	if err != nil {
		t.Fatal(err)
	}
	if r.Hashed != 3 || r.Skipped != 1 || r.Flat != 2 || len(r.Failed) != 1 {
		t.Fatalf("hashed %d skipped %d flat %d failed %v", r.Hashed, r.Skipped, r.Flat, r.Failed)
	}
	if len(r.Pairs) != 1 || r.Pairs[0].A.CanonRel != "PICTURES/a.png" || r.Pairs[0].B.CanonRel != "PICTURES/a_small.jpg" {
		t.Fatalf("pairs %+v", r.Pairs)
	}
	if _, err := Find(acts, Options{MaxDistance: 17}); err == nil {
		t.Error("distance 17 accepted")
	}
}

// pairs finds exactly the brute-force pairs.
func TestPairsMatchBruteForce(t *testing.T) {
	var ps []Photo
	x := uint64(0x9E3779B97F4A7C15)
	for i := 0; i < 400; i++ {
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		h := x
		if i%3 == 0 && i > 0 {
			h = ps[i-1].Hash ^ (1 << (i % 64)) ^ (1 << ((i * 7) % 64)) // a near copy of the previous one
		}
		ps = append(ps, Photo{CanonRel: string(rune('a'+i%26)) + string(rune('a'+i/26)), Hash: h})
	}
	for _, d := range []int{0, 2, 5, 10} {
		want := 0
		for i := range ps {
			for j := i + 1; j < len(ps); j++ {
				if Distance(ps[i].Hash, ps[j].Hash) <= d {
					want++
				}
			}
		}
		if got := len(pairs(ps, d)); got != want {
			t.Errorf("distance %d: %d pairs, brute force %d", d, got, want)
		}
	}
}

// The same text saved as docx, odt and rtf is one group; another text and a
// short one are not grouped. Read-only: nothing is dropped.
func TestFindDocs(t *testing.T) {
	d := t.TempDir()
	body := strings.Repeat("The quarterly report of the archive project, with its numbers and its plans. ", 5)
	created := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(testfix.Docx(filepath.Join(d, "report.docx"), "Report", "A", created, 1, body))
	must(testfix.ODT(filepath.Join(d, "report.odt"), "Report", "A", created, 1, strings.ToUpper(body)))
	must(testfix.RTF(filepath.Join(d, "report.rtf"), "Report", "A", created, body))
	must(testfix.Docx(filepath.Join(d, "other.docx"), "Other", "A", created, 1, strings.Repeat("A different text entirely, about something else. ", 6)))
	must(testfix.Docx(filepath.Join(d, "short.docx"), "S", "A", created, 1, "tiny"))
	must(testfix.Docx(filepath.Join(d, "short2.docx"), "S", "A", created, 1, "tiny"))
	act := func(n string) plan.Action {
		return plan.Action{Kind: "document", SrcHost: "h", SrcPath: filepath.Join(d, n), CanonRel: "FILES/" + n}
	}
	gs, n, err := FindDocs([]plan.Action{act("report.docx"), act("report.odt"), act("report.rtf"), act("other.docx"),
		act("short.docx"), act("short2.docx"), {Kind: "photo", SrcPath: filepath.Join(d, "x.jpg")}}, Options{})
	if err != nil || n != 6 {
		t.Fatalf("%v, %d documents", err, n)
	}
	if len(gs) != 1 || len(gs[0].Members) != 3 || strings.Join(gs[0].Formats, ",") != "docx,odt,rtf" {
		t.Fatalf("groups %+v", gs)
	}
}

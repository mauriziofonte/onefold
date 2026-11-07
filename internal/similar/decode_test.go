// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package similar

import (
	"bytes"
	"encoding/binary"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/image/tiff"

	"github.com/mauriziofonte/onefold/internal/plan"
	"github.com/mauriziofonte/onefold/internal/tools"
)

// noise is deterministic filler that stands for the sensor data of a RAW.
func noise(n int, seed uint64) []byte {
	b := make([]byte, n)
	for i := range b {
		seed ^= seed << 13
		seed ^= seed >> 7
		seed ^= seed << 17
		b[i] = byte(seed)
	}
	return b
}

func jpegOf(t *testing.T, w, h, shift int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, scene(w, h, shift), &jpeg.Options{Quality: 70}); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// RAW photos are read through the JPEG preview they embed (the first large
// one, across the chunks the file is scanned in, past a fake start marker),
// else their largest small JPEG, else, TIFF-based, as a TIFF; TIFF and WebP
// are read in pure Go. Each pairs with the PNG of the same scene.
func TestFindReadsRAWTIFFWebP(t *testing.T) {
	t.Setenv("ONEFOLD_NO_EXTERNAL", "1")
	d := t.TempDir()
	save(t, filepath.Join(d, "a.png"), scene(640, 480, 0))
	write := func(name string, parts ...[]byte) {
		if err := os.WriteFile(filepath.Join(d, name), bytes.Join(parts, nil), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// a thumbnail of another scene first, then a broken start marker, then
	// the preview starting one byte before the first 1 MiB chunk ends
	head := append([]byte("II*\x00"), noise(4096, 1)...)
	thumb := jpegOf(t, 160, 120, 2)
	fake := []byte{0xFF, 0xD8, 0xFF, 0xC0, 0x00, 0x11, 0x08, 0x10, 0x00}
	pad := noise((1<<20)-1-len(head)-len(thumb)-len(fake), 2)
	write("big.nef", head, thumb, fake, pad, jpegOf(t, 1200, 900, 0), noise(1<<20, 3))
	write("small.raf", noise(148, 4), jpegOf(t, 320, 240, 0), noise(5000, 5), jpegOf(t, 160, 120, 3))
	var tb bytes.Buffer
	if err := tiff.Encode(&tb, scene(320, 240, 0), nil); err != nil {
		t.Fatal(err)
	}
	write("plain.dng", tb.Bytes())
	write("scan.tif", tb.Bytes())
	write("none.cr2", []byte("II*\x00"), noise(10000, 6))
	webp, err := os.ReadFile(filepath.Join("testdata", "scene.webp")) // made from scene(160, 120, 0)
	if err != nil {
		t.Fatal(err)
	}
	write("web.webp", webp)
	act := func(name string) plan.Action {
		return plan.Action{Kind: "photo", SrcHost: "h", SrcPath: filepath.Join(d, name), CanonRel: "PICTURES/" + name}
	}
	names := []string{"a.png", "big.nef", "small.raf", "plain.dng", "scan.tif", "web.webp", "none.cr2"}
	var acts []plan.Action
	for _, n := range names {
		acts = append(acts, act(n))
	}
	r, err := Find(acts, Options{MaxDistance: 6})
	if err != nil {
		t.Fatal(err)
	}
	if r.Hashed != 6 || r.Previews != 3 || r.Skipped != 0 || len(r.Failed) != 1 || !strings.Contains(r.Failed[0], "none.cr2: no embedded JPEG preview") {
		t.Fatalf("hashed %d previews %d skipped %d failed %v", r.Hashed, r.Previews, r.Skipped, r.Failed)
	}
	paired := map[string]bool{}
	for _, p := range r.Pairs {
		if p.A.CanonRel == "PICTURES/a.png" {
			paired[p.B.CanonRel] = true
		}
	}
	for _, n := range names[1:6] {
		if !paired["PICTURES/"+n] {
			t.Errorf("%s does not pair with a.png: %+v", n, r.Pairs)
		}
	}
}

// HEIF, HEIC and AVIF go through ffmpeg, at one eighth of their size; a
// file whose declared size ffmpeg did not read (one tile of a tiled HEIC)
// is unreadable, not a wrong hash. Without ffmpeg they are skipped.
func TestFindReadsAVIFWithFFmpeg(t *testing.T) {
	ff, ok := tools.Path("ffmpeg")
	if !ok {
		t.Skip("ffmpeg not installed")
	}
	d := t.TempDir()
	src := filepath.Join(d, "a.png")
	save(t, src, scene(320, 240, 0))
	avif := filepath.Join(d, "a.avif")
	if out, err := exec.Command(ff, "-nostdin", "-loglevel", "error", "-i", src, "-frames:v", "1",
		"-c:v", "libaom-av1", "-still-picture", "1", avif).CombinedOutput(); err != nil {
		t.Skipf("this ffmpeg cannot write AVIF: %v %s", err, out)
	}
	b, err := os.ReadFile(avif)
	if err != nil {
		t.Fatal(err)
	}
	i := bytes.Index(b, []byte("ispe"))
	if i < 0 {
		t.Fatal("no ispe in the AVIF ffmpeg wrote")
	}
	// the same pixels, a declared size they do not have
	binary.BigEndian.PutUint32(b[i+8:], 4032)
	binary.BigEndian.PutUint32(b[i+12:], 3024)
	if err := os.WriteFile(filepath.Join(d, "tile.avif"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	act := func(name string) plan.Action {
		return plan.Action{Kind: "photo", SrcHost: "h", SrcPath: filepath.Join(d, name), CanonRel: "PICTURES/" + name}
	}
	acts := []plan.Action{act("a.png"), act("a.avif"), act("tile.avif")}
	r, err := Find(acts, Options{MaxDistance: 6})
	if err != nil {
		t.Fatal(err)
	}
	if r.Hashed != 2 || r.FFmpeg != 1 || len(r.Failed) != 1 || !strings.Contains(r.Failed[0], "320x240 of a 4032x3024 photo") {
		t.Fatalf("hashed %d ffmpeg %d failed %v", r.Hashed, r.FFmpeg, r.Failed)
	}
	if len(r.Pairs) != 1 || r.Pairs[0].B.CanonRel != "PICTURES/a.png" && r.Pairs[0].A.CanonRel != "PICTURES/a.png" {
		t.Fatalf("pairs %+v", r.Pairs)
	}
	// the frame ffmpeg wrote is one eighth of the photo
	img, err := ffmpegFrame(filepath.Join(d, "a.avif"))
	if err != nil || img.Bounds().Dx() != 40 || img.Bounds().Dy() != 30 {
		t.Fatalf("frame %v %v", img.Bounds(), err)
	}
	t.Setenv("ONEFOLD_NO_EXTERNAL", "1")
	if r, _ := Find(acts, Options{MaxDistance: 6}); r.Skipped != 2 || r.NoFFmpeg != 2 || r.Hashed != 1 {
		t.Fatalf("without ffmpeg: %+v", r)
	}
}

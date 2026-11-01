// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mauriziofonte/onefold/internal/testfix"
)

func rome(t *testing.T) *time.Location {
	t.Helper()
	l, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func extract(t *testing.T, p string, quick bool) Meta {
	t.Helper()
	m, err := ExtractWith(p, Options{Quick: quick})
	must(t, err)
	return m
}

func wantTime(t *testing.T, m Meta, time_, src string) {
	t.Helper()
	if m.Time != time_ || m.TimeSource != src {
		t.Fatalf("time=%q src=%q, want %q %q (meta %+v)", m.Time, m.TimeSource, time_, src, m)
	}
}

func TestZipNewestNonJunkMember(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "a.zip")
	must(t, testfix.Zip(p, []testfix.Member{
		{Name: "doc/a.txt", Body: "a", ModTime: time.Date(2019, 3, 4, 10, 5, 0, 0, time.UTC)},
		{Name: "doc/b.txt", Body: "b", ModTime: time.Date(2020, 6, 7, 8, 9, 10, 0, time.UTC)},
		{Name: "__MACOSX/doc/._b.txt", Body: "x", ModTime: time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Name: "node_modules/x.js", Body: "x", ModTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
	}))
	m := extract(t, p, false)
	if m.Kind != "archive" || m.Entries != 2 {
		t.Fatalf("kind/entries: %+v", m)
	}
	wantTime(t, m, "2020-06-07 08:09:10", "metadata")
}

func TestTarGzNewestMember(t *testing.T) {
	d := t.TempDir()
	loc := rome(t)
	p := filepath.Join(d, "b.tar.gz")
	must(t, testfix.TarGz(p, []testfix.Member{
		{Name: "x/1.txt", Body: "1", ModTime: time.Date(2018, 1, 2, 3, 4, 5, 0, loc)},
		{Name: "x/2.txt", Body: "2", ModTime: time.Date(2018, 5, 6, 7, 8, 9, 0, loc)},
	}, true))
	m := extract(t, p, false)
	wantTime(t, m, "2018-05-06 07:08:09", "metadata")
	pt := filepath.Join(d, "c.tar")
	must(t, testfix.TarGz(pt, []testfix.Member{{Name: "y", Body: "y", ModTime: time.Date(2017, 1, 1, 12, 0, 0, 0, loc)}}, false))
	wantTime(t, extract(t, pt, false), "2017-01-01 12:00:00", "metadata")
}

func Test7zListing(t *testing.T) {
	if _, err := exec.LookPath("7z"); err != nil {
		t.Skip("7z not installed")
	}
	d := t.TempDir()
	src := filepath.Join(d, "src")
	must(t, testfix.File(filepath.Join(src, "a.txt"), "aaa", time.Date(2016, 2, 3, 4, 5, 6, 0, time.Local)))
	must(t, testfix.File(filepath.Join(src, "b.txt"), "bbb", time.Date(2015, 2, 3, 4, 5, 6, 0, time.Local)))
	p := filepath.Join(d, "a.7z")
	cmd := exec.Command("7z", "a", "-bd", p, "a.txt", "b.txt")
	cmd.Dir = src
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("7z: %v %s", err, out)
	}
	m := extract(t, p, false)
	if m.Entries != 2 || m.TimeSource != "metadata" || m.Time[:4] != "2016" {
		t.Fatalf("7z meta: %+v", m)
	}
}

func TestDocxCoreProps(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "r.docx")
	// 09:30Z in June = 11:30 Rome (CEST)
	must(t, testfix.Docx(p, "Relazione", "Mario Rossi", time.Date(2021, 6, 1, 9, 30, 0, 0, time.UTC), 7, "hello"))
	m := extract(t, p, false)
	if m.Kind != "document" || m.Title != "Relazione" || m.Author != "Mario Rossi" || m.Pages != 7 {
		t.Fatalf("docx: %+v", m)
	}
	wantTime(t, m, "2021-06-01 11:30:00", "metadata")
	if m.ExtMismatch {
		t.Fatal("docx is a zip: must not be a mismatch")
	}
}

func TestODTMeta(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "r.odt")
	must(t, testfix.ODT(p, "Verbale", "Anna", time.Date(2019, 11, 2, 16, 45, 1, 0, time.UTC), 3, "x"))
	m := extract(t, p, false)
	if m.Title != "Verbale" || m.Author != "Anna" || m.Pages != 3 {
		t.Fatalf("odt: %+v", m)
	}
	wantTime(t, m, "2019-11-02 16:45:01", "metadata") // wall clock kept
}

func TestRTFInfo(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "n.rtf")
	must(t, testfix.RTF(p, "Nota", "Luca", time.Date(2014, 7, 8, 9, 10, 0, 0, time.UTC), "body"))
	m := extract(t, p, false)
	if m.Title != "Nota" || m.Author != "Luca" {
		t.Fatalf("rtf: %+v", m)
	}
	wantTime(t, m, "2014-07-08 09:10:00", "metadata")
}

func TestPDFInfoAndScanAgree(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "f.pdf")
	loc := time.FixedZone("x", 3600)
	must(t, testfix.PDF(p, "Fattura", "ACME", time.Date(2022, 1, 15, 10, 0, 0, 0, loc), "Totale 100"))
	// pure-Go scan
	var s Meta
	pdfScan(p, &s)
	if s.Time != "2022-01-15 10:00:00" || s.Title != "Fattura" || s.Author != "ACME" {
		t.Fatalf("pdfScan: %+v", s)
	}
	if _, err := exec.LookPath("pdfinfo"); err != nil {
		t.Skip("pdfinfo not installed")
	}
	var i Meta
	if !pdfInfo(p, &i) {
		t.Fatal("pdfinfo failed on fixture")
	}
	if i.Time != s.Time || i.Title != s.Title || i.Author != s.Author || i.Pages != 1 {
		t.Fatalf("pdfinfo %+v != scan %+v", i, s)
	}
}

func TestParsePDFDate(t *testing.T) {
	cases := map[string]string{
		"D:20220115100000+01'00'": "2022-01-15 10:00:00",
		"D:20220715100000Z":       "2022-07-15 12:00:00", // UTC -> CEST
		"D:2022":                  "2022-01-01 01:00:00", // zoneless = UTC (pdfinfo parity)
		"D:20220115":              "2022-01-15 01:00:00",
		"D:20260127145441":        "2026-01-27 15:54:41",
	}
	for in, want := range cases {
		tt, wall, ok := parsePDFDate(in)
		if !ok {
			t.Fatalf("%s: not parsed", in)
		}
		var m Meta
		setTime(&m, tt, wall)
		if m.Time != want {
			t.Errorf("%s -> %s, want %s", in, m.Time, want)
		}
	}
}

func TestPDFStrings(t *testing.T) {
	for in, want := range map[string]string{
		"<FEFF00730061006D0070006C00650000>": "sample",
		`(Fattura \(copia\) n\2601)`:         "Fattura (copia) n°1",
		`(caf\351)`:                          "café",
		"(\376\377\000A\000B)":               "AB",
	} {
		if got := pdfString([]byte(in)); got != want {
			t.Errorf("pdfString(%s)=%q want %q", in, got, want)
		}
	}
}

func TestImagePNGDimsAndMismatch(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "a.png")
	must(t, testfix.PNG(p, 40, 30, 1))
	m := extract(t, p, false)
	if m.Kind != "photo" || m.Prefix != "IMG" || m.Width != 40 || m.Height != 30 || m.ExtMismatch {
		t.Fatalf("png: %+v", m)
	}
	// a PNG named .jpg: photo kind, flagged, fixable to png
	q := filepath.Join(d, "b.jpg")
	b, _ := os.ReadFile(p)
	must(t, os.WriteFile(q, b, 0o644))
	mq := extract(t, q, false)
	if !mq.ExtMismatch || mq.SniffExt != "png" || mq.Kind != "photo" {
		t.Fatalf("mismatch: %+v", mq)
	}
}

func TestQuickAndFallbacks(t *testing.T) {
	d := t.TempDir()
	loc := rome(t)
	mt := time.Date(2013, 4, 5, 6, 7, 8, 0, loc)
	p := filepath.Join(d, "a.zip")
	must(t, testfix.Zip(p, []testfix.Member{{Name: "a", Body: "a", ModTime: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)}}))
	must(t, os.Chtimes(p, mt, mt))
	wantTime(t, extract(t, p, true), "2013-04-05 06:07:08", "filemtime") // quick: no listing
	// filename date beats mtime for generic kinds
	f := filepath.Join(d, "scan_2011-02-03.txt")
	must(t, testfix.File(f, "x", mt))
	m := extract(t, f, false)
	wantTime(t, m, "2011-02-03 12:00:00", "filename")
	if m.Kind != "text" || m.FileCategory != "text" {
		t.Fatalf("txt: %+v", m)
	}
	o := filepath.Join(d, "blob.xyzq")
	must(t, testfix.File(o, "x", mt))
	mo := extract(t, o, false)
	if mo.Kind != "other" {
		t.Fatalf("other: %+v", mo)
	}
	wantTime(t, mo, "2013-04-05 06:07:08", "filemtime")
}

// The media scope is the golden scope: photos of every
// photographic format and every video container.
func TestInScopeUnchanged(t *testing.T) {
	for _, p := range []string{"a.jpg", "a.JPEG", "a.heic", "a.png", "a.webp", "a.tif", "a.cr2", "a.NEF",
		"a.mov", "a.mp4", "a.mkv", "a.avi", "a.3gp", "a.webm", "a.mpg", "a.m4v"} {
		if !InScope(p) {
			t.Errorf("%s must be in media scope", p)
		}
	}
	for _, p := range []string{"a.gif", "a.bmp", "a.svg", "a.psd", "a.nksc", "a.pdf", "a"} {
		if InScope(p) {
			t.Errorf("%s must NOT be in media scope", p)
		}
	}
}

func TestAudioTags(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	if _, err := exec.LookPath("exiftool"); err != nil {
		t.Skip("exiftool not installed")
	}
	d := t.TempDir()
	p := filepath.Join(d, "song.mp3")
	cmd := exec.Command("ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "sine=d=1", "-metadata", "artist=The Band",
		"-metadata", "album=First", "-metadata", "title=Opening", "-metadata", "date=2019", "-metadata", "track=3/10",
		"-metadata", "disc=2/2", p)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg cannot encode mp3 here: %v %s", err, out)
	}
	m := extract(t, p, false)
	if m.Kind != "audio" || m.Author != "The Band" || m.Album != "First" || m.Title != "Opening" ||
		m.TagYear != "2019" || m.Track != 3 || m.Disc != 2 {
		t.Fatalf("audio: %+v", m)
	}
	if m.TimeSource == "metadata" {
		t.Fatalf("ID3 release year must not become the file time: %+v", m)
	}
}

func TestImageExifDate(t *testing.T) {
	if _, err := exec.LookPath("exiftool"); err != nil {
		t.Skip("exiftool not installed")
	}
	d := t.TempDir()
	p := filepath.Join(d, "shot.png")
	must(t, testfix.PNG(p, 20, 10, 9))
	out, err := exec.Command("exiftool", "-q", "-overwrite_original", "-DateTimeOriginal=2012:03:04 05:06:07",
		"-Make=NIKON CORPORATION", "-Model=NIKON D3500", p).CombinedOutput()
	if err != nil {
		t.Fatalf("exiftool write: %v %s", err, out)
	}
	m := extract(t, p, false)
	wantTime(t, m, "2012-03-04 05:06:07", "metadata")
	if m.CanonDevice != "Nikon D3500" || m.Category != "camera" || m.Width != 20 {
		t.Fatalf("image: %+v", m)
	}
}

// Regression: a PNG named .jpg used to hang imagemeta's ScanJPEG forever.
func TestNonJPEGNamedJpgDoesNotHang(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "wrong_extension.jpg")
	must(t, testfix.PNG(p, 8, 8, 3))
	done := make(chan Meta, 1)
	go func() { m, _ := ExtractWith(p, Options{}); done <- m }()
	select {
	case m := <-done:
		if m.Kind != "photo" || !m.ExtMismatch || m.TimeSource != "filemtime" {
			t.Fatalf("meta: %+v", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("extractPhoto hangs on a non-JPEG .jpg")
	}
}

// A .tar.gz larger than the inflate budget is dated from the gzip header and
// does not report a (partial) member count.
func TestTarGzBudget(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "big.tar.gz")
	var tb bytes.Buffer
	tw := tar.NewWriter(&tb)
	rnd := make([]byte, 300<<10)
	rand.New(rand.NewSource(1)).Read(rnd) // incompressible
	for i := 0; i < 4; i++ {
		tw.WriteHeader(&tar.Header{Name: fmt.Sprintf("m%d", i), Mode: 0o644, Size: int64(len(rnd)), ModTime: time.Date(2015, 1, 1+i, 0, 0, 0, 0, time.UTC), Typeflag: tar.TypeReg})
		tw.Write(rnd)
	}
	tw.Close()
	var gb bytes.Buffer
	gw := gzip.NewWriter(&gb)
	gw.ModTime = time.Date(2020, 6, 1, 10, 0, 0, 0, time.UTC)
	gw.Write(tb.Bytes())
	gw.Close()
	must(t, os.WriteFile(p, gb.Bytes(), 0o644))

	full := extract(t, p, false)
	if full.Entries != 4 || full.Time != "2015-01-04 01:00:00" {
		t.Fatalf("complete listing: %+v", full)
	}
	old := gzBudget
	gzBudget = 64 << 10
	defer func() { gzBudget = old }()
	cut := extract(t, p, false)
	if cut.Entries != 0 || cut.Time != "2020-06-01 12:00:00" || cut.TimeSource != "metadata" {
		t.Fatalf("budgeted listing: %+v", cut)
	}
}

func TestSevenZipUTC(t *testing.T) {
	listed := time.Date(2026, 1, 27, 16, 54, 42, 0, time.UTC) // 7-Zip for Windows, printed in October (CEST)
	now := time.Date(2026, 10, 6, 18, 0, 0, 0, time.FixedZone("CEST", 2*3600))
	if got := sevenZipUTC(listed, "windows", now); !got.Equal(time.Date(2026, 1, 27, 14, 54, 42, 0, time.UTC)) {
		t.Fatalf("windows: got %v", got)
	}
	if got := sevenZipUTC(listed, "linux", now); !got.Equal(listed) {
		t.Fatalf("linux (TZ=UTC honoured): got %v", got)
	}
}

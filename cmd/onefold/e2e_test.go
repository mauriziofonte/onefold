// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/config"
	"github.com/mauriziofonte/onefold/internal/meta"
	"github.com/mauriziofonte/onefold/internal/naming"
	"github.com/mauriziofonte/onefold/internal/plan"
	"github.com/mauriziofonte/onefold/internal/settings"
	"github.com/mauriziofonte/onefold/internal/testfix"
)

var bin string

func TestMain(m *testing.M) {
	// every binary run gets its own state dir: the user's config (owner,
	// signature, language) must not leak into the tests
	state, err := os.MkdirTemp("", "onefold-e2e-state")
	if err != nil {
		panic(err)
	}
	os.Setenv("ONEFOLD_STATE_DIR", state)
	// the expectations are wall clocks in Europe/Rome: pin the zone of the
	// binary through the user's config and of this process
	if err := config.SaveTo(filepath.Join(state, config.FileName), config.Config{Timezone: "Europe/Rome"}); err != nil {
		panic(err)
	}
	rome, err := meta.LoadZone("Europe/Rome")
	if err != nil {
		panic(err)
	}
	meta.SetZone(rome)
	// ONEFOLD_BIN: run the E2E against a prebuilt binary (Windows has no Go toolchain).
	if b := os.Getenv("ONEFOLD_BIN"); b != "" {
		bin = b
		code := m.Run()
		os.RemoveAll(state)
		os.Exit(code)
	}
	dir, err := os.MkdirTemp("", "onefold-e2e-bin")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "onefold")
	if runtime.GOOS == "windows" {
		bin += ".exe" // exec finds a Windows program only by its extension
	}
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		panic(string(out))
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.RemoveAll(state)
	os.Exit(code)
}

func need(t *testing.T, tools ...string) {
	t.Helper()
	for _, x := range tools {
		if _, err := exec.LookPath(x); err != nil {
			t.Skipf("%s not installed", x)
		}
	}
}

func dd(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command(bin, args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("onefold %v: %v\nstdout:\n%s\nstderr:\n%s", args, err, out.String(), errb.String())
	}
	return out.String()
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func sh(t *testing.T, name string, args ...string) {
	t.Helper()
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

func writeJPEG(t *testing.T, p string, seed uint8) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{seed, uint8(x * 3), uint8(y * 5), 255})
		}
	}
	var b bytes.Buffer
	must(t, jpeg.Encode(&b, img, &jpeg.Options{Quality: 90}))
	must(t, os.MkdirAll(filepath.Dir(p), 0o755))
	must(t, os.WriteFile(p, b.Bytes(), 0o644))
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	b, err := os.ReadFile(src)
	must(t, err)
	must(t, os.MkdirAll(filepath.Dir(dst), 0o755))
	must(t, os.WriteFile(dst, b, 0o644))
}

func snapshot(t *testing.T, root string) string {
	t.Helper()
	var lines []string
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, _ := os.ReadFile(p)
		fi, _ := d.Info()
		h := sha256.Sum256(b)
		lines = append(lines, p+" "+hex.EncodeToString(h[:])+" "+fi.ModTime().UTC().String())
		return nil
	})
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

func loadPlan(t *testing.T, p string) []plan.Action {
	t.Helper()
	acts, err := plan.Read(p)
	must(t, err)
	return acts
}

func byName(acts []plan.Action) map[string]plan.Action {
	m := map[string]plan.Action{}
	for _, a := range acts {
		m[filepath.Base(a.SrcPath)+"@"+a.SrcHost] = a
	}
	return m
}

// TestE2E drives catalog -> plan -> apply -> export-text on a synthetic
// two-host corpus and checks the contract end to end.
func TestE2E(t *testing.T) {
	need(t, "exiftool", "ffmpeg", "ffprobe")
	root := testfix.TempDir(t)
	A, B := filepath.Join(root, "roomA"), filepath.Join(root, "roomB")
	ts := time.Date(2021, 6, 1, 9, 30, 0, 0, time.UTC)
	old := time.Date(2015, 3, 3, 3, 3, 3, 0, time.Local)

	// --- media
	writeJPEG(t, filepath.Join(A, "DCIM/p1.jpg"), 1)
	sh(t, "exiftool", "-q", "-overwrite_original", "-DateTimeOriginal=2019:08:09 23:02:16", "-SubSecTimeOriginal=123",
		"-Make=NIKON CORPORATION", "-Model=NIKON D3500", filepath.Join(A, "DCIM/p1.jpg"))
	copyFile(t, filepath.Join(A, "DCIM/p1.jpg"), filepath.Join(B, "backup/photo-copy.jpg")) // exact dupe on the other host
	copyFile(t, filepath.Join(A, "DCIM/p1.jpg"), filepath.Join(A, "DCIM/p1.jpg.bak"))       // generic copy of a media winner
	must(t, os.MkdirAll(filepath.Join(A, "VID"), 0o755))
	sh(t, "ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=d=2:s=320x240", "-pix_fmt", "yuv420p",
		"-metadata", "creation_time=2021-04-04T11:26:44Z", filepath.Join(A, "VID/clip.mp4"))

	// --- documents
	must(t, testfix.Docx(filepath.Join(A, "docs/report.docx"), "Relazione Q1", "Mario Rossi", ts, 4, "Contenuto relazione"))
	copyFile(t, filepath.Join(A, "docs/report.docx"), filepath.Join(B, "old/report (1).docx")) // dirty-name dupe
	must(t, testfix.Docx(filepath.Join(B, "docs/report.docx"), "Relazione Q1", "Mario Rossi", ts, 4, "Versione diversa"))
	must(t, testfix.ODT(filepath.Join(A, "docs/verbale.odt"), "Verbale", "Anna", time.Date(2019, 11, 2, 16, 45, 1, 0, time.UTC), 2, "Testo verbale"))
	must(t, testfix.RTF(filepath.Join(A, "docs/nota.rtf"), "Nota", "Luca", time.Date(2014, 7, 8, 9, 10, 0, 0, time.UTC), `Citt\'e0`))
	must(t, testfix.PDF(filepath.Join(B, "pdf/fattura.pdf"), "Fattura 12", "ACME", time.Date(2022, 1, 15, 10, 0, 0, 0, time.FixedZone("x", 3600)), "Totale 100"))
	must(t, testfix.File(filepath.Join(A, "notes/scan_2011-02-03.txt"), "appunti", old))
	must(t, testfix.File(filepath.Join(A, "notes/Readme.MD"), "# readme", old))

	// --- archives, images, audio, other
	zipMembers := []testfix.Member{{Name: "a/1.txt", Body: "1", ModTime: time.Date(2018, 5, 6, 7, 8, 9, 0, time.UTC)}}
	must(t, testfix.Zip(filepath.Join(A, "arch/backup.zip"), zipMembers))
	copyFile(t, filepath.Join(A, "arch/backup.zip"), filepath.Join(B, "arch/backup-copy.zip"))
	must(t, testfix.PNG(filepath.Join(A, "img/logo.png"), 30, 20, 7))
	must(t, testfix.PNG(filepath.Join(A, "img/really-png.dat.gif"), 30, 20, 8)) // gif ext, png content
	must(t, os.MkdirAll(filepath.Join(A, "music"), 0o755))
	sh(t, "ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "sine=d=1", "-metadata", "artist=The Band", "-metadata", "album=Live",
		"-metadata", "title=Intro", "-metadata", "date=2019", "-metadata", "track=3", filepath.Join(A, "music/x.mp3"))
	must(t, testfix.File(filepath.Join(B, "bin/setup.exe"), "MZ fake exe", old))
	must(t, testfix.File(filepath.Join(B, "bin/LICENSE"), "no extension", old))

	// --- junk that must never be cataloged
	for _, j := range []string{"Thumbs.db", ".DS_Store", "__MACOSX/._x.jpg", ".thumbnails/t.jpg", "node_modules/m.txt", "Windows/w.txt"} {
		must(t, testfix.File(filepath.Join(B, j), "junk", old))
	}
	before := snapshot(t, root)

	work := testfix.TempDir(t)
	ma, mb := filepath.Join(work, "a.jsonl"), filepath.Join(work, "b.jsonl")

	// Default scope is media only: the golden scope, here p1.jpg, clip.mp4, logo.png
	dd(t, "catalog", "-o", filepath.Join(work, "media.jsonl"), A)
	media, _ := catalog.LoadManifest(filepath.Join(work, "media.jsonl"))
	if len(media) != 3 {
		t.Fatalf("default media scope: %d records", len(media))
	}

	dd(t, "catalog", "-preset", "all", "-allow-mixed", "-host", "pc", "-o", ma, A)
	dd(t, "catalog", "-preset", "all", "-allow-mixed", "-host", "nas", "-o", mb, B) // skip rules are ON by default
	ra, _ := catalog.LoadManifest(ma)
	rb, _ := catalog.LoadManifest(mb)
	for _, r := range rb {
		for _, junk := range []string{"Thumbs.db", ".DS_Store", "__MACOSX", ".thumbnails", "node_modules", "Windows", "setup.exe", "LICENSE"} {
			if strings.Contains(r.Path, junk) {
				t.Fatalf("junk/swept file cataloged: %s", r.Path)
			}
		}
	}
	if len(ra) != 12 || len(rb) != 5 {
		t.Fatalf("records A=%d B=%d", len(ra), len(rb))
	}

	// F1 opt-out: -no-sweep brings back system/dev dirs, system ext and no-ext files (junk stays out)
	mns := filepath.Join(work, "b-nosweep.jsonl")
	dd(t, "catalog", "-preset", "all", "-allow-mixed", "-host", "nas", "-no-sweep", "-o", mns, B)
	rns, _ := catalog.LoadManifest(mns)
	paths := canonPaths(rns)
	for _, want := range []string{"Windows/w.txt", "node_modules/m.txt", "bin/setup.exe", "bin/LICENSE"} {
		if !strings.Contains(paths, want) {
			t.Errorf("-no-sweep must include %s", want)
		}
	}
	for _, junk := range []string{"Thumbs.db", ".DS_Store", "__MACOSX", ".thumbnails"} {
		if strings.Contains(paths, junk) {
			t.Errorf("junk %s must stay excluded with -no-sweep", junk)
		}
	}
	md1 := filepath.Join(work, "b-dev.jsonl")
	dd(t, "catalog", "-preset", "all", "-allow-mixed", "-host", "nas", "-skip-dev-dirs=false", "-o", md1, B)
	rdev, _ := catalog.LoadManifest(md1)
	if p := canonPaths(rdev); !strings.Contains(p, "node_modules/m.txt") || strings.Contains(p, "Windows/w.txt") {
		t.Errorf("single opt-out: %s", p)
	}

	// Reuse == fresh
	mr := filepath.Join(work, "a-reuse.jsonl")
	out := dd(t, "catalog", "-preset", "all", "-allow-mixed", "-host", "pc", "-reuse", ma, "-o", mr, A)
	if !strings.Contains(out, "reused: 12") {
		t.Fatalf("reuse summary: %s", out)
	}
	rr, _ := catalog.LoadManifest(mr)
	if canon(rr) != canon(ra) {
		t.Fatal("reused manifest differs from fresh one")
	}

	// owner and signature from the user config (onefold config), read by plan and apply
	dd(t, "config", "set", "artist", "E2E Owner")
	dd(t, "config", "set", "signature", "E2E signature")
	if out := dd(t, "config"); !strings.Contains(out, "E2E Owner") || !strings.Contains(out, "diag-log") {
		t.Fatalf("config list: %s", out)
	}

	// plan
	pp := filepath.Join(work, "plan.jsonl")
	out = dd(t, "plan", "-o", pp, "-layout", "document", "-verify-full", "-fix-ext", "-report", filepath.Join(work, "r.md"), ma, mb)
	if !strings.Contains(out, "verify-full:") {
		t.Fatalf("plan output: %s", out)
	}
	dd(t, "plan", "-o", filepath.Join(work, "plan2.jsonl"), "-layout", "document", "-verify-full", "-fix-ext", "-report", filepath.Join(work, "r.json"), ma, mb)
	acts := loadPlan(t, pp)
	got := byName(acts)
	expect := map[string]string{
		"p1.jpg@pc":              "PICTURES/JPG/2019/DSC_20190809_230216.jpg",
		"report.docx@pc":         "DOCUMENTS/DOC/2021/Mario Rossi/Relazione Q1",
		"verbale.odt@pc":         "DOCUMENTS/DOC/2019/Anna/Verbale.odt",
		"nota.rtf@pc":            "DOCUMENTS/DOC/2014/Luca/Nota.rtf",
		"fattura.pdf@nas":        "DOCUMENTS/PDF/2022/ACME/Fattura 12.pdf",
		"scan_2011-02-03.txt@pc": "DOCUMENTS/TXT/2011/Unknown/scan_2011-02-03.txt",
		"Readme.MD@pc":           "DOCUMENTS/TXT/2015/Unknown/Readme.md",
		"backup.zip@pc":          "FILES/ZIP/2018/backup.zip",
		"logo.png@pc":            "PICTURES/PNG/", // a photo of the golden scope
		"x.mp3@pc":               "FILES/MP3/",
	}
	for k, want := range expect {
		a, ok := got[k]
		if !ok {
			t.Errorf("missing golden %s", k)
			continue
		}
		if !strings.HasPrefix(a.CanonRel, want) && a.CanonRel != want {
			t.Errorf("%s -> %s, want %s", k, a.CanonRel, want)
		}
	}
	if a := got["logo.png@pc"]; !strings.HasPrefix(a.CanonRel, "PICTURES/PNG/") || !a.IsMediaAction() {
		t.Errorf("logo: %s", a.CanonRel)
	}
	// different-content docx with the same title: kept, collision-suffixed
	pair := []string{got["report.docx@pc"].CanonRel, got["report.docx@nas"].CanonRel}
	sort.Strings(pair)
	if pair[0] != "DOCUMENTS/DOC/2021/Mario Rossi/Relazione Q1.docx" || pair[1] != "DOCUMENTS/DOC/2021/Mario Rossi/Relazione Q1_01.docx" {
		t.Errorf("distinct same-title docx not both kept: %v", pair)
	}
	// dupes: photo copy on nas + .bak absorbed; dirty docx copy dropped; zip copy dropped
	p1 := got["p1.jpg@pc"]
	dropped := map[string]string{}
	for _, a := range acts {
		for _, d := range a.Drops {
			dropped[filepath.Base(d.Path)] = d.Reason
		}
	}
	for _, f := range []string{"photo-copy.jpg", "p1.jpg.bak", "report (1).docx", "backup-copy.zip"} {
		if dropped[f] != "exact-dupe" {
			t.Errorf("%s not dropped as exact-dupe (%q)", f, dropped[f])
		}
	}
	if len(p1.Drops) != 2 || p1.Artist != "E2E Owner" {
		t.Errorf("photo asset: %+v", p1)
	}
	if got["report.docx@pc"].Artist != "" {
		t.Error("generic action must not carry an Artist")
	}
	// Gif named file with png content -> fix-ext renames to .png
	if a := got["really-png.dat.gif@pc"]; !strings.HasSuffix(a.CanonRel, "/really-png.dat.png") {
		t.Errorf("fix-ext: %s", a.CanonRel)
	}
	var clip plan.Action
	for _, a := range acts {
		if a.Kind == "video" {
			clip = a
		}
	}
	if !strings.HasPrefix(clip.CanonRel, "VIDEOS/2021/VID_20210404_1326") {
		t.Errorf("video canon: %s", clip.CanonRel)
	}

	// report
	md, _ := os.ReadFile(filepath.Join(work, "r.md"))
	for _, w := range []string{"## By kind", "Relazione Q1", "really-png.dat.gif"} {
		if !strings.Contains(string(md), w) {
			t.Errorf("report.md missing %q", w)
		}
	}
	var rep map[string]any
	jb, _ := os.ReadFile(filepath.Join(work, "r.json"))
	must(t, json.Unmarshal(jb, &rep))

	// dry-run
	out = dd(t, "apply", "-with-stats", pp)
	if !strings.Contains(out, "DRY-RUN") || !strings.Contains(out, "from source nas") || !strings.Contains(out, "from source pc") {
		t.Fatalf("dry-run: %s", out)
	}

	// execute per host, then re-run (idempotent)
	G := filepath.Join(work, "golden")
	dd(t, "apply", "-execute", "-target", G, "-only-host", "pc", pp)
	dd(t, "apply", "-execute", "-target", G, "-only-host", "nas", pp)
	g1 := snapshot(t, G)
	dd(t, "apply", "-execute", "-target", G, pp)
	if snapshot(t, G) != g1 {
		t.Fatal("second apply changed the golden tree")
	}
	for _, a := range acts {
		dst := filepath.Join(G, filepath.FromSlash(a.CanonRel))
		fi, err := os.Stat(dst)
		if err != nil {
			t.Fatalf("missing materialized %s", a.CanonRel)
		}
		if a.IsMediaAction() {
			continue
		}
		src, _ := os.ReadFile(a.SrcPath)
		cp, _ := os.ReadFile(dst)
		if !bytes.Equal(src, cp) {
			t.Errorf("generic copy differs: %s", a.CanonRel)
		}
		if want, _ := time.ParseInLocation("2006-01-02 15:04:05", a.Time, meta.Zone()); !fi.ModTime().Equal(want) {
			t.Errorf("mtime %s: %v want %v", a.CanonRel, fi.ModTime(), want)
		}
	}
	// media silvered: owner and signature of the config embedded in the photo copy
	tags, _ := exec.Command("exiftool", "-s3", "-Artist", "-Comment", filepath.Join(G, filepath.FromSlash(p1.CanonRel))).Output()
	if got := strings.ReplaceAll(strings.TrimSpace(string(tags)), "\r", ""); got != "E2E Owner\nE2E signature" {
		t.Errorf("photo not silvered with the config tags: %q", got)
	}

	// export-text
	T := filepath.Join(work, "text")
	out = dd(t, "export-text", "-target", T, pp)
	if !strings.Contains(out, "0 errors") {
		t.Fatalf("export-text: %s", out)
	}
	side, err := os.ReadFile(filepath.Join(T, "DOCUMENTS/DOC/2021/Mario Rossi/Relazione Q1.docx.txt"))
	if err != nil {
		// the pc docx may be the _01 one depending on hash order
		side, err = os.ReadFile(filepath.Join(T, "DOCUMENTS/DOC/2021/Mario Rossi/Relazione Q1_01.docx.txt"))
	}
	must(t, err)
	if !strings.Contains(string(side), "Title: Relazione Q1") {
		t.Errorf("sidecar: %s", side)
	}
	if _, err := os.Stat(filepath.Join(T, "PICTURES")); !os.IsNotExist(err) {
		t.Error("media must not get text sidecars")
	}

	// Sources untouched by the whole flow
	if snapshot(t, root) != before {
		t.Fatal("source trees modified")
	}
}

func canonPaths(recs []catalog.Record) string {
	var ps []string
	for _, r := range recs {
		ps = append(ps, filepath.ToSlash(r.Path)) // manifests keep the OS separator
	}
	sort.Strings(ps)
	return strings.Join(ps, "\n")
}

func canon(recs []catalog.Record) string {
	var lines []string
	for _, r := range recs {
		b, _ := json.Marshal(r)
		lines = append(lines, string(b))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// TestCLIValidation: bad flags fail fast with a non-zero exit.
func TestCLIValidation(t *testing.T) {
	for _, args := range [][]string{
		{"catalog", "-preset", "nope", testfix.TempDir(t)},
		{"catalog", "-category", "bogus", testfix.TempDir(t)},
		{"catalog", "-min-size", "12Q", testfix.TempDir(t)},
		{"plan", "-layout", "bogus", "x.jsonl"},
		{"apply", "-execute", "missing.jsonl"},
		{"export-text", "plan.jsonl"},
	} {
		if err := exec.Command(bin, args...).Run(); err == nil {
			t.Errorf("%v: expected failure", args)
		}
	}
	// naming regimes: refused without -allow-mixed, exit 2, nothing written
	in, out := testfix.TempDir(t), filepath.Join(testfix.TempDir(t), "o")
	for _, args := range [][]string{
		{"catalog", "-preset", "all", "-o", filepath.Join(out, "m.jsonl"), in},
		{"catalog", "-preset", "media,document", "-o", filepath.Join(out, "m.jsonl"), in},
		{"merge", "-input", in, "-output", out, "-preset", "media"},
		{"merge", "-input", in, "-output", out, "-layout", "music"},
		{"merge", "-input", in, "-output", out, "-naming", "library-files"},
		{"rearrange", "-preset", "image,MEDIA", in},
		{"rearrange", "-naming", "library", in},
	} {
		var stderr bytes.Buffer
		cmd := exec.Command(bin, args...)
		cmd.Stderr = &stderr
		err := cmd.Run()
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 2 || !strings.Contains(stderr.String(), "-allow-mixed") {
			t.Errorf("%v: want exit 2 with the -allow-mixed hint, got %v %s", args, err, stderr.String())
		}
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("a refused command wrote its output")
	}
	for _, args := range [][]string{
		{"catalog", "-preset", "video", "-o", filepath.Join(testfix.TempDir(t), "m.jsonl"), in},
		{"merge", "-input", in, "-output", out, "-preset", "image,video", "-naming", "music"},
		{"merge", "-input", in, "-output", out, "-preset", "media", "-layout", "dedupe", "-allow-mixed"},
	} {
		if b, err := exec.Command(bin, args...).CombinedOutput(); err != nil {
			t.Errorf("%v: %v %s", args, err, b)
		}
	}
	var b bytes.Buffer
	cmd := exec.Command(bin, "help")
	cmd.Stderr = &b
	cmd.Run()
	for _, w := range []string{"catalog", "plan", "apply", "export-text", "push", "-no-sweep", "-verify-full", "-layout"} {
		if !strings.Contains(b.String(), w) {
			t.Errorf("usage missing %q", w)
		}
	}
}

// TestCorpusFlow runs the whole CLI on the real-file corpus (testdata/fixtures).
func TestCorpusFlow(t *testing.T) {
	corpus := filepath.Join("..", "..", "testdata", "fixtures")
	if _, err := os.Stat(corpus); err != nil {
		t.Skip("testdata/fixtures missing")
	}
	before := snapshot(t, corpus)
	w := testfix.TempDir(t)
	m, p, g, x := filepath.Join(w, "m.jsonl"), filepath.Join(w, "p.jsonl"), filepath.Join(w, "golden"), filepath.Join(w, "text")
	out := dd(t, "catalog", "-preset", "all", "-allow-mixed", "-host", "fx", "-o", m, corpus)
	if !strings.Contains(out, "assets:       39") {
		t.Fatalf("catalog: %s", out)
	}
	out = dd(t, "plan", "-o", p, "-layout", "document", "-verify-full", "-report", filepath.Join(w, "r.md"), m)
	if !strings.Contains(out, "exact-dupe 1 ") || !strings.Contains(out, "GOLDEN assets: 38") {
		t.Fatalf("plan: %s", out)
	}
	acts := loadPlan(t, p)
	kept := map[string]bool{}
	for _, a := range acts {
		kept[filepath.Base(a.SrcPath)] = true
	}
	if !kept["wrong_extension.jpg"] || !kept["web_export.png"] {
		t.Error("mislabeled .jpg and the real .png (same bytes) must both be kept")
	}
	if kept["photo_original.jpg"] == kept["photo_copy.jpg"] {
		t.Error("identical photo pair must collapse to one")
	}
	// without exiftool/ffmpeg apply refuses to start (copies would stay
	// unsilvered for good) unless -allow-unstamped
	noTools := exec.Command(bin, "apply", "-execute", "-target", g, p)
	noTools.Env = append(os.Environ(), "ONEFOLD_NO_EXTERNAL=1")
	if b, err := noTools.CombinedOutput(); err == nil || !strings.Contains(string(b), "-allow-unstamped") {
		t.Fatalf("apply without tools: %v\n%s", err, b)
	}
	if _, err := os.Stat(g); !os.IsNotExist(err) {
		t.Fatalf("refused apply wrote the target: %v", err)
	}
	dd(t, "apply", "-execute", "-allow-unstamped", "-target", g, p)
	g1 := snapshot(t, g)
	dd(t, "apply", "-execute", "-allow-unstamped", "-target", g, p)
	if snapshot(t, g) != g1 {
		t.Fatal("re-apply changed the golden tree")
	}
	out = dd(t, "export-text", "-target", x, p)
	if !strings.Contains(out, "12 eligible") || !strings.Contains(out, "0 errors") {
		t.Fatalf("export-text: %s", out)
	}
	// year dirs come from the checkout mtime: locate sidecars by glob
	for name, wantText := range map[string]string{
		"XLS/*/Unknown/sample.xlsx.txt": "Name\tAge\tCity\tCountry\nJohn Doe\t30\tRome\tItaly",
		"XLS/*/Unknown/sample.ods.txt":  "Name\tAge\tCity\tCountry\nJohn Doe\t30\tRome\tItaly",
		"DOC/*/Unknown/sample.docx.txt": "This is a sample document for testing RearrAngel.",
		"DOC/*/Unknown/sample.rtf.txt":  "This is a sample RTF document for testing.",
	} {
		hits, _ := filepath.Glob(filepath.Join(x, "DOCUMENTS", name))
		if len(hits) != 1 {
			t.Errorf("%s: %d sidecars", name, len(hits))
			continue
		}
		if b, _ := os.ReadFile(hits[0]); !strings.Contains(string(b), wantText) {
			t.Errorf("%s:\n%s", name, b)
		}
	}
	if snapshot(t, corpus) != before {
		t.Fatal("corpus modified")
	}
}

// TestMergeCLI: the <EXT>/<YEAR> tree as output and input, dry-run, execute, idempotent re-run.
func TestMergeCLI(t *testing.T) {
	root := testfix.TempDir(t)
	O, B := filepath.Join(root, "FILES"), filepath.Join(root, "B")
	old := time.Date(2012, 1, 2, 3, 4, 5, 0, time.Local)
	must(t, testfix.File(filepath.Join(O, "TXT", "2012", "a.txt"), "alpha", old))
	must(t, testfix.File(filepath.Join(B, "copy-of-a.txt"), "alpha", old))
	must(t, testfix.File(filepath.Join(B, "sub", "b.txt"), "beta", old))
	must(t, testfix.Docx(filepath.Join(B, "r.docx"), "T", "A", time.Date(2019, 5, 5, 5, 5, 5, 0, time.UTC), 1, "x"))
	before := snapshot(t, B)

	out := dd(t, "-input", O, "-input", B, "-output", O)
	if !strings.Contains(out, "DRY-RUN") || !strings.Contains(out, "to copy:           2 files") || !strings.Contains(out, "already in output: 1") {
		t.Fatalf("dry-run: %s", out)
	}
	if _, err := os.Stat(filepath.Join(O, "TXT", "2012", "b.txt")); !os.IsNotExist(err) {
		t.Fatal("dry-run wrote")
	}
	out = dd(t, "merge", "-input", O, "-input", B, "-output", O, "-execute", "-perf")
	if !strings.Contains(out, "missing after run: 0") {
		t.Fatalf("execute: %s", out)
	}
	for _, rel := range []string{"TXT/2012/b.txt", "DOCX/2019/r.docx"} {
		if _, err := os.Stat(filepath.Join(O, filepath.FromSlash(rel))); err != nil {
			t.Errorf("missing %s", rel)
		}
	}
	out = dd(t, "-input", O, "-input", B, "-output", O, "-execute")
	if !strings.Contains(out, "to copy:           0 files") {
		t.Fatalf("second run: %s", out)
	}
	if snapshot(t, B) != before {
		t.Fatal("input modified")
	}
	if err := exec.Command(bin, "-input", B).Run(); err == nil {
		t.Fatal("merge without -output must fail")
	}
}

// TestRearrangeCLI: dry-run, execute, idempotent re-run, undo via the journal.
func TestRearrangeCLI(t *testing.T) {
	root := filepath.Join(testfix.TempDir(t), "FILES")
	must(t, testfix.File(filepath.Join(root, "TXT", "2020", "[Desktop] note -- frev.1.txt"), "note", time.Date(2019, 5, 5, 5, 5, 5, 0, time.Local)))
	must(t, testfix.File(filepath.Join(root, "MP4", "clip.mp4"), "clip", time.Date(2018, 1, 1, 1, 1, 1, 0, time.Local)))
	must(t, testfix.File(filepath.Join(root, "OLD", "clip copy.mp4"), "clip", time.Date(2020, 1, 1, 1, 1, 1, 0, time.Local)))
	before := snapshot(t, root)
	out := dd(t, "rearrange", root)
	if !strings.Contains(out, "DRY-RUN") || !strings.Contains(out, "to move/rename:    2") || !strings.Contains(out, "1 files to delete") {
		t.Fatalf("dry-run: %s", out)
	}
	if snapshot(t, root) != before {
		t.Fatal("dry-run touched the tree")
	}
	out = dd(t, "rearrange", "-execute", "-list", root)
	for _, rel := range []string{"TXT/2019/[Desktop] note.txt", "MP4/2018/clip.mp4"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Errorf("missing %s:\n%s", rel, out)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "OLD")); !os.IsNotExist(err) {
		t.Error("duplicate not deleted / empty dir not removed")
	}
	journal := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "journal:") {
			journal = strings.Fields(l)[1]
		}
	}
	out = dd(t, "rearrange", "-execute", root)
	if !strings.Contains(out, "to move/rename:    0") {
		t.Fatalf("second run: %s", out)
	}
	out = dd(t, "rearrange", "-undo", journal)
	if !strings.Contains(out, "undo: 2 files moved, 0 duplicates deleted, 0 empty dirs removed, 0 failed") {
		t.Fatalf("undo: %s", out)
	}
	if _, err := os.Stat(filepath.Join(root, "TXT", "2020", "[Desktop] note -- frev.1.txt")); err != nil {
		t.Fatal("undo did not restore the original name")
	}
	if out := dd(t, "naming"); !strings.Contains(out, "photo-month") || !strings.Contains(out, "{month-name-it}") {
		t.Fatalf("naming listing: %s", out)
	}
}

// TestDesktopArchiveCLI: families with the [folder] prefix, skip rules by
// name, a settings file, the GUI's command (settings.Args) read back by
// -print-settings, then purge-sources.
func TestDesktopArchiveCLI(t *testing.T) {
	root := testfix.TempDir(t)
	desk, O := filepath.Join(root, "Desktop"), filepath.Join(root, "OUT")
	mt := time.Date(2021, 4, 5, 6, 7, 8, 0, time.Local)
	for f, body := range map[string]string{"desktop.ini": "[.ShellClassInfo]", "Chrome.lnk": "L", "Sito.url": "[InternetShortcut]",
		"~$Relazione.docx": "owner", "x.tmp": "tmp", "Relazione.docx": "PK report", "nota.txt": "a note",
		"Progetti/Alfa/spec.pdf": "%PDF-1.4 spec", "Progetti/Alfa/desktop.ini": "[.ShellClassInfo]", "foo.qqzz": "unknown", "noext": "none"} {
		must(t, testfix.File(filepath.Join(desk, filepath.FromSlash(f)), body, mt))
	}
	out := dd(t, "merge", "-input", desk, "-output", O, "-name", "all=ext-year+prefix", "-name", "doc=ext-year+prefix:2", "-list")
	for _, want := range []string{"to copy:           5 files", "excluded:          junk 2, shortcuts 2, transient 2",
		// documents (doc, txt, pdf) at level 2, everything else at level 1
		"[" + filepath.Base(root) + "] nota.txt", "[Progetti] spec.pdf", "[" + filepath.Base(root) + "] Relazione.docx",
		"QQZZ/2021/[Desktop] foo.qqzz", "NOEXT/2021/[Desktop] noext"} {
		if !strings.Contains(out, want) {
			t.Fatalf("%q missing:\n%s", want, out)
		}
	}
	// exclusions by path and pattern
	out = dd(t, "merge", "-input", desk, "-output", O, "-name", "all=ext-year", "-exclude-path", filepath.Join(desk, "Progetti"),
		"-exclude-glob", "*.qqzz", "-list")
	if !strings.Contains(out, "to copy:           3 files") || !strings.Contains(out, "EXCL\texclude-path\t"+filepath.Join(desk, "Progetti")) ||
		!strings.Contains(out, "EXCL\texclude-glob\t"+filepath.Join(desk, "foo.qqzz")) {
		t.Fatalf("-exclude-path/-exclude-glob:\n%s", out)
	}
	// -keep shortcuts: the shortcuts are copied too; -skip unknown-ext drops foo.qqzz
	out = dd(t, "merge", "-input", desk, "-output", O, "-name", "all=ext-year", "-keep", "shortcuts", "-skip", "unknown-ext")
	if !strings.Contains(out, "to copy:           6 files") || !strings.Contains(out, "unknown-ext 1") {
		t.Fatalf("-keep/-skip:\n%s", out)
	}
	// a settings file, overridden by a flag; -print-settings shows the result
	s := filepath.Join(root, "s.json")
	must(t, os.WriteFile(s, []byte(`{"kind":"merge","inputs":["`+jsonEsc(desk)+`"],"output":"`+jsonEsc(O)+`",
		"families":{"doc":{"preset":"documents","stem":"raw"},"rest":{"preset":"ext-flat"}},"filters":{"rules":{"transient":false}}}`), 0o644))
	var got map[string]any
	must(t, json.Unmarshal([]byte(dd(t, "merge", "-settings", s, "-keep", "junk", "-print-settings")), &got))
	rules := got["filters"].(map[string]any)["rules"].(map[string]any)
	if rules["transient"] != false || rules["junk"] != false || got["families"].(map[string]any)["doc"].(map[string]any)["stem"] != "raw" {
		t.Fatalf("print-settings: %v", got)
	}
	if b, err := exec.Command(bin, "rearrange", "-settings", s, root).CombinedOutput(); err == nil || !strings.Contains(string(b), "not rearrange") {
		t.Fatalf("a merge settings file used by rearrange: %v %s", err, b)
	}
	// execute with a report (transient files kept by the settings), then purge the sources
	report := filepath.Join(root, "r.json")
	out = dd(t, "merge", "-settings", s, "-execute", "-report", report)
	if !strings.Contains(out, "settled:           7 input files") {
		t.Fatalf("execute:\n%s", out)
	}
	out = dd(t, "purge-sources", "-from", report)
	if !strings.Contains(out, "to delete:         7 files") {
		t.Fatalf("purge dry run:\n%s", out)
	}
	if b, err := exec.Command(bin, "purge-sources", "-from", report, "-execute").CombinedOutput(); err == nil || !strings.Contains(string(b), "-expect") {
		t.Fatalf("execute without -expect: %v %s", err, b)
	}
	if err := exec.Command(bin, "purge-sources", "-from", report, "-execute", "-expect", "6").Run(); err == nil {
		t.Fatal("a wrong -expect was accepted")
	}
	if _, err := os.Stat(filepath.Join(desk, "nota.txt")); err != nil {
		t.Fatal("a refused purge deleted")
	}
	out = dd(t, "purge-sources", "-from", report, "-execute", "-expect", "7")
	if !strings.Contains(out, "deleted:           7 files") || !strings.Contains(out, "empty dirs:        2 removed (1 OS droppings") {
		t.Fatalf("purge:\n%s", out)
	}
	left, _ := os.ReadDir(desk)
	var names []string
	for _, e := range left {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if strings.Join(names, "|") != "Chrome.lnk|Sito.url|desktop.ini" {
		t.Fatalf("left in the desktop: %v", names)
	}
}

func jsonEsc(p string) string { return strings.ReplaceAll(p, `\`, `\\`) }

// The GUI's "copy command" (settings.Args) read back by the CLI gives the same
// settings.
func TestSettingsArgsRoundTrip(t *testing.T) {
	d := testfix.TempDir(t)
	m := settings.Merge{Kind: "merge", Inputs: []string{filepath.Join(d, "a b"), filepath.Join(d, "c")}, Output: filepath.Join(d, "o"),
		Filters: catalog.Filters{MaxSize: "2G", Exclude: "Backup old,.trash", Rules: map[string]bool{"shortcuts": false, "junk": false},
			ExcludePaths: []string{filepath.Join(d, "a b", "DRIVERS")}, ExcludeGlobs: []string{"*.data", "**/node cache/**"}},
		NoFsync: true, Workers: 3}
	m.Families = map[string]naming.Choice{"img": {Preset: "photo-timestamp", Stem: "prefix", Level: 3}, "doc": {Preset: "documents", Stem: "raw"},
		"rest": {Preset: "ext-year", Stem: "clean"}}
	r := settings.Rearrange{Kind: "rearrange", Root: filepath.Join(d, "r"), ReportDupes: true, KeepEmptyDirs: true,
		Filters: catalog.Filters{Rules: map[string]bool{"transient": false}}}
	r.Template = "{EXT}/{stem}<.{ext}>"
	r.Families = map[string]naming.Choice{"music": {Preset: "music"}}
	for _, c := range []struct {
		args []string
		want any
	}{{m.Args(), m}, {r.Args(), r}} {
		got := dd(t, append([]string{c.args[0], "-print-settings"}, c.args[1:]...)...)
		want, _ := json.MarshalIndent(c.want, "", "  ")
		if strings.TrimSpace(got) != string(want) {
			t.Errorf("%v:\n got  %s\n want %s", c.args, got, want)
		}
	}
}

// Package folders are flagged with the flags that leave them out.
func TestPackagesCLI(t *testing.T) {
	root := testfix.TempDir(t)
	in, O := filepath.Join(root, "IN"), filepath.Join(root, "OUT")
	mt := time.Date(2021, 4, 5, 6, 7, 8, 0, time.Local)
	for _, f := range []string{"COMP1/DRIVERS/Display/nv.inf", "COMP1/DRIVERS/Display/nv.sys", "COMP1/DRIVERS/Display/license.txt", "COMP1/notes.txt"} {
		must(t, testfix.File(filepath.Join(in, filepath.FromSlash(f)), "content "+f, mt))
	}
	drv := filepath.Join(in, "COMP1", "DRIVERS", "Display")
	out := dd(t, "merge", "-input", in, "-output", O, "-list")
	if !strings.Contains(out, "packages:          1 folders look like driver (1)") || !strings.Contains(out, "-exclude-path '"+drv+"'") ||
		!strings.Contains(out, "PKG\tdriver\t"+drv+"\t1") {
		t.Fatalf("packages:\n%s", out)
	}
	out = dd(t, "merge", "-input", in, "-output", O, "-exclude-path", drv)
	if strings.Contains(out, "packages:") || !strings.Contains(out, "to copy:           1 files") {
		t.Fatalf("after the exclusion:\n%s", out)
	}
}

// TestGoldenDeltaCLI: the golden tree is built from room A; room B arrives
// later. golden-index + match plan only what the tree does not hold, apply
// adds it, and nothing already in the tree changes.
func TestGoldenDeltaCLI(t *testing.T) {
	corpus := filepath.Join("..", "..", "testdata", "media-synth")
	w := testfix.TempDir(t)
	G := filepath.Join(w, "golden")
	mA, mB, pA := filepath.Join(w, "a.jsonl"), filepath.Join(w, "b.jsonl"), filepath.Join(w, "pa.jsonl")
	ix, sub, rep := filepath.Join(w, "index.jsonl"), filepath.Join(w, "sub.jsonl"), filepath.Join(w, "match.json")
	dd(t, "catalog", "-host", "pc", "-o", mA, filepath.Join(corpus, "A"))
	dd(t, "plan", "-o", pA, mA)
	dd(t, "apply", "-execute", "-allow-unstamped", "-target", G, pA)
	before := snapshot(t, G)
	// the look-alike review lists, never changes: the synthetic photos are plain colours
	if out := dd(t, "similar", "-o", filepath.Join(w, "similar.json"), pA); !strings.Contains(out, "plain (not compared) 9") {
		t.Fatalf("similar: %s", out)
	}
	out := dd(t, "golden-index", "-o", ix, "-manifest", mA, pA)
	if !strings.Contains(out, "assets 12 ") {
		t.Fatalf("golden-index: %s", out)
	}
	dd(t, "catalog", "-host", "pc", "-o", mB, filepath.Join(corpus, "B"))
	out = dd(t, "match", "-index", ix, "-o", sub, "-report", rep, mB)
	if !strings.Contains(out, "new:          7 ") || !strings.Contains(out, "in the tree:  5") {
		t.Fatalf("match: %s", out)
	}
	dd(t, "apply", "-execute", "-allow-unstamped", "-target", G, sub)
	after := snapshot(t, G)
	for _, l := range strings.Split(before, "\n") {
		if !strings.Contains(after, l) {
			t.Fatalf("a golden file changed: %s", l)
		}
	}
	if got, want := strings.Count(after, "\n")-strings.Count(before, "\n"), 7; got != want {
		t.Fatalf("added %d files, want %d", got, want)
	}
	// reclaim on the plan of both rooms: the exact copy in B is verified against
	// its winner in A (golden copy present), the variants are for review;
	// the command deletes nothing, the script does
	pAB, script := filepath.Join(w, "pab.jsonl"), filepath.Join(w, "reclaim.sh")
	dd(t, "plan", "-o", pAB, mA, mB)
	out = dd(t, "reclaim", "-target", G, "-script", script, "-o", filepath.Join(w, "reclaim.json"), pAB)
	if !strings.Contains(out, "exact duplicates verified now: 1 ") || !strings.Contains(out, "never in the script): 4") {
		t.Fatalf("reclaim: %s", out)
	}
	if _, err := os.Stat(filepath.Join(corpus, "B", "IMG_0001 (1).jpg")); err != nil {
		t.Fatal("reclaim deleted a file")
	}
	// cold storage: copy the golden, audit it, find a flipped byte
	cold := filepath.Join(w, "cold")
	dd(t, "backup", "-execute", G, cold)
	if out := dd(t, "backup", "-check", cold); !strings.Contains(out, "manifests 1; files ok ") {
		t.Fatalf("backup -check: %s", out)
	}
	var victim string
	filepath.WalkDir(cold, func(p string, d fs.DirEntry, err error) error {
		if err == nil && victim == "" && d.Type().IsRegular() && !strings.Contains(p, ".onefold") {
			victim = p
		}
		return nil
	})
	b, _ := os.ReadFile(victim)
	b[len(b)/2] ^= 0xff
	os.WriteFile(victim, b, 0o644)
	if out, err := exec.Command(bin, "backup", "-check", cold).CombinedOutput(); err == nil || !strings.Contains(string(out), "CHANGED (bit rot or edited): 1") {
		t.Fatalf("backup -check after damage: %v\n%s", err, out)
	}
	// bulk seed script of the golden of room A: every action of the plan is in the staging tree
	if out := dd(t, "seed", "-from", G, "-to", "user@nas:/golden", "-o", filepath.Join(w, "seed.sh"), pA); !strings.Contains(out, ": 12 files,") {
		t.Fatalf("seed: %s", out)
	}
	if b, _ := os.ReadFile(filepath.Join(w, "seed.sh-files")); strings.Count(string(b), "\n") != 12 {
		t.Fatalf("seed file list:\n%s", b)
	}
	// the sub-plan applied again adds nothing
	dd(t, "apply", "-execute", "-allow-unstamped", "-target", G, sub)
	if snapshot(t, G) != after {
		t.Fatal("re-apply of the sub-plan changed the tree")
	}
}

// TestApplyLinkCLI: apply -execute -link publishes hard links of the winners
// (same file as the source, original bytes, source mtime untouched), needs no
// external tool, and a re-run changes nothing.
func TestApplyLinkCLI(t *testing.T) {
	root := testfix.TempDir(t)
	src, G := filepath.Join(root, "src"), filepath.Join(root, "golden")
	old := time.Date(2012, 1, 2, 3, 4, 5, 0, time.Local)
	writeJPEG(t, filepath.Join(src, "IMG_20120102_030405.jpg"), 7)
	must(t, os.Chtimes(filepath.Join(src, "IMG_20120102_030405.jpg"), old, old))
	must(t, testfix.File(filepath.Join(src, "notes.txt"), "alpha", old))
	m, p := filepath.Join(root, "m.jsonl"), filepath.Join(root, "p.jsonl")
	dd(t, "catalog", "-preset", "all", "-allow-mixed", "-host", "pc", "-o", m, src)
	dd(t, "plan", "-o", p, "-layout", "document", m)
	before := snapshot(t, src)

	if out := dd(t, "apply", "-link", p); !strings.Contains(out, "link[original bytes") {
		t.Fatalf("dry-run -link: %s", out)
	}
	cmd := exec.Command(bin, "apply", "-execute", "-link", "-target", G, p)
	cmd.Env = append(os.Environ(), "ONEFOLD_NO_EXTERNAL=1") // links need no tool
	if b, err := cmd.CombinedOutput(); err != nil || !strings.Contains(string(b), "written 2,") {
		t.Fatalf("apply -link: %v\n%s", err, b)
	}
	acts := loadPlan(t, p)
	if len(acts) != 2 {
		t.Fatalf("plan: %d actions", len(acts))
	}
	for _, a := range acts {
		si, err := os.Stat(a.SrcPath)
		must(t, err)
		gi, err := os.Stat(filepath.Join(G, filepath.FromSlash(a.CanonRel)))
		must(t, err)
		if !os.SameFile(si, gi) {
			t.Errorf("%s is not a hard link of %s", a.CanonRel, a.SrcPath)
		}
	}
	if out := dd(t, "apply", "-execute", "-link", "-target", G, p); !strings.Contains(out, "written 0, already present 2") {
		t.Fatalf("second apply -link: %s", out)
	}
	if snapshot(t, src) != before {
		t.Fatal("apply -link modified a source")
	}
}

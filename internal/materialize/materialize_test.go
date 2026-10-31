// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package materialize

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mauriziofonte/onefold/internal/fsx"
	"github.com/mauriziofonte/onefold/internal/meta"
	"github.com/mauriziofonte/onefold/internal/plan"
	"github.com/mauriziofonte/onefold/internal/tools"
)

// Generic kinds = byte-identical copy + mtime, idempotent, source untouched.
func TestGenericCopy(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "src", "a.docx")
	os.MkdirAll(filepath.Dir(src), 0o755)
	body := []byte("PK\x03\x04 pretend docx")
	os.WriteFile(src, body, 0o644)
	old := time.Date(2001, 1, 1, 0, 0, 0, 0, time.Local)
	os.Chtimes(src, old, old)

	a := plan.Action{ID: "x", Kind: "document", SrcPath: src, SrcSize: int64(len(body)),
		CanonRel: "FILES/DOCX/2019/a.docx", Time: "2019-05-06 07:08:09"}
	target := filepath.Join(d, "golden")
	res, err := Apply(a, target, nil)
	if err != nil || res.Skipped || res.Unsilvered != "" {
		t.Fatalf("apply: %v %+v", err, res)
	}
	got, _ := os.ReadFile(res.Dst)
	if !bytes.Equal(got, body) {
		t.Fatal("generic copy is not byte-identical")
	}
	fi, _ := os.Stat(res.Dst)
	want := time.Date(2019, 5, 6, 7, 8, 9, 0, meta.Zone())
	if !fi.ModTime().Equal(want) {
		t.Fatalf("mtime %v want %v (canonical zone, whatever the host zone)", fi.ModTime(), want)
	}
	noTemps(t, filepath.Dir(res.Dst))
	again, err := Apply(a, target, nil)
	if err != nil || !again.Skipped {
		t.Fatalf("second apply must skip: %v %+v", err, again)
	}
	sfi, _ := os.Stat(src)
	sb, _ := os.ReadFile(src)
	if !sfi.ModTime().Equal(old) || !bytes.Equal(sb, body) {
		t.Fatal("source modified")
	}
}

// A temp left by an interrupted run is rebuilt from scratch, never published as is.
func TestStaleTempRebuilt(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "a.txt")
	os.WriteFile(src, []byte("the real content"), 0o644)
	a := plan.Action{ID: "x", Kind: "text", SrcPath: src, CanonRel: "FILES/TXT/2019/a.txt", Time: "2019-05-06 07:08:09"}
	target := filepath.Join(d, "golden")
	dst := filepath.Join(target, filepath.FromSlash(a.CanonRel))
	os.MkdirAll(filepath.Dir(dst), 0o755)
	os.WriteFile(fsx.TempFor(dst, false), []byte("half-written garbage from a crash, longer than the source"), 0o644)
	if _, err := Apply(a, target, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "the real content" {
		t.Fatalf("published %q", got)
	}
	noTemps(t, filepath.Dir(dst))
}

// Media without the tools: faithful copies, flagged unsilvered with the reason.
func TestUnsilveredWithoutTools(t *testing.T) {
	t.Setenv("ONEFOLD_NO_EXTERNAL", "1")
	d := t.TempDir()
	photo := writeJPEG(t, filepath.Join(d, "p.jpg"))
	video := filepath.Join(d, "v.mp4")
	os.WriteFile(video, []byte("not really a video"), 0o644)
	target := filepath.Join(d, "golden")
	for _, c := range []struct {
		a    plan.Action
		want string
	}{
		{plan.Action{Kind: "photo", SrcPath: photo, CanonRel: "PICTURES/JPG/2019/IMG_1.jpg", Time: "2019-05-06 07:08:09"}, NoExiftool},
		{plan.Action{Kind: "video", SrcPath: video, CanonRel: "VIDEOS/2019/VID_1.mp4", Time: "2019-05-06 07:08:09"}, NoFfmpeg},
	} {
		res, err := Apply(c.a, target, nil)
		if err != nil || res.Unsilvered != c.want {
			t.Fatalf("%s: %v %+v", c.a.Kind, err, res)
		}
		src, _ := os.ReadFile(c.a.SrcPath)
		got, _ := os.ReadFile(res.Dst)
		if !bytes.Equal(src, got) {
			t.Fatalf("%s: not a faithful copy", c.a.Kind)
		}
		noTemps(t, filepath.Dir(res.Dst))
	}
	st := ApplyAll([]plan.Action{{Kind: "photo", SrcPath: photo, CanonRel: "PICTURES/JPG/2019/IMG_2.jpg", Time: "2019-05-06 07:08:09"}},
		target, RunOptions{Workers: 1})
	if st.Written != 1 || st.Unsilvered != 1 {
		t.Fatalf("stats %+v", st)
	}
}

// With exiftool: a photo is stamped before it gets its final name; a file
// exiftool refuses is still published (faithful copy) and flagged, in the
// batch and in the one-shot path, and the batch keeps working after it.
func TestPhotoStamping(t *testing.T) {
	if !tools.Has("exiftool") {
		t.Skip("exiftool not available")
	}
	d := t.TempDir()
	good := writeJPEG(t, filepath.Join(d, "good.jpg"))
	bad := filepath.Join(d, "bad.jpg")
	os.WriteFile(bad, []byte("not a jpeg at all"), 0o644)
	target := filepath.Join(d, "golden Nicolò Città") // non-ASCII path through the argument-file batch
	act := func(src, rel string) plan.Action {
		return plan.Action{Kind: "photo", SrcPath: src, CanonRel: rel, Time: "2019-05-06 07:08:09", Artist: "Test Artist"}
	}
	batch, err := NewExifBatch()
	if err != nil {
		t.Fatal(err)
	}
	defer batch.Close()
	for i, b := range []*ExifBatch{batch, nil} {
		sfx := []string{"b", "o"}[i]
		res, err := Apply(act(bad, "PICTURES/JPG/2019/BAD_"+sfx+".jpg"), target, b)
		if err != nil || !strings.HasPrefix(res.Unsilvered, "exiftool: ") {
			t.Fatalf("refused file (%s): %v %+v", sfx, err, res)
		}
		if got, _ := os.ReadFile(res.Dst); string(got) != "not a jpeg at all" {
			t.Fatalf("refused file (%s) not published as a faithful copy", sfx)
		}
		os.WriteFile(filepath.Join(target, "PICTURES", "JPG", "2019", ".GOOD_"+sfx+".jpg.onefold-tmp_exiftool_tmp"), []byte("left by a killed run"), 0o644)
		res, err = ApplyWith(act(good, "PICTURES/JPG/2019/GOOD_"+sfx+".jpg"), target, b, Silver{Signature: "Test signature"})
		if err != nil || res.Unsilvered != "" {
			t.Fatalf("good file (%s): %v %+v", sfx, err, res)
		}
		out, _ := tools.Command("exiftool", "-s3", "-DateTimeOriginal", "-Artist", "-Comment", res.Dst).Output()
		out = bytes.ReplaceAll(out, []byte("\r\n"), []byte("\n"))
		if want := "2019:05:06 07:08:09\nTest Artist\nTest signature\n"; string(out) != want {
			t.Fatalf("stamped tags (%s) = %q want %q", sfx, out, want)
		}
		noTemps(t, filepath.Dir(res.Dst))
	}
}

func TestBatchRefusalIsStampError(t *testing.T) {
	if !tools.Has("exiftool") {
		t.Skip("exiftool not available")
	}
	d := t.TempDir()
	bad := filepath.Join(d, "x.dat")
	os.WriteFile(bad, []byte("garbage"), 0o644)
	b, err := NewExifBatch()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	var se *StampError
	if err := b.Stamp([]string{"-overwrite_original", "-m", "-Comment=x", bad}); !errors.As(err, &se) || se.Msg == "" {
		t.Fatalf("want StampError, got %v", err)
	}
	if err := b.Stamp([]string{"-Comment=a\nb", bad}); err == nil || errors.As(err, &se) {
		t.Fatalf("line break: want a plain error (one-shot fallback), got %v", err)
	}
}

func writeJPEG(t *testing.T, p string) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 12))
	for i := range img.Pix {
		img.Pix[i] = uint8(i)
	}
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := jpeg.Encode(f, img, nil); err != nil {
		t.Fatal(err)
	}
	return p
}

func noTemps(t *testing.T, dir string) {
	t.Helper()
	es, _ := os.ReadDir(dir)
	for _, e := range es {
		if fsx.IsTemp(e.Name()) || strings.HasSuffix(e.Name(), ".part") {
			t.Fatalf("temp left behind: %s", e.Name())
		}
	}
}

// MissingTools names the silvering tools the remaining media actions need and
// do not find; byte-copied media, generic kinds and existing targets need none.
func TestMissingTools(t *testing.T) {
	t.Setenv("ONEFOLD_NO_EXTERNAL", "1")
	target := t.TempDir()
	photo := plan.Action{Kind: "photo", SrcPath: "/s/IMG_1.jpg", CanonRel: "PICTURES/JPG/2019/IMG_1.jpg"}
	video := plan.Action{Kind: "video", SrcPath: "/s/VID_1.mp4", CanonRel: "VIDEOS/2019/VID_1.mp4"}
	raw := plan.Action{Kind: "photo", SrcPath: "/s/DSC_1.nef", CanonRel: "PICTURES/NEF/2019/DSC_1.nef"}
	doc := plan.Action{Kind: "document", SrcPath: "/s/a.pdf", CanonRel: "FILES/PDF/2019/a.pdf"}
	got := func(acts ...plan.Action) string { return strings.Join(MissingTools(acts, target), ",") }
	if g := got(photo, video, raw, doc); g != "exiftool,ffmpeg" {
		t.Errorf("photo+video: %q", g)
	}
	if g := got(raw, doc); g != "" {
		t.Errorf("raw+generic: %q", g)
	}
	// the photo is already in the target: skipped by apply, nothing to stamp
	p := filepath.Join(target, filepath.FromSlash(photo.CanonRel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if g := got(photo, video); g != "ffmpeg" {
		t.Errorf("existing photo: %q", g)
	}
}

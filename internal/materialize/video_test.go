// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package materialize

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mauriziofonte/onefold/internal/meta"
	"github.com/mauriziofonte/onefold/internal/plan"
	"github.com/mauriziofonte/onefold/internal/tools"
)

// needVideoTools skips without ffmpeg (silvering) and ffprobe (reading the
// result back); CI installs both.
func needVideoTools(t *testing.T) {
	t.Helper()
	for _, n := range []string{"ffmpeg", "ffprobe"} {
		if !tools.Has(n) {
			t.Skipf("%s not installed", n)
		}
	}
}

// makeVideo writes a short clip with an audio track (lavfi sources).
func makeVideo(t *testing.T, p string) {
	t.Helper()
	ff, _ := tools.Path("ffmpeg")
	args := []string{"-loglevel", "error", "-f", "lavfi", "-i", "color=c=red:s=64x48:d=1:r=5",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-shortest"}
	if strings.HasSuffix(p, ".avi") {
		args = append(args, "-c:v", "mjpeg", "-c:a", "pcm_s16le")
	} else {
		args = append(args, "-c:v", "mpeg4", "-c:a", "aac") // native encoders: the pinned Windows ffmpeg has no libx264
	}
	if out, err := exec.Command(ff, append(args, p)...).CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v\n%s", err, out)
	}
}

type probed struct {
	Format struct {
		Tags map[string]string `json:"tags"`
	} `json:"format"`
	Streams []struct {
		CodecType string            `json:"codec_type"`
		Tags      map[string]string `json:"tags"`
	} `json:"streams"`
}

func probe(t *testing.T, p string) probed {
	t.Helper()
	fp, _ := tools.Path("ffprobe")
	out, err := exec.Command(fp, "-v", "error", "-show_entries", "format_tags:stream=codec_type:stream_tags", "-of", "json", p).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", p, err)
	}
	var r probed
	if err := json.Unmarshal(out, &r); err != nil {
		t.Fatal(err)
	}
	// tag names differ in case between containers
	low := func(m map[string]string) map[string]string {
		l := map[string]string{}
		for k, v := range m {
			l[strings.ToLower(k)] = v
		}
		return l
	}
	r.Format.Tags = low(r.Format.Tags)
	for i := range r.Streams {
		r.Streams[i].Tags = low(r.Streams[i].Tags)
	}
	return r
}

// A video is remuxed (streams copied) with the canonical time as explicit
// UTC, the owner, the comment and the stream language, gets the canonical
// mtime, leaves no temp and is skipped by a second run.
func TestVideoSilvering(t *testing.T) {
	needVideoTools(t)
	prev := meta.Zone()
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	meta.SetZone(ny) // the canonical time is a New York wall clock: creation_time must be its UTC instant
	defer meta.SetZone(prev)

	d := t.TempDir()
	src := filepath.Join(d, "clip.mp4")
	makeVideo(t, src)
	before, _ := os.ReadFile(src)
	a := plan.Action{Kind: "video", SrcPath: src, SrcSize: int64(len(before)), CanonRel: "VIDEOS/2019/VID_20190506_070809.mp4",
		Time: "2019-05-06 07:08:09", Artist: "Jane Doe"}
	target := filepath.Join(d, "golden")
	res, err := ApplyWith(a, target, nil, Silver{Signature: "My archive", Language: "ita"})
	if err != nil || res.Unsilvered != "" || res.Skipped {
		t.Fatalf("apply: %v %+v", err, res)
	}
	p := probe(t, res.Dst)
	if got := p.Format.Tags["creation_time"]; !strings.HasPrefix(got, "2019-05-06T11:08:09") {
		t.Errorf("creation_time %q, want the UTC instant 2019-05-06T11:08:09", got)
	}
	if p.Format.Tags["artist"] != "Jane Doe" || p.Format.Tags["comment"] != "My archive" {
		t.Errorf("format tags %v", p.Format.Tags)
	}
	kinds := map[string]bool{}
	for _, s := range p.Streams {
		kinds[s.CodecType] = true
		if s.Tags["language"] != "ita" {
			t.Errorf("%s stream language %q", s.CodecType, s.Tags["language"])
		}
	}
	if !kinds["video"] || !kinds["audio"] {
		t.Errorf("streams lost in the remux: %v", kinds)
	}
	if fi, err := os.Stat(res.Dst); err != nil || !fi.ModTime().Equal(time.Date(2019, 5, 6, 7, 8, 9, 0, ny)) {
		t.Errorf("mtime: %v %v", fi.ModTime(), err)
	}
	if after, _ := os.ReadFile(src); !bytes.Equal(before, after) {
		t.Fatal("source modified")
	}
	noTemps(t, filepath.Dir(res.Dst))
	if res, err := ApplyWith(a, target, nil, Silver{}); err != nil || !res.Skipped {
		t.Fatalf("second run: %v %+v", err, res)
	}
}

// Empty silver tags write no comment and no language; the owner travels in
// the plan and is written only when set.
func TestVideoSilveringEmptyTags(t *testing.T) {
	needVideoTools(t)
	d := t.TempDir()
	src := filepath.Join(d, "clip.mp4")
	makeVideo(t, src)
	res, err := ApplyWith(plan.Action{Kind: "video", SrcPath: src, CanonRel: "VIDEOS/2019/VID_1.mp4", Time: "2019-05-06 07:08:09"},
		filepath.Join(d, "golden"), nil, Silver{})
	if err != nil || res.Unsilvered != "" {
		t.Fatalf("apply: %v %+v", err, res)
	}
	p := probe(t, res.Dst)
	if _, ok := p.Format.Tags["comment"]; ok {
		t.Errorf("comment written: %v", p.Format.Tags)
	}
	if _, ok := p.Format.Tags["artist"]; ok {
		t.Errorf("artist written: %v", p.Format.Tags)
	}
	for _, s := range p.Streams {
		if l := s.Tags["language"]; l != "" && l != "und" {
			t.Errorf("%s stream language %q", s.CodecType, l)
		}
	}
}

// AVI has no creation_time slot: the remux keeps the container, writes the
// comment, and the date travels in the mtime.
func TestVideoSilveringAVI(t *testing.T) {
	needVideoTools(t)
	d := t.TempDir()
	src := filepath.Join(d, "clip.avi")
	makeVideo(t, src)
	res, err := ApplyWith(plan.Action{Kind: "video", SrcPath: src, CanonRel: "VIDEOS/2019/VID_1.avi", Time: "2019-05-06 07:08:09"},
		filepath.Join(d, "golden"), nil, Silver{Signature: "My archive"})
	if err != nil || res.Unsilvered != "" {
		t.Fatalf("apply: %v %+v", err, res)
	}
	if c := probe(t, res.Dst).Format.Tags["comment"]; c != "My archive" {
		t.Errorf("comment %q", c)
	}
	want := time.Date(2019, 5, 6, 7, 8, 9, 0, meta.Zone())
	if fi, err := os.Stat(res.Dst); err != nil || !fi.ModTime().Equal(want) {
		t.Errorf("mtime %v, want %v (%v)", fi.ModTime(), want, err)
	}
}

// A file ffmpeg refuses is published as a faithful copy, flagged with the
// ffmpeg reason, and no temp is left.
func TestVideoRefusedByFfmpeg(t *testing.T) {
	needVideoTools(t)
	d := t.TempDir()
	src := filepath.Join(d, "broken.mp4")
	if err := os.WriteFile(src, []byte("not a video at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ApplyWith(plan.Action{Kind: "video", SrcPath: src, CanonRel: "VIDEOS/2019/VID_2.mp4", Time: "2019-05-06 07:08:09"},
		filepath.Join(d, "golden"), nil, Silver{Signature: "x"})
	if err != nil || !strings.HasPrefix(res.Unsilvered, "ffmpeg: ") {
		t.Fatalf("refused video: %v %+v", err, res)
	}
	got, _ := os.ReadFile(res.Dst)
	if string(got) != "not a video at all" {
		t.Fatal("not a faithful copy")
	}
	noTemps(t, filepath.Dir(res.Dst))
}

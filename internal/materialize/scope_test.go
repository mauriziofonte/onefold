// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package materialize

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mauriziofonte/onefold/internal/plan"
	"github.com/mauriziofonte/onefold/internal/tools"
)

// Camera RAW and MPEG-PS are published byte-identical by design: no tool
// runs and the copy is not counted as unsilvered.
func TestByteCopyMedia(t *testing.T) {
	d := t.TempDir()
	for _, c := range []struct{ name, kind, rel string }{
		{"IMG_1234.CR2", "photo", "PICTURES/CR2/2019/DSC_20190809_230216.cr2"},
		{"DSC_0001.nef", "photo", "PICTURES/NEF/2019/DSC_20190809_230217.nef"},
		{"clip.mpg", "video", "VIDEOS/2019/VID_20190809_230218.mpg"},
	} {
		src := filepath.Join(d, c.name)
		body := []byte("raw payload of " + c.name)
		os.WriteFile(src, body, 0o644)
		a := plan.Action{Kind: c.kind, SrcPath: src, SrcSize: int64(len(body)), CanonRel: c.rel, Time: "2019-08-09 23:02:16", Artist: "Owner"}
		res, err := ApplyWith(a, filepath.Join(d, "golden"), nil, Silver{Signature: "sig", Language: "ita"})
		if err != nil || res.Unsilvered != "" || res.Skipped {
			t.Fatalf("%s: %v %+v", c.name, err, res)
		}
		if got, _ := os.ReadFile(res.Dst); !bytes.Equal(got, body) {
			t.Fatalf("%s: not byte-identical", c.name)
		}
	}
}

// The EXIF-based photo formats get the signature as UserComment, PNG as Comment.
func TestPhotoFormatsStamping(t *testing.T) {
	if !tools.Has("exiftool") {
		t.Skip("exiftool not available")
	}
	d := t.TempDir()
	for _, ext := range []string{"heic", "png", "webp", "tiff", "avif"} {
		src := filepath.Join("..", "meta", "testdata", "photo", "capture."+ext)
		a := plan.Action{Kind: "photo", SrcPath: src, CanonRel: "PICTURES/" + strings.ToUpper(ext) + "/2019/X." + ext,
			Time: "2019-05-06 07:08:09", Artist: "Test Artist"}
		res, err := ApplyWith(a, d, nil, Silver{Signature: "Test signature"})
		if err != nil || res.Unsilvered != "" {
			t.Fatalf("%s: %v %+v", ext, err, res)
		}
		tag := "-UserComment"
		if ext == "png" {
			tag = "-Comment"
		}
		out, _ := tools.Command("exiftool", "-s3", "-DateTimeOriginal", "-Artist", tag, res.Dst).Output()
		out = bytes.ReplaceAll(out, []byte("\r\n"), []byte("\n"))
		if want := "2019:05:06 07:08:09\nTest Artist\nTest signature\n"; string(out) != want {
			t.Errorf("%s: stamped tags %q want %q", ext, out, want)
		}
		noTemps(t, filepath.Dir(res.Dst))
	}
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import "testing"

// The photo formats of the golden scope yield kind photo, capture
// time with sub-second, device and size, with and without the external tools
// (testdata/photo: tiny synthetic images, EXIF written by exiftool/pillow-heif).
func TestPhotoFormats(t *testing.T) {
	type want struct {
		time   string
		subsec int64
		device string
		w, h   int
	}
	canon := want{"2019-08-09 23:02:16", 250000000, "Canon EOS 1000D", 64, 48}
	cases := map[string]want{
		"png": canon, "webp": canon, "tiff": canon, "avif": canon,
		"heic": {"2023-05-14 10:12:33", 457000000, "Apple iPhone 13", 64, 64}, // ispe: coded size
	}
	for _, noExt := range []string{"1", ""} {
		t.Setenv("ONEFOLD_NO_EXTERNAL", noExt)
		for ext, w := range cases {
			m, err := Extract("testdata/photo/capture." + ext)
			if err != nil || m.Kind != "photo" || m.Time != w.time || m.TimeSource != "metadata" || m.SubSecNs != w.subsec ||
				m.CanonDevice != w.device || m.Width != w.w || m.Height != w.h {
				t.Errorf("no-external=%q %s: %+v err=%v", noExt, ext, m, err)
			}
		}
	}
}

func TestSubSecNs(t *testing.T) {
	for in, want := range map[string]int64{"457": 457000000, "05": 50000000, "1": 100000000, "": 0, "x": 0, "1234567890": 0} {
		if got := subSecNs(in); got != want {
			t.Errorf("subSecNs(%q)=%d want %d", in, got, want)
		}
	}
}

func TestHeifDims(t *testing.T) {
	ispe := func(w, h uint32) []byte {
		return []byte{0, 0, 0, 20, 'i', 's', 'p', 'e', 0, 0, 0, 0,
			byte(w >> 24), byte(w >> 16), byte(w >> 8), byte(w), byte(h >> 24), byte(h >> 16), byte(h >> 8), byte(h)}
	}
	b := append(append(append([]byte("xxxx"), ispe(512, 512)...), ispe(4032, 3024)...), ispe(320, 240)...)
	if w, h := heifDims(b); w != 4032 || h != 3024 {
		t.Fatalf("heifDims %dx%d", w, h)
	}
	if w, h := heifDims([]byte("no boxes here")); w != 0 || h != 0 {
		t.Fatalf("heifDims empty %dx%d", w, h)
	}
}

// A JPEG without DateTimeOriginal: by default its CreateDate/ModifyDate are
// not used (the golden names of such photos stay as they were); with the
// fallback on, CreateDate then ModifyDate give the capture time.
func TestPhotoDateFallback(t *testing.T) {
	defer SetPhotoDateFallback(false)
	for _, c := range []struct{ file, want string }{
		{"createdate.jpg", "2018-03-04 05:06:07"},
		{"modifydate.jpg", "2017-02-03 04:05:06"},
	} {
		SetPhotoDateFallback(false)
		m, err := Extract("testdata/photo/" + c.file)
		if err != nil {
			t.Fatal(err)
		}
		if m.TimeSource == "metadata" {
			t.Errorf("%s without the fallback: %s from %s", c.file, m.Time, m.TimeSource)
		}
		SetPhotoDateFallback(true)
		if m, err = Extract("testdata/photo/" + c.file); err != nil {
			t.Fatal(err)
		}
		if m.Time != c.want || m.TimeSource != "metadata" {
			t.Errorf("%s with the fallback: %q from %s, want %s", c.file, m.Time, m.TimeSource, c.want)
		}
	}
}

func TestFilenameCounter(t *testing.T) {
	for in, want := range map[string]string{
		"IMG-20200101-WA0007.jpg": "WA0007", "VID-20200101-wa0012.mp4": "0012", "2020-01-01_0003.jpg": "0003",
		"IMG_20200101_123456.jpg": "", "2020-01-01.jpg": "", "holiday.jpg": "", "Scan 2020-01-01 (2).jpg": "2",
	} {
		if got := FilenameCounter(in); got != want {
			t.Errorf("FilenameCounter(%q) = %q, want %q", in, got, want)
		}
	}
}

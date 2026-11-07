// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// scanJPEG walks every JPEG fixture, and every truncation and single-byte
// change of their headers, exactly as imagemeta's walk does; it ends with
// errJPEGStray only where imagemeta would never end.
func TestScanJPEGMatchesImagemeta(t *testing.T) {
	var files []string
	for _, g := range []string{"testdata/photo/*.jpg", "../../testdata/fixtures/images/*/*.jpg", "../../testdata/media-synth/*/*.jpg"} {
		m, _ := filepath.Glob(g)
		files = append(files, m...)
	}
	if len(files) < 10 {
		t.Fatalf("only %d JPEG fixtures", len(files))
	}
	runs, stray := 0, 0
	for _, p := range files {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		head := min(len(b), 2048)
		variants := [][]byte{b}
		for n := 0; n < head; n += 7 {
			variants = append(variants, b[:n])
		}
		for i := 2; i < head; i += 13 {
			for _, v := range []byte{0xFF, 0xD9, 0xD8, 0x00} {
				c := bytes.Clone(b[:head])
				c[i] = v
				variants = append(variants, c)
			}
		}
		for _, v := range variants {
			runs++
			if jpegSame(t, v) {
				stray++
			}
		}
	}
	t.Logf("%d walks, %d stopped where imagemeta loops", runs, stray)
	if stray == 0 {
		t.Error("no variant reached the case imagemeta never ends: the test lost its point")
	}
}

// A damaged JPEG (an end-of-image before the tables, then a marker) is read
// as unreadable at once: the photo is dated by its name, the scan goes on.
func TestStrayJPEGMarkerEnds(t *testing.T) {
	b := []byte("\xff\xd8\xff\xd9\xff\xe1\x00\x10" + strings.Repeat("\x00", 64))
	done := make(chan error, 1)
	go func() {
		_, _, err := decodePhoto(bytes.NewReader(b))
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, errJPEGStray) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("decodePhoto did not return")
	}
	p := filepath.Join(t.TempDir(), "IMG_20200102_030405.jpg")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := extractPhoto(p)
	if err != nil || m.Time != "2020-01-02 03:04:05" || m.TimeSource != "filename" {
		t.Fatalf("%+v %v", m, err)
	}
}

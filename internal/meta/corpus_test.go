// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// corpus = real files produced by real tools (LibreOffice, pandoc, ImageMagick,
// WinRAR, 7-Zip, tar).
var corpus = filepath.Join("..", "..", "testdata", "fixtures")

type want struct {
	kind, src, time string // time "" = do not check (mtime of the checkout)
	title           string
	pages, entries  int
	mismatch        string // expected sniffExt when extMismatch
}

// Times: January 2026 fixtures = CET (+01:00); the RAR is a 2005 RAR4 (DOS local time).
var corpusWant = map[string]want{
	"archives/sample.7z":                   {kind: "archive", src: "metadata", time: "2026-01-27 15:54:42", entries: 3},
	"archives/sample.rar":                  {kind: "archive", src: "metadata", time: "2005-11-16 21:07:46", entries: 10},
	"archives/sample.tar":                  {kind: "archive", src: "metadata", time: "2026-01-27 15:54:42", entries: 3},
	"archives/sample.tar.gz":               {kind: "archive", src: "metadata", time: "2026-01-27 15:54:42", entries: 3},
	"archives/sample.zip":                  {kind: "archive", src: "metadata", time: "2026-01-27 15:54:42", entries: 3},
	"archives/corrupted/truncated.zip":     {kind: "archive", src: "filemtime"},
	"archives/corrupted/empty.zip":         {kind: "archive", src: "filemtime"},
	"documents/sample.docx":                {kind: "document", src: "metadata", time: "2026-01-27 15:54:41", pages: 1},
	"documents/sample.odt":                 {kind: "document", src: "metadata", time: "2026-01-27 15:54:41"},
	"documents/sample.pdf":                 {kind: "document", src: "metadata", time: "2026-01-27 15:54:41", title: "sample", pages: 1},
	"documents/sample.rtf":                 {kind: "document", src: "filemtime"},           // no \creatim in the file
	"documents/corrupted/truncated.pdf":    {kind: "document", src: "filemtime", pages: 1}, // pure-Go scan counts the one /Type /Page it sees
	"documents/corrupted/empty.pdf":        {kind: "document", src: "filemtime"},
	"sheets/sample.xlsx":                   {kind: "document", src: "filemtime"}, // core.xml has no dcterms:created
	"sheets/sample.ods":                    {kind: "document", src: "filemtime"}, // meta.xml has no creation-date
	"sheets/sample.csv":                    {kind: "text", src: "filemtime"},
	"sheets/corrupted/truncated.xlsx":      {kind: "document", src: "filemtime"},
	"text/sample.txt":                      {kind: "text", src: "filemtime"},
	"text/sample.md":                       {kind: "text", src: "filemtime"},
	"text/sample.html":                     {kind: "text", src: "filemtime"},
	"text/empty.txt":                       {kind: "text", src: "filemtime"},
	"media/sample.mp3":                     {kind: "audio", src: "filemtime"},
	"media/sample.wav":                     {kind: "audio", src: "filemtime"},
	"media/sample.mp4":                     {kind: "video", src: "filemtime"},
	"media/corrupted/truncated.mp4":        {kind: "video", src: "filemtime"},
	"xmp/sample.eps":                       {kind: "image", src: "metadata", time: "2026-01-27 15:54:43"}, // PostScript CreateDate +00:00
	"xmp/sample.psd":                       {kind: "image", src: "filemtime"},
	"images/corrupted/wrong_extension.jpg": {kind: "photo", src: "filemtime", mismatch: "png"},
	"images/corrupted/truncated.jpg":       {kind: "photo", src: "filemtime"},
	"images/corrupted/empty.jpg":           {kind: "photo", src: "filemtime"},
	"images/no_exif/web_export.png":        {kind: "photo", src: "filemtime"},
}

func TestCorpus(t *testing.T) {
	if _, err := os.Stat(corpus); err != nil {
		t.Skip("testdata/fixtures missing")
	}
	seen := 0
	err := filepath.WalkDir(corpus, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(corpus, p)
		rel = filepath.ToSlash(rel)
		done := make(chan Meta, 1)
		go func() { m, _ := ExtractWith(p, Options{}); done <- m }()
		var m Meta
		select {
		case m = <-done:
		case <-time.After(20 * time.Second):
			t.Fatalf("%s: extraction hangs", rel)
		}
		if m.Time == "" || m.TimeSource == "none" {
			t.Errorf("%s: no time at all: %+v", rel, m)
		}
		w, ok := corpusWant[rel]
		if !ok {
			if strings.HasPrefix(rel, "images/") && m.Kind != "photo" {
				t.Errorf("%s: kind %s", rel, m.Kind)
			}
			return nil
		}
		seen++
		if m.Kind != w.kind || m.TimeSource != w.src {
			t.Errorf("%s: kind=%s src=%s, want %s %s", rel, m.Kind, m.TimeSource, w.kind, w.src)
		}
		if w.time != "" && m.Time != w.time {
			t.Errorf("%s: time %s, want %s", rel, m.Time, w.time)
		}
		if m.Title != w.title || m.Pages != w.pages || m.Entries != w.entries {
			t.Errorf("%s: title=%q pages=%d entries=%d, want %q %d %d", rel, m.Title, m.Pages, m.Entries, w.title, w.pages, w.entries)
		}
		if (w.mismatch != "") != m.ExtMismatch || m.SniffExt != w.mismatch {
			t.Errorf("%s: mismatch=%v sniff=%q, want %q", rel, m.ExtMismatch, m.SniffExt, w.mismatch)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen != len(corpusWant) {
		t.Fatalf("checked %d of %d expected corpus files", seen, len(corpusWant))
	}
}

// Heavy: a 334 MB bootable ISO, not part of this repository: set
// ONEFOLD_ISO_FIXTURE to its path to run this test.
func TestISOListing(t *testing.T) {
	p := os.Getenv("ONEFOLD_ISO_FIXTURE")
	if p == "" {
		t.Skip("ONEFOLD_ISO_FIXTURE not set")
	}
	if _, err := os.Stat(p); err != nil {
		t.Skipf("ONEFOLD_ISO_FIXTURE: %v", err)
	}
	m := extract(t, p, false)
	// newest FILE member: 2009-01-29 01:07:35 UTC = 02:07:35 CET. 5861 = native
	// ISO 9660 walk; `7z l` reports 5862 because it adds the synthetic El Torito
	// entry "[BOOT]/Boot-NoEmul.img".
	if m.Kind != "archive" || m.Entries != 5861 || m.Time != "2009-01-29 02:07:35" || m.FileCategory != "disk_image" {
		t.Fatalf("iso: %+v", m)
	}
}

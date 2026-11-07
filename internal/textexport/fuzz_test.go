// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package textexport

import (
	"testing"
	"unicode/utf8"
)

// Fuzz targets of the pure-Go text readers: the Office 97-2003 streams (the
// container itself is fuzzed by internal/meta FuzzOLE2), RTF and HTML. None
// may panic, and the text they return is valid UTF-8.

// oleSeed adds the named streams of a fixture.
func oleSeed(f *testing.F, file string, names ...string) map[string][]byte {
	st, err := oleStreams("testdata/"+file, names...)
	if err != nil {
		f.Fatal(err)
	}
	return st
}

func checkText(t *testing.T, s string) {
	if !utf8.ValidString(s) {
		t.Fatalf("invalid UTF-8: %q", s)
	}
}

func FuzzDocText(f *testing.F) {
	st := oleSeed(f, "memo.doc", "WordDocument", "0Table", "1Table")
	f.Add(st["WordDocument"], st["0Table"], st["1Table"])
	f.Fuzz(func(t *testing.T, wd, t0, t1 []byte) {
		s, _ := docStreams(map[string][]byte{"WordDocument": wd, "0Table": t0, "1Table": t1})
		checkText(t, s)
	})
}

func FuzzXLSText(f *testing.F) {
	f.Add(oleSeed(f, "sheet.xls", "Workbook")["Workbook"])
	f.Fuzz(func(t *testing.T, wb []byte) {
		s, _ := xlsStream(wb)
		checkText(t, s)
	})
}

func FuzzPPTText(f *testing.F) {
	f.Add(oleSeed(f, "slides.ppt", "PowerPoint Document")["PowerPoint Document"])
	f.Fuzz(func(t *testing.T, doc []byte) {
		s, _ := pptStream(doc)
		checkText(t, s)
	})
}

func FuzzRTFText(f *testing.F) {
	f.Add(`{\rtf1\ansi\ansicpg1252{\fonttbl{\f0 Arial;}}\f0 Caff\'e8 荤? 10\par next}`)
	f.Add(`{\rtf1{\*\generator x;}{\pict\pngblip 89504e47}text\line \tab end}`)
	f.Fuzz(func(t *testing.T, s string) {
		checkText(t, rtfText(s))
	})
}

func FuzzHTMLText(f *testing.F) {
	f.Add(`<html><head><style>p{}</style><script>x()</script></head><body><p>Caff&egrave; &amp; <b>bold</b></p><br>end</body></html>`)
	f.Fuzz(func(t *testing.T, s string) {
		htmlText(s)
	})
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package names

import (
	"strings"
	"testing"
)

func TestParseLegacy(t *testing.T) {
	cases := []struct{ in, norm, clean, normNN, cleanNN, origin, date string }{
		{"[Registratore Tascam] 151209_0027", "[Registratore Tascam] 151209_0027", "151209_0027", "", "", "Registratore Tascam", ""},
		{"[asset] localdata -- frev.1", "[asset] localdata", "localdata", "", "", "asset", ""},
		{"(20220120_235959) [Desktop] note (2)", "[Desktop] note", "note", "", "", "Desktop", "20220120_235959"},
		{"prova1_01", "prova1_01", "prova1_01", "prova1", "prova1", "", ""},
		{"[x] a_01 (1) -- frev.3", "[x] a_01", "a_01", "[x] a", "a", "x", ""},
		{"esame10_09_04", "esame10_09_04", "esame10_09_04", "esame10_09", "esame10_09", "", ""},
		{"_01", "_01", "_01", "", "", "", ""},
		{"photo_2019", "photo_2019", "photo_2019", "", "", "", ""},
	}
	for _, c := range cases {
		l := ParseLegacy(c.in)
		if l.Raw != c.in || l.Norm != c.norm || l.Clean != c.clean || l.NormNoNN != c.normNN || l.CleanNoNN != c.cleanNN ||
			l.Origin != c.origin || l.NameDate != c.date {
			t.Errorf("%q -> %+v", c.in, l)
		}
	}
	if HasMarkers("a") || !HasMarkers("a (1)") || !HasMarkers("[x] a") || HasMarkers("a_01") {
		t.Fatal("HasMarkers")
	}
	for p, want := range map[string]bool{"/d/x (2).txt": true, "/d/x - Copy.txt": true, "/d/Copia di x.doc": true, "/d/x_copy2.txt": true, "/d/x.txt": false, "/d/copyright.txt": false,
		// localized Explorer and Finder copies
		"/d/Foto - Kopie.jpg": true, "/d/Photo - Copie (2).jpg": true, "/d/Foto - Cópia.jpg": true, "/d/zdjęcie - kopia.jpg": true,
		"/d/bild kopia 2.jpg": true, "/d/kuva - kopio.jpg": true, "/d/фото - копия.jpg": true, "/d/写真 - コピー.jpg": true,
		"/d/照片 - 副本.jpg": true, "/d/사진 - 복사본.jpg": true, "/d/Kopie von Bild.jpg": true, "/d/Copie de photo.jpg": true,
		"/d/kopiert.txt": false, "/d/Kopieren.txt": false} {
		if IsCopyName(p) != want {
			t.Errorf("IsCopyName(%s)", p)
		}
	}
}

func TestSanitizeAndNN(t *testing.T) {
	for in, want := range map[string]string{`a/b\c:d*e?"f"<g>|h`: "a_b_c_d_e__f__g__h", "  x   y. ": "x y", "...": "", "CON": "CON_", "com1.log": "com1_.log"} {
		if got := Sanitize(in, 100); got != want {
			t.Errorf("Sanitize(%q)=%q want %q", in, got, want)
		}
	}
	long := strings.Repeat("è", 100)
	if s := Sanitize(long, 101); len(s) != 100 {
		t.Fatalf("utf8-safe cut: %d", len(s))
	}
	if SanitizeName("CON.txt") != "CON_.txt" || SanitizeName(" .txt") != "unnamed.txt" {
		t.Fatal("SanitizeName")
	}
	if WithNN("a/b.tar.gz", 3) != "a/b.tar_03.gz" || WithNN("x/noext", 1) != "x/noext_01" {
		t.Fatal("WithNN")
	}
	if !IsNNOf("d/a_01.txt", "d/a.txt") || IsNNOf("d/a_01.txt", "e/a.txt") || IsNNOf("d/A_01.txt", "d/a.txt") {
		t.Fatal("IsNNOf")
	}
}

func TestTables(t *testing.T) {
	for in, want := range map[[2]int]string{{3840, 2160}: "4K", {1080, 1920}: "1080p", {1280, 720}: "720p", {640, 480}: "480p", {320, 240}: "SD", {0, 0}: ""} {
		if VideoTier(in[0], in[1]) != want {
			t.Errorf("tier %v", in)
		}
	}
	if SizeBucket(0) != "0MB+" || SizeBucket(7<<20) != "5MB+" || SizeBucket(2<<30) != "1GB+" {
		t.Fatal("SizeBucket")
	}
	if TypePrefix("video") != "VID" || TypePrefix("zzz") != "FILE" || len(MonthIT) != 12 || MonthEN[0] != "January" {
		t.Fatal("tables")
	}
}

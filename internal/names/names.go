// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package names holds the file-name primitives shared by planning, naming,
// merge and rearrange: portable sanitizing, the onefold collision suffix, the
// grammar of legacy tool markers, and small value tables (month names, video
// tiers, size buckets, type prefixes). Pure string functions, no I/O.
package names

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/mauriziofonte/onefold/internal/kinds"

	"golang.org/x/text/unicode/norm"
)

var (
	badFS  = regexp.MustCompile(`[/\\:*?"<>|\x00-\x1f]`)
	spaces = regexp.MustCompile(`\s+`)
)

// Sanitize makes s safe as one path component on Linux AND Windows, truncated
// (UTF-8 safe) to max bytes.
func Sanitize(s string, max int) string {
	s = badFS.ReplaceAllString(s, "_")
	s = spaces.ReplaceAllString(s, " ")
	s = strings.Trim(s, " .")
	if len(s) > max {
		cut := max
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = strings.TrimRight(s[:cut], " .")
	}
	return deReserve(s)
}

// winReserved are device names Windows refuses as a file/dir name, with or
// without extension (CON, CON.txt, com1.log...).
var winReserved = map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true}

// DeReserve appends "_" to a Windows-reserved stem ("CON" -> "CON_", "com1.log" -> "com1_.log").
func DeReserve(s string) string { return deReserve(s) }

func deReserve(s string) string {
	stem, ext := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		stem, ext = s[:i], s[i:]
	}
	if winReserved[strings.ToUpper(strings.TrimSpace(stem))] {
		return stem + "_" + ext
	}
	return s
}

// SplitName splits a base name into stem and extension (with the dot). A
// "extension" longer than 20 bytes or containing a space is part of the stem
// ("Report 2020.final draft").
func SplitName(base string) (stem, ext string) {
	ext = filepath.Ext(base)
	if len(ext) > 20 || strings.ContainsAny(ext, " ") {
		ext = ""
	}
	return strings.TrimSuffix(base, ext), ext
}

// SanitizeName makes a whole file name (stem + extension) portable, keeping the
// extension, max 200 bytes.
func SanitizeName(name string) string {
	stem, ext := SplitName(name)
	stem = Sanitize(stem, 200-len(ext))
	ext = badFS.ReplaceAllString(ext, "_")
	if stem == "" {
		stem = "unnamed"
	}
	return deReserve(stem + strings.TrimRight(ext, " ."))
}

// WithNN inserts the onefold collision suffix _NN before the extension of the
// last path segment.
func WithNN(rel string, n int) string {
	dir, file := path.Split(rel)
	ext := path.Ext(file)
	if ext == file { // ".hidden": no stem
		ext = ""
	}
	return dir + strings.TrimSuffix(file, ext) + fmt.Sprintf("_%02d", n) + ext
}

var reNNSuffix = regexp.MustCompile(`^(.*)_(\d{2,3})$`)

// NNOf returns n when cur == WithNN(target, n) (exact case), else 0.
func NNOf(cur, target string) int {
	cd, cf := path.Split(cur)
	td, tf := path.Split(target)
	if cd != td || path.Ext(cf) != path.Ext(tf) {
		return 0
	}
	m := reNNSuffix.FindStringSubmatch(strings.TrimSuffix(cf, path.Ext(cf)))
	if m == nil || m[1] != strings.TrimSuffix(tf, path.Ext(tf)) {
		return 0
	}
	n, _ := strconv.Atoi(m[2])
	return n
}

// IsNNOf reports whether cur == WithNN(target, n) for some n (exact case).
func IsNNOf(cur, target string) bool { return NNOf(cur, target) > 0 }

// Legacy markers left in names by earlier organizing tools and by copies:
//
//	"(yyyyMMdd_HHmmss) "  date prefix           -> removed (NameDate keeps it)
//	"[origin] "           origin-folder prefix  -> kept by Norm, removed by Clean
//	" -- frev.N"          collision suffix      -> removed
//	" (N)"                OS copy suffix        -> removed
//	"_NN"                 collision suffix      -> removed ONCE, only when the
//	                      base name exists next to the file (caller decides)
var (
	reNameDate = regexp.MustCompile(`^\((\d{8}_\d{6})\) `)
	reOrigin   = regexp.MustCompile(`^\[([^\]]+)\] `)
	reFrev     = regexp.MustCompile(` -- frev\.\d+$`)
	reParenN   = regexp.MustCompile(` \(\d+\)$`)
	reNN       = regexp.MustCompile(`_\d{2,3}$`)
)

// Legacy is the parse of a stem.
type Legacy struct {
	Raw       string // untouched
	Norm      string // "[origin] " kept, every other marker removed (except _NN)
	Clean     string // every marker removed (except _NN)
	NormNoNN  string // Norm without one trailing _NN ("" if none)
	CleanNoNN string // Clean without one trailing _NN ("" if none)
	Origin    string // value of "[origin] "
	NameDate  string // value of "(yyyyMMdd_HHmmss) "
}

// ParseLegacy parses the markers of a stem (no extension).
func ParseLegacy(stem string) Legacy {
	l := Legacy{Raw: stem}
	core := stem
	for i := 0; i < 8; i++ {
		before := core
		if m := reNameDate.FindStringSubmatch(core); m != nil {
			l.NameDate = m[1]
			core = core[len(m[0]):]
		}
		if m := reOrigin.FindStringSubmatch(core); m != nil {
			if l.Origin == "" {
				l.Origin = m[1]
			}
			core = core[len(m[0]):]
		}
		for _, re := range []*regexp.Regexp{reFrev, reParenN} {
			if loc := re.FindStringIndex(core); loc != nil && loc[0] > 0 {
				core = core[:loc[0]]
			}
		}
		if core == before {
			break
		}
	}
	core = strings.TrimSpace(core)
	if core == "" {
		core = strings.TrimSpace(stem)
	}
	var noNN string
	if loc := reNN.FindStringIndex(core); loc != nil && loc[0] > 0 {
		noNN = strings.TrimSpace(core[:loc[0]])
	}
	l.Clean, l.CleanNoNN = core, noNN
	l.Norm, l.NormNoNN = core, noNN
	if l.Origin != "" {
		l.Norm = "[" + l.Origin + "] " + core
		if noNN != "" {
			l.NormNoNN = "[" + l.Origin + "] " + noNN
		}
	}
	return l
}

// HasMarkers reports whether a stem carries a legacy marker other than a
// trailing _NN (which alone is ambiguous: "_09_04", "M1_75" are information).
func HasMarkers(stem string) bool { return ParseLegacy(stem).Clean != stem }

// copyWords are the words Windows Explorer ("x - Copy") and macOS Finder
// ("x copy 2") append to a copy, per language: en, it/es, de/nl/cs,
// fr/ro, pt, pl/sv, da/no, fi, tr, hu, ru, uk, el, ja, zh, ko.
var copyWords = []string{"copy", "copia", "kopie", "copie", "cópia", "kopia", "kopi", "kopio", "kopya",
	"másolat", "копия", "копія", "αντίγραφο", "コピー", "副本", "복사본"}

// copyPrefixes are the "Copy of x" forms of older Windows versions.
var copyPrefixes = []string{"copy of ", "copia di ", "copia - ", "copia de ", "cópia de ", "kopie von ", "kopie van ", "copie de "}

var reCopy = func() *regexp.Regexp {
	q := func(l []string) string {
		out := make([]string, len(l))
		for i, w := range l {
			out[i] = regexp.QuoteMeta(w)
		}
		return strings.Join(out, "|")
	}
	return regexp.MustCompile(`(?i)( \(\d+\)$|[ _-](` + q(copyWords) + `)( ?\d+)?$|^(` + q(copyPrefixes) + `))`)
}()

// IsCopyName reports whether the stem of a path looks like an OS/user copy
// ("x (2)", "x - copy", "x - Kopie", "Copia di x"): such names lose winner
// elections.
func IsCopyName(p string) bool {
	b := filepath.Base(p)
	return reCopy.MatchString(strings.TrimSuffix(b, filepath.Ext(b)))
}

// NFC is s in Unicode normalization form C: every name onefold writes from a
// template is NFC, whatever form the source used (macOS writes NFD).
func NFC(s string) string { return norm.NFC.String(s) }

// FoldKey is the comparison key of a name or relative path: NFC and lower
// case, so "Café.JPG" in NFD and "café.jpg" in NFC are the same slot (NTFS and
// APFS treat case, APFS also the normalization form, as one name).
func FoldKey(s string) string { return strings.ToLower(norm.NFC.String(s)) }

// Month names.
var (
	MonthIT = []string{"Gennaio", "Febbraio", "Marzo", "Aprile", "Maggio", "Giugno", "Luglio", "Agosto", "Settembre", "Ottobre", "Novembre", "Dicembre"}
	MonthEN = []string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
)

// VideoTier buckets a video by its SHORT side (portrait-safe); "" if unknown.
func VideoTier(w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}
	s := w
	if h < s {
		s = h
	}
	switch {
	case s >= 2160:
		return "4K"
	case s >= 1080:
		return "1080p"
	case s >= 720:
		return "720p"
	case s >= 480:
		return "480p"
	}
	return "SD"
}

// SizeBucket is the coarse size folder: 0MB+ 5MB+ 10MB+ 20MB+ 50MB+ 100MB+
// 200MB+ 500MB+ 1GB+.
func SizeBucket(n int64) string {
	const mb = 1 << 20
	switch {
	case n < 5*mb:
		return "0MB+"
	case n < 10*mb:
		return "5MB+"
	case n < 20*mb:
		return "10MB+"
	case n < 50*mb:
		return "20MB+"
	case n < 100*mb:
		return "50MB+"
	case n < 200*mb:
		return "100MB+"
	case n < 500*mb:
		return "200MB+"
	case n < 1024*mb:
		return "500MB+"
	}
	return "1GB+"
}

// TypePrefix is the short type tag of a kind: IMG VID AUD DOC ARC FILE.
func TypePrefix(kind string) string {
	switch kind {
	case kinds.Photo, kinds.Image:
		return "IMG"
	case kinds.Video:
		return "VID"
	case kinds.Audio:
		return "AUD"
	case kinds.Document, kinds.Text:
		return "DOC"
	case kinds.Archive:
		return "ARC"
	}
	return "FILE"
}

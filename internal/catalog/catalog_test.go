// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mauriziofonte/onefold/internal/kinds"
	"github.com/mauriziofonte/onefold/internal/testfix"
)

var t0 = time.Date(2020, 5, 6, 7, 8, 9, 0, time.Local)

// tree builds a mixed room. Returns the room path.
func tree(t *testing.T) string {
	t.Helper()
	room := filepath.Join(t.TempDir(), "TMP") // name collides with the "tmp" dev-dir prune: root guard
	files := map[string]string{
		"a.jpg":                    "\xff\xd8\xff jpeg-ish",
		"docs/report.pdf":          "%PDF-1.4 fake",
		"docs/notes.txt":           "notes",
		"docs/tmp/cache.txt":       "tmp content",
		"docs/OneDrive/o.txt":      "cloud",
		"Windows/sys.txt":          "system",
		"node_modules/x.js":        "module",
		".thumbnails/123.jpg":      "\xff\xd8\xff thumb",
		".hidden.txt":              "dot file",
		"__MACOSX/._a.jpg":         "fork",
		"Thumbs.db":                "junk",
		"app/setup.exe":            "MZ exe",
		"app/lib.dll":              "MZ dll",
		"weird.qqzzq":              "unknown ext",
		"noext":                    "no extension",
		"big.bin":                  strings.Repeat("x", 5000),
		"reencoded/v.mp4":          "\x00\x00\x00\x18ftypisom",
		"empty.txt":                "",
		"music/song.mp3":           "ID3 fake",
		"archive/a.zip":            "PK\x03\x04 fake",
		"sub/reencoded-not-dir.md": "md",
	}
	for p, body := range files {
		if err := testfix.File(filepath.Join(room, p), body, t0); err != nil {
			t.Fatal(err)
		}
	}
	return room
}

func run(t *testing.T, rooms []string, o Options) ([]Record, *Summary) {
	t.Helper()
	if o.Out == "" {
		o.Out = filepath.Join(t.TempDir(), "m.jsonl")
	}
	if o.Workers == 0 {
		o.Workers = 4
	}
	if o.Coverage == 0 {
		o.Coverage, o.Sample = 5, 4096
	}
	sum, err := Build(rooms, o)
	if err != nil {
		t.Fatal(err)
	}
	recs, err := LoadManifest(o.Out)
	if err != nil {
		t.Fatal(err)
	}
	return recs, sum
}

func rels(room string, recs []Record) []string {
	var out []string
	for _, r := range recs {
		rel, _ := filepath.Rel(room, r.Path)
		out = append(out, filepath.ToSlash(rel))
	}
	sort.Strings(out)
	return out
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	sort.Strings(want)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("\ngot  %v\nwant %v", got, want)
	}
}

func TestDefaultMediaScope(t *testing.T) {
	room := tree(t)
	exts, _, _ := kinds.PresetExts([]string{"media"})
	recs, _ := run(t, []string{room}, Options{Exts: exts})
	// .thumbnails pruned (dot dir), __MACOSX pruned, .hidden skipped
	eq(t, rels(room, recs), []string{"a.jpg", "reencoded/v.mp4"})
}

func TestAllDefaultsKeepDevAndCloud(t *testing.T) {
	room := tree(t)
	recs, _ := run(t, []string{room}, Options{})
	eq(t, rels(room, recs), []string{"a.jpg", "docs/report.pdf", "docs/notes.txt", "docs/tmp/cache.txt", "docs/OneDrive/o.txt",
		"Windows/sys.txt", "node_modules/x.js", "app/setup.exe", "app/lib.dll", "weird.qqzzq", "noext", "big.bin",
		"reencoded/v.mp4", "music/song.mp3", "archive/a.zip", "sub/reencoded-not-dir.md"})
}

func TestSweepFilters(t *testing.T) {
	room := tree(t)
	prune := append(append(append([]string{}, kinds.SystemDirs...), kinds.CloudDirs...), kinds.DevDirs...)
	recs, _ := run(t, []string{room}, Options{Prune: prune, SkipSystemExt: true, SkipUnknownExt: true, SkipNoExt: true})
	// room "TMP" itself survives the "tmp" prune (root guard); docs/tmp is pruned
	eq(t, rels(room, recs), []string{"a.jpg", "docs/report.pdf", "docs/notes.txt", "reencoded/v.mp4", "music/song.mp3",
		"archive/a.zip", "sub/reencoded-not-dir.md"})
}

// Every file and directory left out is reported once with its reason; the
// shortcut rule governs lnk (flagged system too); unknown extensions and files
// without extension follow their own rules.
func TestSkipReasons(t *testing.T) {
	room := tree(t)
	for p, body := range map[string]string{"Chrome.lnk": "L", "Site.url": "[InternetShortcut]", "x.tmp": "t", "~$Report.docx": "o",
		"y.crdownload": "c", "user.ini": "i", "Report.docx": "PK\x03\x04 doc"} {
		if err := testfix.File(filepath.Join(room, p), body, t0); err != nil {
			t.Fatal(err)
		}
	}
	f := Filters{Exclude: "archive"}
	o, err := f.Options("all")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	o.OnSkip = func(s Skip) {
		rel, _ := filepath.Rel(room, s.Path)
		if s.Dir {
			rel += "/"
		}
		if _, dup := got[filepath.ToSlash(rel)]; dup {
			t.Errorf("reported twice: %s", rel)
		}
		got[filepath.ToSlash(rel)] = s.Rule
	}
	recs, sum := run(t, []string{room}, o)
	want := map[string]string{
		"docs/tmp/": RuleDevDirs, "docs/OneDrive/": RuleCloudDirs, "Windows/": RuleSystemDirs, "node_modules/": RuleDevDirs,
		".thumbnails/": RuleDotDirs, "__MACOSX/": RuleJunk, "archive/": ReasonExclude,
		".hidden.txt": ReasonDotfile, "Thumbs.db": RuleJunk, "app/setup.exe": RuleSystemExt, "app/lib.dll": RuleSystemExt,
		"weird.qqzzq": RuleUnknownExt, "noext": RuleNoExt, "empty.txt": ReasonEmpty, "Chrome.lnk": RuleShortcuts,
		"Site.url": RuleShortcuts, "x.tmp": RuleTransient, "~$Report.docx": RuleTransient, "y.crdownload": RuleTransient,
		"user.ini": RuleSystemExt, "big.bin": RuleSystemExt,
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: reason %q, want %q", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("reported %d, want %d: %v", len(got), len(want), got)
	}
	files := 0
	for k := range want {
		if !strings.HasSuffix(k, "/") {
			files++
		}
	}
	if sum.Skipped != files {
		t.Errorf("Skipped %d, want %d (files only)", sum.Skipped, files)
	}
	eq(t, rels(room, recs), []string{"a.jpg", "docs/report.pdf", "docs/notes.txt", "reencoded/v.mp4", "music/song.mp3",
		"sub/reencoded-not-dir.md", "Report.docx"})

	// rules off: shortcuts and transient kept (lnk despite its system flag),
	// unknown and no-extension files kept
	f.Rules = map[string]bool{RuleShortcuts: false, RuleTransient: false, RuleUnknownExt: false, RuleNoExt: false}
	if o, err = f.Options("all"); err != nil {
		t.Fatal(err)
	}
	recs, _ = run(t, []string{room}, o)
	kept := strings.Join(rels(room, recs), "|")
	for _, k := range []string{"Chrome.lnk", "Site.url", "x.tmp", "~$Report.docx", "weird.qqzzq", "noext"} {
		if !strings.Contains(kept, k) {
			t.Errorf("%s not kept with its rule off: %s", k, kept)
		}
	}
	if strings.Contains(kept, "user.ini") || strings.Contains(kept, "setup.exe") {
		t.Errorf("system extensions kept: %s", kept)
	}
}

func TestExcludeIsRelativeToRoom(t *testing.T) {
	room := tree(t)
	// "TMP" is in the room path: it must NOT exclude the whole room
	recs, _ := run(t, []string{room}, Options{Exclude: []string{"TMP", "reencoded"}, Exts: map[string]bool{"jpg": true, "mp4": true, "md": true}})
	eq(t, rels(room, recs), []string{"a.jpg"})
}

func TestSizeAndLimit(t *testing.T) {
	room := tree(t)
	recs, _ := run(t, []string{room}, Options{MinSize: 100})
	eq(t, rels(room, recs), []string{"big.bin"})
	recs, _ = run(t, []string{room}, Options{MaxSize: 10, Exts: map[string]bool{"txt": true, "md": true, "js": true}})
	eq(t, rels(room, recs), []string{"docs/notes.txt", "node_modules/x.js", "sub/reencoded-not-dir.md", "Windows/sys.txt", "docs/OneDrive/o.txt"})
	recs, sum := run(t, []string{room}, Options{Limit: 3})
	if len(recs) != 3 || sum.Limited == 0 {
		t.Fatalf("limit: %d recs, limited %d", len(recs), sum.Limited)
	}
}

func TestHostAndKinds(t *testing.T) {
	room := tree(t)
	recs, sum := run(t, []string{room}, Options{Host: "nas1"})
	for _, r := range recs {
		if r.Host != "nas1" || r.MTimeNs != t0.UnixNano() || r.Room != room {
			t.Fatalf("record fields: %+v", r)
		}
	}
	if sum.ByKind["document"] != 1 || sum.ByKind["text"] != 5 || sum.ByKind["other"] != 6 || sum.ByKind["archive"] != 1 {
		t.Fatalf("by kind: %v", sum.ByKind)
	}
}

func TestReuse(t *testing.T) {
	room := tree(t)
	first, _ := run(t, []string{room}, Options{Host: "h"})
	old := filepath.Join(t.TempDir(), "old.jsonl")
	if _, err := Build([]string{room}, Options{Workers: 2, Coverage: 5, Sample: 4096, Out: old, Host: "h"}); err != nil {
		t.Fatal(err)
	}
	prev, _ := LoadManifestByPath(old)
	// change one file (size+mtime) -> must be re-extracted
	changed := filepath.Join(room, "docs/notes.txt")
	if err := testfix.File(changed, "notes, edited", t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	second, sum := run(t, []string{room}, Options{Host: "h", Reuse: prev})
	if sum.Reused != len(first)-1 {
		t.Fatalf("reused %d, want %d", sum.Reused, len(first)-1)
	}
	fresh, _ := run(t, []string{room}, Options{Host: "h"})
	if canon(second) != canon(fresh) {
		t.Fatal("reuse output differs from a fresh catalog")
	}
}

func canon(recs []Record) string {
	var lines []string
	for _, r := range recs {
		lines = append(lines, r.Path+"|"+r.Hash+"|"+r.Meta.Time+"|"+r.Meta.Kind)
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// Cataloging never modifies a source byte or mtime.
func TestSourcesUntouched(t *testing.T) {
	room := tree(t)
	before := snapshot(t, room)
	run(t, []string{room}, Options{})
	run(t, []string{room}, Options{Quick: true})
	if after := snapshot(t, room); after != before {
		t.Fatal("source tree changed during catalog")
	}
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
		lines = append(lines, p+" "+hex.EncodeToString(h[:])+" "+fi.ModTime().String())
		return nil
	})
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// Whole-file preload (WholeMax) must produce byte-identical records to the
// seek-based path, on the real-file corpus.
func TestWholeReadEquivalence(t *testing.T) {
	corpus := filepath.Join("..", "..", "testdata", "fixtures")
	if _, err := os.Stat(corpus); err != nil {
		t.Skip("corpus missing")
	}
	a, _ := run(t, []string{corpus}, Options{KeepJunk: true})
	b, _ := run(t, []string{corpus}, Options{KeepJunk: true, WholeMax: 1 << 30})
	if len(a) == 0 || canonFull(a) != canonFull(b) {
		t.Fatalf("whole-read records differ:\n%s\n----\n%s", canonFull(a), canonFull(b))
	}
}

func canonFull(recs []Record) string {
	var lines []string
	for _, r := range recs {
		r.MTimeNs = 0
		b, _ := json.Marshal(r)
		lines = append(lines, string(b))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// A cloud file whose content is not on this computer is left out (reading it
// would download it), also with -no-sweep; -keep online-only reads it.
func TestOnlineOnlyRule(t *testing.T) {
	for _, c := range []struct {
		f    Filters
		want bool
	}{{Filters{}, true}, {Filters{NoSweep: true}, true}, {Filters{Rules: map[string]bool{RuleOnlineOnly: false}}, false}} {
		if got := c.f.RuleOn(RuleOnlineOnly); got != c.want {
			t.Errorf("%+v: RuleOn(online-only) = %v, want %v", c.f, got, c.want)
		}
	}
	room := testfix.TempDir(t)
	for _, n := range []string{"here.txt", "cloud.txt"} {
		if err := testfix.File(filepath.Join(room, n), "content of "+n, t0); err != nil {
			t.Fatal(err)
		}
	}
	if err := testfix.SetOffline(filepath.Join(room, "cloud.txt")); errors.Is(err, testfix.ErrNoOffline) {
		t.Skip("no settable online-only marker here: fsx tests cover the flag")
	} else if err != nil {
		t.Fatal(err)
	}
	o, err := Filters{}.Options("all")
	if err != nil {
		t.Fatal(err)
	}
	var reasons []string
	o.OnSkip = func(s Skip) { reasons = append(reasons, filepath.Base(s.Path)+":"+s.Rule) }
	recs, sum := run(t, []string{room}, o)
	eq(t, rels(room, recs), []string{"here.txt"})
	if sum.OnlineOnly != 1 || sum.OnlineOnlyBytes != int64(len("content of cloud.txt")) || strings.Join(reasons, ",") != "cloud.txt:"+RuleOnlineOnly {
		t.Fatalf("online-only %d (%d bytes), reasons %v", sum.OnlineOnly, sum.OnlineOnlyBytes, reasons)
	}
	if o, err = (Filters{Rules: map[string]bool{RuleOnlineOnly: false}}).Options("all"); err != nil {
		t.Fatal(err)
	}
	recs, _ = run(t, []string{room}, o)
	eq(t, rels(room, recs), []string{"cloud.txt", "here.txt"})
}

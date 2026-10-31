// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package merge

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/fsx"
	"github.com/mauriziofonte/onefold/internal/sparsehash"
)

var (
	t2010 = time.Date(2010, 5, 5, 10, 0, 0, 0, time.Local)
	t2015 = time.Date(2015, 6, 6, 10, 0, 0, 0, time.Local)
	t2020 = time.Date(2020, 7, 7, 10, 0, 0, 0, time.Local)
)

func write(t *testing.T, p, body string, mt time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}
}

func snap(t *testing.T, root string) map[string]string {
	t.Helper()
	m := map[string]string{}
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.Contains(p, string(filepath.Separator)+CacheDir+string(filepath.Separator)) {
			return nil
		}
		b, _ := os.ReadFile(p)
		fi, _ := d.Info()
		h := sha256.Sum256(b)
		rel, _ := filepath.Rel(root, p)
		m[filepath.ToSlash(rel)] = hex.EncodeToString(h[:8]) + "@" + fi.ModTime().UTC().Format(time.RFC3339Nano)
		return nil
	})
	return m
}

func opts(inputs []string, out string, execute bool) Options {
	return Options{Inputs: inputs, Output: out, Execute: execute, Workers: 4,
		Catalog: catalog.Options{Workers: 4, Coverage: sparsehash.DefaultCoverage, Sample: sparsehash.DefaultSampleSize}}
}

func run(t *testing.T, o Options) *Result {
	t.Helper()
	r, err := Run(o)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func targets(r *Result) map[string]string {
	m := map[string]string{}
	for _, c := range r.Copies {
		m[filepath.Base(c.SrcPath)+"|"+filepath.Base(filepath.Dir(c.SrcPath))] = c.Rel
	}
	return m
}

// scenario: O is an <EXT>/<YEAR>/<name> tree and also the first input.
func scenario(t *testing.T) (O, B, C string) {
	root := t.TempDir()
	O, B, C = filepath.Join(root, "FILES"), filepath.Join(root, "B"), filepath.Join(root, "C")
	write(t, filepath.Join(O, "PDF/2010/fattura.pdf"), "%PDF fattura 2010", t2010)
	write(t, filepath.Join(O, "TXT/2015/Readme.txt"), "readme in O", t2015)
	write(t, filepath.Join(O, "TXT/2015/dup-a.txt"), "same bytes twice in O", t2015)
	write(t, filepath.Join(O, "TXT/2015/dup-b.txt"), "same bytes twice in O", t2015)
	write(t, filepath.Join(O, "notes.foo.tmp"), "user file that merely looks temporary", t2015) // must survive
	write(t, filepath.Join(O, "PDF/2010/stale.pdf.abc"+fsx.TempSuffix), "leftover of an interrupted run", t2015)

	write(t, filepath.Join(B, "scan/fattura-copy.pdf"), "%PDF fattura 2010", t2020) // already in O
	write(t, filepath.Join(B, "docs/new.pdf"), "%PDF brand new", t2020)
	write(t, filepath.Join(B, "docs/readme.txt"), "different readme", t2015) // case-only collision
	write(t, filepath.Join(B, "x/report (1).txt"), "report body", t2010)     // dirty duplicate
	write(t, filepath.Join(B, "y/report.txt"), "report body", t2020)         // clean duplicate: wins
	write(t, filepath.Join(B, "win/CON.txt"), "reserved name", t2020)
	write(t, filepath.Join(B, "nested/deep.txt"), "deep file", t2020)

	write(t, filepath.Join(C, "a/new.pdf"), "%PDF brand new", t2010) // dup of B/docs/new.pdf, older, but C is a later input
	write(t, filepath.Join(C, "b/unique.txt"), "only in C", t2020)
	return
}

func TestMergeDryRunWritesNothing(t *testing.T) {
	O, B, C := scenario(t)
	before := snap(t, O)
	bB, bC := snap(t, B), snap(t, C)
	r := run(t, opts([]string{O, B, C}, O, false))
	if !mapsEqual(snap(t, O), before) || !mapsEqual(snap(t, B), bB) || !mapsEqual(snap(t, C), bC) {
		t.Fatal("dry-run modified a tree")
	}
	if _, err := os.Stat(filepath.Join(O, CacheDir)); !os.IsNotExist(err) {
		t.Fatal("dry-run wrote the cache")
	}
	if len(r.Copies) != 6 || r.AlreadyPresent != 1 || r.InputDupes != 2 || r.OutputDupGroups != 1 {
		t.Fatalf("plan: copies=%d already=%d inDupes=%d outDupGroups=%d", len(r.Copies), r.AlreadyPresent, r.InputDupes, r.OutputDupGroups)
	}
}

func TestMergeExecute(t *testing.T) {
	O, B, C := scenario(t)
	before := snap(t, O)
	bB, bC := snap(t, B), snap(t, C)
	r := run(t, opts([]string{O, B, C}, O, true))
	if r.Missing() != 0 || r.Copied != 6 || r.StaleTemps != 1 {
		t.Fatalf("result: copied=%d missing=%d stale=%d failed=%v", r.Copied, r.Missing(), r.StaleTemps, r.Failed)
	}
	after := snap(t, O)
	// Every pre-existing output file unchanged (except our stale temp)
	for k, v := range before {
		if strings.HasSuffix(k, fsx.TempSuffix) {
			if _, ok := after[k]; ok {
				t.Errorf("stale temp %s not removed", k)
			}
			continue
		}
		if after[k] != v {
			t.Errorf("existing output file %s changed", k)
		}
	}
	// Inputs untouched
	if !mapsEqual(snap(t, B), bB) || !mapsEqual(snap(t, C), bC) {
		t.Fatal("inputs modified")
	}
	want := map[string]string{
		"new.pdf|docs":    "PDF/2020/new.pdf", // B before C (input order)
		"readme.txt|docs": "TXT/2015/readme_01.txt",
		"report.txt|y":    "TXT/2020/report.txt",
		"CON.txt|win":     "TXT/2020/CON_.txt",
		"deep.txt|nested": "TXT/2020/deep.txt",
		"unique.txt|b":    "TXT/2020/unique.txt",
	}
	got := targets(r)
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s -> %q, want %q", k, got[k], w)
		}
	}
	// copies are byte-identical, keep the source mtime, no temp left
	for _, c := range r.Copies {
		dst := filepath.Join(O, filepath.FromSlash(c.Rel))
		a, _ := os.ReadFile(c.SrcPath)
		b, err := os.ReadFile(dst)
		if err != nil || !bytes.Equal(a, b) {
			t.Errorf("copy %s differs", c.Rel)
		}
		sf, _ := os.Stat(c.SrcPath)
		df, _ := os.Stat(dst)
		if !sf.ModTime().Equal(df.ModTime()) {
			t.Errorf("mtime not preserved on %s", c.Rel)
		}
	}
	filepath.WalkDir(O, func(p string, d fs.DirEntry, err error) error {
		if err == nil && fsx.IsTemp(d.Name()) {
			t.Errorf("temp left: %s", p)
		}
		return nil
	})
	// Idempotent, served by the cache
	r2 := run(t, opts([]string{O, B, C}, O, true))
	if len(r2.Copies) != 0 || r2.Copied != 0 || r2.AlreadyPresent != r.AlreadyPresent+r.InputDupes+len(r.Copies) {
		t.Fatalf("second run: copies=%d already=%d", len(r2.Copies), r2.AlreadyPresent)
	}
}

func TestMergeOverlaps(t *testing.T) {
	O, B, _ := scenario(t)
	parent := filepath.Dir(O)
	// input == output, input inside output, output inside input, input listed twice
	r := run(t, opts([]string{O, filepath.Join(O, "TXT"), parent, B, B}, O, false))
	if len(r.Rooms) != 2 || r.Rooms[0] != O || r.Rooms[1] != parent {
		t.Fatalf("rooms: %v", r.Rooms)
	}
	for _, c := range r.Copies {
		if strings.HasPrefix(c.SrcPath, O+string(filepath.Separator)) {
			t.Fatalf("output file used as copy source: %s", c.SrcPath)
		}
	}
	// output-only merge (no other input): nothing to copy
	r2 := run(t, opts([]string{O}, O, false))
	if len(r2.Copies) != 0 {
		t.Fatalf("self-merge copies %d", len(r2.Copies))
	}
}

// Two different 10 MB files with an identical sparse sample.
func TestSparseCollisionNeverLosesData(t *testing.T) {
	root := t.TempDir()
	O, B := filepath.Join(root, "O"), filepath.Join(root, "B")
	body := bytes.Repeat([]byte("0123456789abcdef"), 10<<20/16)
	body[0] = 0 // binary: sparse (sampled) hash instead of full
	other := append([]byte(nil), body...)
	other[5<<20+12345] ^= 0xff // one byte in an unsampled region
	os.MkdirAll(filepath.Join(O, "BIN/2020"), 0o755)
	os.MkdirAll(B, 0o755)
	os.WriteFile(filepath.Join(O, "BIN/2020/a.bin"), body, 0o644)
	os.WriteFile(filepath.Join(B, "b.bin"), other, 0o644)
	ha, _ := sparsehash.HashFile(filepath.Join(O, "BIN/2020/a.bin"), 5, 4096)
	hb, _ := sparsehash.HashFile(filepath.Join(B, "b.bin"), 5, 4096)
	if ha.Hex != hb.Hex || ha.Full {
		t.Fatalf("fixture must collide on the sparse hash (%s %s full=%v)", ha.Hex, hb.Hex, ha.Full)
	}
	o := opts([]string{O, B}, O, false)
	o.TrustSparse = true
	if r := run(t, o); len(r.Copies) != 0 {
		t.Fatalf("trust-sparse should (wrongly) consider it present: %d", len(r.Copies))
	}
	r := run(t, opts([]string{O, B}, O, true))
	if r.Verify.Splits != 1 || r.Copied != 1 || r.Missing() != 0 {
		t.Fatalf("default merge must detect the collision and copy: %+v", r.Verify)
	}
}

func TestBlockedParent(t *testing.T) {
	root := t.TempDir()
	O, B := filepath.Join(root, "O"), filepath.Join(root, "B")
	write(t, filepath.Join(O, "ZIP"), "a FILE named like the target dir", t2015)
	write(t, filepath.Join(B, "x.zip"), "PK\x03\x04 zip", t2020)
	r := run(t, opts([]string{B}, O, true))
	if len(r.Blocked) != 1 || r.Missing() != 1 || r.Copied != 0 {
		t.Fatalf("blocked: %+v", r)
	}
	if b, _ := os.ReadFile(filepath.Join(O, "ZIP")); string(b) != "a FILE named like the target dir" {
		t.Fatal("blocking file touched")
	}
}

func TestRenameNoReplace(t *testing.T) {
	d := t.TempDir()
	a, b := filepath.Join(d, "a"), filepath.Join(d, "b")
	os.WriteFile(a, []byte("A"), 0o644)
	os.WriteFile(b, []byte("B"), 0o644)
	if err := fsx.RenameNoReplace(a, b); err != fsx.ErrExists {
		t.Fatalf("want ErrExists, got %v", err)
	}
	if x, _ := os.ReadFile(b); string(x) != "B" {
		t.Fatal("target overwritten")
	}
	c := filepath.Join(d, "c")
	if err := fsx.RenameNoReplace(a, c); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(a); !os.IsNotExist(err) {
		t.Fatal("source still present after rename")
	}
}

func TestLayouts(t *testing.T) {
	O, B, C := scenario(t)
	o := opts([]string{O, B, C}, O, false)
	o.Layout = "timeline"
	r := run(t, o)
	for _, c := range r.Copies {
		if !strings.HasPrefix(c.Rel, "TIMELINE/") {
			t.Fatalf("timeline layout: %s", c.Rel)
		}
	}
	o.Layout = "bogus"
	if _, err := Run(o); err == nil {
		t.Fatal("bad layout accepted")
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	ka := make([]string, 0, len(a))
	for k := range a {
		ka = append(ka, k)
	}
	sort.Strings(ka)
	for _, k := range ka {
		if a[k] != b[k] {
			return false
		}
	}
	return true
}

// Paths beyond Windows MAX_PATH (260) and a 200-byte file name.
func TestLongPaths(t *testing.T) {
	root := t.TempDir()
	O, B := filepath.Join(root, "O"), filepath.Join(root, "B")
	deep := B
	for i := 0; i < 8; i++ {
		deep = filepath.Join(deep, strings.Repeat("d", 40)+string(rune('a'+i)))
	}
	name := strings.Repeat("n", 190) + ".txt"
	src := filepath.Join(deep, name)
	if len(src) < 300 {
		t.Fatalf("fixture path too short: %d", len(src))
	}
	write(t, src, "deep content", t2020)
	r := run(t, opts([]string{B}, O, true))
	if r.Copied != 1 || r.Missing() != 0 {
		t.Fatalf("long path merge: %+v failed=%v", r, r.Failed)
	}
	got, err := os.ReadFile(filepath.Join(O, "TXT", "2020", name))
	if err != nil || string(got) != "deep content" {
		t.Fatalf("long copy: %v", err)
	}
}

// Settled (base of the source purge): after an execute, every input
// file whose content is in the output, with the output file holding it; a
// blocked copy and its input duplicates are not in it. Excluded lists what the
// filters left out.
func TestSettledAndExcluded(t *testing.T) {
	root := t.TempDir()
	O, A := filepath.Join(root, "O"), filepath.Join(root, "A")
	write(t, filepath.Join(O, "TXT", "2010", "old.txt"), "already there", t2010)
	write(t, filepath.Join(A, "old copy.txt"), "already there", t2010)
	write(t, filepath.Join(A, "new.txt"), "brand new", t2015)
	write(t, filepath.Join(A, "sub", "new dup.txt"), "brand new", t2020)
	write(t, filepath.Join(A, "x.tmp"), "transient", t2015)
	write(t, filepath.Join(O, "BIN"), "a FILE where a directory is needed", t2010)
	write(t, filepath.Join(A, "blocked.bin"), "blocked content", t2015)
	o := opts([]string{A}, O, true)
	o.Naming = "{EXT}/{year}/{stem}<.{ext}>"
	o.Catalog.SkipTransient = true
	r, err := Run(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Blocked) != 1 {
		t.Fatalf("blocked %v", r.Blocked)
	}
	got := map[string]string{}
	for _, s := range r.Settled {
		rel, _ := filepath.Rel(root, s.Keeper)
		got[filepath.Base(s.Src)] = filepath.ToSlash(rel)
		if s.Content == "" || s.Size == 0 {
			t.Errorf("%s: content %q size %d", s.Src, s.Content, s.Size)
		}
	}
	want := map[string]string{"old copy.txt": "O/TXT/2010/old.txt", "new.txt": "O/TXT/2015/new.txt", "new dup.txt": "O/TXT/2015/new.txt"}
	if !mapsEqual(got, want) {
		t.Fatalf("settled %v want %v", got, want)
	}
	if len(r.Excluded) != 1 || filepath.Base(r.Excluded[0].Path) != "x.tmp" || r.Excluded[0].Rule != catalog.RuleTransient {
		t.Fatalf("excluded %+v", r.Excluded)
	}
	// a dry run lists the exclusions but settles nothing
	o.Execute = false
	if r = run(t, o); len(r.Settled) != 0 || len(r.Excluded) != 1 {
		t.Fatalf("dry run: settled %d excluded %d", len(r.Settled), len(r.Excluded))
	}
}

// Re-plan: on the records of a preview's state cache, with new user
// exclusions, the plan equals a fresh preview with those exclusions, and
// nothing is read from the inputs again.
func TestReplanEqualsPreview(t *testing.T) {
	root := t.TempDir()
	O, A := filepath.Join(root, "O"), filepath.Join(root, "A")
	write(t, filepath.Join(O, "TXT", "2010", "old.txt"), "already there", t2010)
	write(t, filepath.Join(A, "old copy.txt"), "already there", t2010)
	write(t, filepath.Join(A, "DRIVERS", "Display", "nvdata.data"), "driver data", t2015)
	write(t, filepath.Join(A, "DRIVERS", "Display", "license.txt"), "driver license", t2015)
	write(t, filepath.Join(A, "keep", "a.txt"), "keep a", t2015)
	write(t, filepath.Join(A, "keep", "a copy.txt"), "keep a", t2020)
	write(t, filepath.Join(A, "x.data"), "x data", t2020)
	state := filepath.Join(root, "state.jsonl")
	o := opts([]string{A}, O, false)
	o.StateCache = state
	run(t, o)
	recs, err := catalog.LoadManifest(state)
	if err != nil {
		t.Fatal(err)
	}
	excl := func(o *Options) {
		o.Catalog.ExcludePaths = []string{filepath.Join(A, "DRIVERS")}
		o.Catalog.ExcludeGlobs, _ = catalog.CompileGlobs([]string{"*.data"})
	}
	fresh := opts([]string{A}, O, false)
	excl(&fresh)
	want := run(t, fresh)
	re := opts([]string{A}, O, false)
	excl(&re)
	re.Records, re.StateCache = recs, state
	before, _ := os.ReadFile(state)
	got := run(t, re)
	if !mapsEqual(targets(got), targets(want)) || got.AlreadyPresent != want.AlreadyPresent || got.InputDupes != want.InputDupes ||
		got.InputFiles != want.InputFiles {
		t.Fatalf("replan %v (%d %d %d)\nfresh  %v (%d %d %d)", targets(got), got.AlreadyPresent, got.InputDupes, got.InputFiles,
			targets(want), want.AlreadyPresent, want.InputDupes, want.InputFiles)
	}
	if len(got.Excluded) != 3 { // the two driver files and x.data, each a file
		t.Fatalf("excluded %+v", got.Excluded)
	}
	if after, _ := os.ReadFile(state); !bytes.Equal(before, after) {
		t.Fatal("a re-plan rewrote the state cache")
	}
	// removing the exclusions on the same records brings everything back
	all := opts([]string{A}, O, false)
	all.Records = recs
	if r := run(t, all); len(r.Copies) != 4 || len(r.Excluded) != 0 {
		t.Fatalf("no exclusions: %d copies, %+v", len(r.Copies), r.Excluded)
	}
}

// -verify-uncached gives the same merge (copies read back from the device).
func TestVerifyUncached(t *testing.T) {
	O, B, C := scenario(t)
	o := opts([]string{O, B, C}, O, true)
	o.VerifyUncached = true
	r := run(t, o)
	if r.Missing() != 0 || r.Copied != 6 || len(r.Failed) != 0 {
		t.Fatalf("result: copied=%d missing=%d failed=%v cached=%d", r.Copied, r.Missing(), r.Failed, r.CachedVerify)
	}
	if (runtime.GOOS == "linux" || runtime.GOOS == "windows") && r.CachedVerify != 0 {
		t.Errorf("%d copies verified through the cache", r.CachedVerify)
	}
}

// -keep-dotfiles: dotfiles are merged like any content; 0-byte files are
// identified by their name (one per name in the output), and a second merge
// copies nothing again.
func TestKeepDotfiles(t *testing.T) {
	root := t.TempDir()
	A, B, O := filepath.Join(root, "A"), filepath.Join(root, "B"), filepath.Join(root, "O")
	for p, body := range map[string]string{
		"A/.env.sample": "KEY=1", "A/a.txt": "text a", "A/x/.nomedia": "", "A/pkg/__init__.py": "",
		"B/.env.sample": "KEY=1", "B/y/.nomedia": "", "B/other/__init__.py": "", "B/empty.txt": "",
	} {
		write(t, filepath.Join(root, p), body, time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC))
	}
	if err := os.MkdirAll(O, 0o755); err != nil {
		t.Fatal(err)
	}
	o := opts([]string{A, B}, O, true)
	o.Catalog.KeepDotfiles, o.Catalog.KeepEmpty = true, true
	r := run(t, o)
	// .env.sample once (same content), a.txt, one .nomedia, one __init__.py, empty.txt
	if r.Copied != 5 || r.Missing() != 0 {
		t.Fatalf("first merge: copied %d missing %d failed %v", r.Copied, r.Missing(), r.Failed)
	}
	r = run(t, o)
	if r.Copied != 0 || len(r.Copies) != 0 {
		t.Fatalf("second merge copied %d: %+v", r.Copied, r.Copies)
	}
	// without the option, the historical scope: no dotfile, no empty file
	O2 := filepath.Join(root, "O2")
	os.MkdirAll(O2, 0o755)
	if r := run(t, opts([]string{A, B}, O2, true)); r.Copied != 1 {
		t.Fatalf("default merge copied %d, want 1 (a.txt)", r.Copied)
	}
}

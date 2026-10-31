// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package rearrange

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/merge"
	"github.com/mauriziofonte/onefold/internal/sparsehash"
	"github.com/mauriziofonte/onefold/internal/testfix"
)

func y(year int) time.Time { return time.Date(year, 6, 15, 12, 0, 0, 0, time.Local) }

func put(t *testing.T, root, rel, body string, mt time.Time) {
	t.Helper()
	if err := testfix.File(filepath.Join(root, filepath.FromSlash(rel)), body, mt); err != nil {
		t.Fatal(err)
	}
}

// tree returns rel -> content+mtime of every file (excluding .onefold).
func tree(t *testing.T, root string) map[string]string {
	t.Helper()
	m := map[string]string{}
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == merge.CacheDir {
				return filepath.SkipDir
			}
			return nil
		}
		b, _ := os.ReadFile(p)
		fi, _ := d.Info()
		h := sha256.Sum256(b)
		rel, _ := filepath.Rel(root, p)
		m[filepath.ToSlash(rel)] = hex.EncodeToString(h[:6]) + "@" + fi.ModTime().UTC().Format(time.RFC3339)
		return nil
	})
	return m
}

func contents(m map[string]string) []string {
	var out []string
	for _, v := range m {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func scenario(t *testing.T) string {
	root := filepath.Join(t.TempDir(), "FILES")
	must(t, testfix.PDF(filepath.Join(root, "PDF", "2015", "report.pdf"), "R", "A", time.Date(2012, 1, 2, 3, 4, 5, 0, time.UTC), "x"))
	put(t, root, "TXT/2020/a.txt", "alpha", y(2020))
	put(t, root, "TXT/2020/a_01.txt", "alpha two", y(2020))
	put(t, root, "TXT/2020/b.txt", "bravo", y(2020))
	put(t, root, "TXT/2020/[Desktop] b -- frev.1.txt", "bravo other", y(2020))
	put(t, root, "MP4/clip.mp4", "not really a video", y(2018))
	put(t, root, "PDF/2020/X.PDF", "%PDF-1.4 no date here", y(2020))
	put(t, root, "TXT/2020/c.txt", "cycle one", y(2019)) // wants TXT/2019/c.txt
	put(t, root, "TXT/2019/c.txt", "cycle two", y(2020)) // wants TXT/2020/c.txt
	put(t, root, "TXT/2021/dup.txt", "same bytes", y(2021))
	put(t, root, "OLD/dup copy.txt", "same bytes", y(2022))
	put(t, root, "TXT/1980/ancient.txt", "no valid date", time.Date(1980, 1, 1, 0, 0, 0, 0, time.Local))
	put(t, root, ".hidden/secret.txt", "out of scope", y(2020))
	put(t, root, "TXT/2020/.dotfile", "out of scope", y(2020))
	put(t, root, "TXT/2020/empty.txt", "", y(2020))
	return root
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func opts(root string, execute bool) Options {
	return Options{Root: root, Execute: execute, DeleteDupes: true, Workers: 4,
		Catalog: catalog.Options{Workers: 4, Coverage: sparsehash.DefaultCoverage, Sample: sparsehash.DefaultSampleSize}}
}

var wantAfter = []string{
	"PDF/2012/report.pdf",
	"TXT/2020/a.txt", "TXT/2020/a_01.txt",
	"TXT/2020/b.txt", "TXT/2020/[Desktop] b.txt",
	"MP4/2018/clip.mp4",
	"PDF/2020/X.pdf",
	"TXT/2019/c.txt", "TXT/2020/c.txt",
	"TXT/2021/dup.txt",
	"TXT/UNDATED/ancient.txt",
	".hidden/secret.txt", "TXT/2020/.dotfile", "TXT/2020/empty.txt",
}

func TestRearrangeEndToEnd(t *testing.T) {
	root := scenario(t)
	before := tree(t, root)

	// dry-run: nothing touched, no journal
	r, err := Run(opts(root, false))
	must(t, err)
	if !mapsEq(tree(t, root), before) {
		t.Fatal("dry-run modified the tree")
	}
	if _, err := os.Stat(filepath.Join(root, merge.CacheDir)); !os.IsNotExist(err) {
		t.Fatal("dry-run wrote .onefold")
	}
	if len(r.Deletes) != 1 || r.Deletes[0].Path != "OLD/dup copy.txt" || r.Deletes[0].Keeper != "TXT/2021/dup.txt" {
		t.Fatalf("deletes: %+v", r.Deletes)
	}

	r, err = Run(opts(root, true))
	must(t, err)
	if len(r.Failed) > 0 {
		t.Fatalf("failures: %v", r.Failed)
	}
	after := tree(t, root)
	var got []string
	for k := range after {
		got = append(got, k)
	}
	sort.Strings(got)
	want := append([]string(nil), wantAfter...)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("tree after:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// content + mtime preserved: after == before minus the deleted duplicate
	delete(before, "OLD/dup copy.txt")
	if strings.Join(contents(after), ",") != strings.Join(contents(before), ",") {
		t.Fatal("content or mtime changed by the rearrange")
	}
	// the cycle really swapped
	if after["TXT/2019/c.txt"] != before["TXT/2020/c.txt"] || after["TXT/2020/c.txt"] != before["TXT/2019/c.txt"] {
		t.Fatal("cycle not resolved")
	}
	if _, err := os.Stat(filepath.Join(root, "OLD")); !os.IsNotExist(err) {
		t.Fatal("emptied dir OLD not removed")
	}
	if _, err := os.Stat(filepath.Join(root, "TXT", "1980")); !os.IsNotExist(err) {
		t.Fatal("emptied dir TXT/1980 not removed")
	}
	if _, err := os.Stat(r.Journal); err != nil {
		t.Fatal("journal missing")
	}

	// Idempotent
	r2, err := Run(opts(root, true))
	must(t, err)
	if len(r2.Moves) != 0 || len(r2.Deletes) != 0 {
		t.Fatalf("second run: moves %v deletes %v", r2.Moves, r2.Deletes)
	}

	// Undo restores every moved file (deletion is final)
	done, failed, err := Recover(r.Journal, true)
	must(t, err)
	if failed != 0 || done != len(r.Moves) {
		t.Fatalf("undo: done %d failed %d of %d", done, failed, len(r.Moves))
	}
	undone := tree(t, root)
	if !mapsEq(undone, before) {
		t.Fatalf("undo did not restore the original layout")
	}
	// and forward again from the same journal
	done, failed, err = Recover(r.Journal, false)
	must(t, err)
	if failed != 0 || done != len(r.Moves) || !mapsEq(tree(t, root), after) {
		t.Fatalf("resume forward: done %d failed %d", done, failed)
	}
}

// An interrupted run (a file left mid-move) blocks new runs until -resume.
func TestInterruptedRunNeedsResume(t *testing.T) {
	root := scenario(t)
	r, err := Run(opts(root, true))
	must(t, err)
	must(t, err)
	m := r.Moves[0]
	// simulate a crash between pass 1 and pass 2 for this file
	must(t, os.Rename(filepath.Join(root, filepath.FromSlash(m.Dst)), filepath.Join(root, filepath.FromSlash(m.Tmp))))
	if _, err := Run(opts(root, false)); err == nil || !strings.Contains(err.Error(), "resume") {
		t.Fatalf("run over a pending move must refuse: %v", err)
	}
	done, failed, err := Recover(r.Journal, false)
	must(t, err)
	if done != 1 || failed != 0 {
		t.Fatalf("resume: %d %d", done, failed)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(m.Dst))); err != nil {
		t.Fatal("resumed file not at its destination")
	}
}

// Duplicates are deleted only when the full content still matches the keeper.
func TestDuplicateReverifiedBeforeDelete(t *testing.T) {
	root := filepath.Join(t.TempDir(), "R")
	put(t, root, "TXT/2021/dup.txt", "same bytes", y(2021))
	put(t, root, "OLD/dup copy.txt", "same bytes", y(2022))
	o := opts(root, false)
	r, err := Run(o)
	must(t, err)
	if len(r.Deletes) != 1 {
		t.Fatalf("plan: %+v", r.Deletes)
	}
	// content changes between plan and execution: simulate by editing the keeper
	put(t, root, "TXT/2021/dup.txt", "changed!!!", y(2021))
	res := &Result{Deletes: r.Deletes}
	doDeletes(root, res, 1, func() {}, nil)
	if res.Deleted != 0 || len(res.Failed) != 1 {
		t.Fatalf("a changed keeper must block the deletion: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(root, "OLD", "dup copy.txt")); err != nil {
		t.Fatal("duplicate deleted despite changed keeper")
	}
	// report mode keeps duplicates and rearranges them
	o.DeleteDupes = false
	r, err = Run(o)
	must(t, err)
	if len(r.Deletes) != 0 {
		t.Fatal("report mode must not delete")
	}
}

func TestNamingPresetsRearrange(t *testing.T) {
	root := filepath.Join(t.TempDir(), "R")
	put(t, root, "x/(20151209_002700) [Registratore Tascam] 151209_0027 -- frev.1.wav", "RIFF....WAVE", y(2015))
	o := opts(root, true)
	o.Naming = "ext-year"
	r, err := Run(o)
	must(t, err)
	if len(r.Moves) != 1 || r.Moves[0].Dst != "WAV/2015/[Registratore Tascam] 151209_0027.wav" {
		t.Fatalf("ext-year keeps the origin prefix: %+v", r.Moves)
	}
	o.Naming = "ext-era-year-origin" // origin recovered from the name, not the parent folder
	r, err = Run(o)
	must(t, err)
	if len(r.Moves) != 0 {
		t.Fatalf("origin preset: %+v", r.Moves)
	}
	o.Naming = "ext-year-clean"
	r, err = Run(o)
	must(t, err)
	if len(r.Moves) != 1 || r.Moves[0].Dst != "WAV/2015/151209_0027.wav" {
		t.Fatalf("ext-year-clean drops the origin prefix: %+v", r.Moves)
	}
}

func mapsEq(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// Re-plan: the records of a preview with new user exclusions give the
// plan of a fresh preview with those exclusions; excluded files stay put.
func TestReplanEqualsPreview(t *testing.T) {
	root := filepath.Join(t.TempDir(), "R")
	put(t, root, "a.txt", "alpha", y(2015))
	put(t, root, "a copy.txt", "alpha", y(2016))
	put(t, root, "DRIVERS/Display/nvdata.data", "driver", y(2013))
	put(t, root, "docs/b.pdf", "%PDF-1.4 b", y(2019))
	state := filepath.Join(t.TempDir(), "state.jsonl")
	o := opts(root, false)
	o.StateCache = state
	if _, err := Run(o); err != nil {
		t.Fatal(err)
	}
	recs, err := catalog.LoadManifest(state)
	must(t, err)
	fresh := opts(root, false)
	fresh.Catalog.ExcludePaths = []string{filepath.Join(root, "DRIVERS")}
	want, err := Run(fresh)
	must(t, err)
	re := opts(root, false)
	re.Catalog.ExcludePaths = fresh.Catalog.ExcludePaths
	re.Records = recs
	got, err := Run(re)
	must(t, err)
	moves := func(r *Result) string {
		var s []string
		for _, m := range r.Moves {
			s = append(s, m.Src+">"+m.Dst)
		}
		for _, d := range r.Deletes {
			s = append(s, "del "+d.Path)
		}
		sort.Strings(s)
		return strings.Join(s, "|")
	}
	if moves(got) != moves(want) || got.Files != want.Files {
		t.Fatalf("replan %s (%d)\nfresh  %s (%d)", moves(got), got.Files, moves(want), want.Files)
	}
	if len(got.Excluded) != 1 || !strings.HasSuffix(filepath.ToSlash(got.Excluded[0].Path), "DRIVERS/Display/nvdata.data") ||
		got.Excluded[0].Rule != catalog.ReasonExcludePath {
		t.Fatalf("excluded %+v", got.Excluded)
	}
}

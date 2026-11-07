// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mauriziofonte/onefold/internal/fsx"
	"github.com/mauriziofonte/onefold/internal/rearrange"
	"github.com/mauriziofonte/onefold/internal/testfix"
)

// Crash tests: merge, rearrange, purge-sources and apply are stopped at each
// of their writes, fsyncs, renames, links and removals in turn (a binary
// built with -tags crashtest, fsx.CrashPoint), as a kill -9 would stop them.
// After every stop the command is run again (rearrange: resumed from its
// journal), and the result must be the one of a run that was never stopped:
// the same names with the same bytes, the sources untouched (merge, apply)
// or deleted only where their content is in the output (purge), no
// onefold temporary file left behind.

var (
	crashOnce sync.Once
	crashBin  string
	crashErr  error
	crashDir  string
)

// crashBinary builds the crashtest binary once per test run.
func crashBinary(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no Go toolchain: the crash tests build their own binary")
	}
	crashOnce.Do(func() {
		crashDir, crashErr = os.MkdirTemp("", "onefold-crash-bin")
		if crashErr != nil {
			return
		}
		crashBin = filepath.Join(crashDir, "onefold-crash")
		if runtime.GOOS == "windows" {
			crashBin += ".exe"
		}
		cmd := exec.Command("go", "build", "-tags", "crashtest", "-o", crashBin, ".")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			crashErr = errors.New(string(out))
		}
	})
	if crashErr != nil {
		t.Fatal(crashErr)
	}
	return crashBin
}

func removeCrashDir() {
	if crashDir != "" {
		os.RemoveAll(crashDir)
	}
}

// runAt runs the crashtest binary, stopping at crash point n (0: never).
// It reports whether the run was stopped; any other failure fails the test.
func runAt(t *testing.T, n int, args ...string) (stopped bool, out string) {
	t.Helper()
	cmd := exec.Command(crashBinary(t), args...)
	cmd.Env = append(os.Environ(), fsx.CrashEnv+"="+strconv.Itoa(n), "ONEFOLD_NO_EXTERNAL=1", "ONEFOLD_NO_HASH_CACHE=1")
	b, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == fsx.CrashExit {
		return true, string(b)
	}
	if err != nil {
		t.Fatalf("onefold %v (crash point %d): %v\n%s", args, n, err, b)
	}
	return false, string(b)
}

// treeOf maps every file under root (outside .onefold folders) to its
// content hash and modification time; temps lists onefold temporary files
// and rearrange's in-flight names anywhere under root.
func treeOf(t *testing.T, root string) (tree map[string]string, temps []string) {
	t.Helper()
	tree = map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if fsx.IsTemp(d.Name()) || strings.HasSuffix(d.Name(), rearrange.MoveSuffix) {
			temps = append(temps, p)
		}
		if d.IsDir() {
			if d.Name() == ".onefold" {
				return filepath.SkipDir
			}
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		h := sha256.Sum256(b)
		tree[filepath.ToSlash(rel)] = hex.EncodeToString(h[:8]) + " " + fi.ModTime().UTC().Format(time.RFC3339)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree, temps
}

func sameTree(t *testing.T, what string, n int, got, want map[string]string) {
	t.Helper()
	var diff []string
	for k, v := range want {
		if got[k] != v {
			diff = append(diff, "want "+k+" "+v+", got "+got[k])
		}
	}
	for k, v := range got {
		if _, ok := want[k]; !ok {
			diff = append(diff, "extra "+k+" "+v)
		}
	}
	if len(diff) > 0 {
		sort.Strings(diff)
		t.Fatalf("%s after a stop at crash point %d differs from an uninterrupted run:\n%s", what, n, strings.Join(diff, "\n"))
	}
}

// everyPoint runs one iteration per crash point, 1, 2, ..., until the step
// completes without being stopped; it returns how many points there were.
func everyPoint(t *testing.T, iter func(n int, dir string) (stopped bool)) int {
	t.Helper()
	base := testfix.TempDir(t)
	for n := 1; n < 2000; n++ {
		dir := filepath.Join(base, strconv.Itoa(n))
		if !iter(n, dir) {
			return n - 1
		}
	}
	t.Fatal("more than 2000 crash points")
	return 0
}

var mtime = time.Date(2019, 5, 6, 7, 8, 9, 0, time.UTC)

// crashInputs writes two input folders with copies inside and across them,
// a name taken by two contents, and a nested folder.
func crashInputs(t *testing.T, dir string) (a, b string) {
	t.Helper()
	a, b = filepath.Join(dir, "A"), filepath.Join(dir, "B")
	for rel, body := range map[string]string{
		"A/a.txt": "alpha", "A/sub/b.txt": "beta", "A/c.pdf": "%PDF-1.4 c", "A/same.txt": "alpha",
		"B/a.txt": "alpha two", "B/e.txt": "beta", "B/deep/x/f.csv": "f,1\n", "B/g.txt": "gamma",
	} {
		must(t, testfix.File(filepath.Join(dir, filepath.FromSlash(rel)), body, mtime))
	}
	return a, b
}

func TestCrashMerge(t *testing.T) {
	crashBinary(t)
	ref := testfix.TempDir(t)
	a, b := crashInputs(t, ref)
	merge := func(n int, a, b, o string) bool {
		stopped, _ := runAt(t, n, "merge", "-input", a, "-input", b, "-output", o, "-execute", "-copy-j", "1")
		return stopped
	}
	merge(0, a, b, filepath.Join(ref, "O"))
	want, _ := treeOf(t, filepath.Join(ref, "O"))
	if len(want) != 6 {
		t.Fatalf("reference merge: %v", want)
	}
	points := everyPoint(t, func(n int, dir string) bool {
		a, b := crashInputs(t, dir)
		o := filepath.Join(dir, "O")
		before, _ := treeOf(t, dir)
		if !merge(n, a, b, o) {
			return false
		}
		merge(0, a, b, o)
		got, temps := treeOf(t, o)
		sameTree(t, "merge output", n, got, want)
		if len(temps) > 0 {
			t.Fatalf("crash point %d: temporary files left: %v", n, temps)
		}
		all, _ := treeOf(t, dir)
		for k, v := range before {
			if all[k] != v {
				t.Fatalf("crash point %d: input %s changed", n, k)
			}
		}
		return true
	})
	t.Logf("merge: %d crash points", points)
	if points < 10 {
		t.Fatalf("only %d crash points: the hook is not reached", points)
	}
}

// rearrangeTree writes a tree to rearrange: files to rename and move (two of
// them trading places is not possible with names from a template, so
// folders collapse instead), a confirmed duplicate to delete, a file already
// in place.
func rearrangeTree(t *testing.T, root string) {
	t.Helper()
	for rel, body := range map[string]string{
		"TXT/2020/[Desktop] note -- frev.1.txt": "note",
		"MP4/clip.mp4":                          "clip",
		"OLD/clip copy.mp4":                     "clip",
		"misc/report.pdf":                       "%PDF-1.4 report",
		"misc/deep/data.csv":                    "x,y\n",
		"TXT/2019/kept.txt":                     "kept",
	} {
		must(t, testfix.File(filepath.Join(root, filepath.FromSlash(rel)), body, mtime))
	}
}

func TestCrashRearrange(t *testing.T) {
	crashBinary(t)
	ref := filepath.Join(testfix.TempDir(t), "R")
	rearrangeTree(t, ref)
	runAt(t, 0, "rearrange", "-execute", ref)
	want, _ := treeOf(t, ref)
	if len(want) != 5 {
		t.Fatalf("reference rearrange: %v", want)
	}
	points := everyPoint(t, func(n int, dir string) bool {
		root := filepath.Join(dir, "R")
		rearrangeTree(t, root)
		if stopped, _ := runAt(t, n, "rearrange", "-execute", root); !stopped {
			return false
		}
		journals, _ := filepath.Glob(filepath.Join(root, ".onefold", "rearrange-*.jsonl"))
		if len(journals) > 1 {
			t.Fatalf("crash point %d: %d journals", n, len(journals))
		}
		if len(journals) == 1 {
			// what the person is told to do after an interrupted run
			cmd := exec.Command(crashBinary(t), "rearrange", "-resume", journals[0])
			cmd.Env = append(os.Environ(), "ONEFOLD_NO_EXTERNAL=1", "ONEFOLD_NO_HASH_CACHE=1")
			if out, err := cmd.CombinedOutput(); err != nil {
				if !strings.Contains(string(out), "no header") {
					t.Fatalf("crash point %d: resume: %v\n%s", n, err, out)
				}
				// stopped while writing the journal's first line: nothing was
				// changed, a new run starts over
			}
		}
		runAt(t, 0, "rearrange", "-execute", root)
		got, temps := treeOf(t, root)
		sameTree(t, "rearranged tree", n, got, want)
		if len(temps) > 0 {
			t.Fatalf("crash point %d: in-flight files left: %v", n, temps)
		}
		return true
	})
	t.Logf("rearrange: %d crash points", points)
	if points < 10 {
		t.Fatalf("only %d crash points: the hook is not reached", points)
	}
}

var toDelete = regexp.MustCompile(`to delete:\s+(\d+) files`)

// inputsOf is the tree of the two input folders of crashInputs.
func inputsOf(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, in := range []string{"A", "B"} {
		tr, _ := treeOf(t, filepath.Join(dir, in))
		for k, v := range tr {
			out[in+"/"+k] = v
		}
	}
	return out
}

func TestCrashPurge(t *testing.T) {
	crashBinary(t)
	setup := func(dir string) (report, o string) {
		a, b := crashInputs(t, dir)
		o, report = filepath.Join(dir, "O"), filepath.Join(dir, "r.json")
		runAt(t, 0, "merge", "-input", a, "-input", b, "-output", o, "-execute", "-report", report)
		return report, o
	}
	purge := func(n int, report string) bool {
		_, out := runAt(t, 0, "purge-sources", "-from", report)
		m := toDelete.FindStringSubmatch(out)
		if m == nil {
			t.Fatalf("purge dry run:\n%s", out)
		}
		if m[1] == "0" {
			return false
		}
		stopped, _ := runAt(t, n, "purge-sources", "-from", report, "-execute", "-expect", m[1])
		return stopped
	}
	ref := testfix.TempDir(t)
	report, o := setup(ref)
	merged, _ := treeOf(t, o)
	purge(0, report)
	left := inputsOf(t, ref)
	points := everyPoint(t, func(n int, dir string) bool {
		report, o := setup(dir)
		if !purge(n, report) {
			return false
		}
		purge(0, report)
		got, temps := treeOf(t, o)
		sameTree(t, "merge output", n, got, merged)
		sameTree(t, "inputs", n, inputsOf(t, dir), left)
		if len(temps) > 0 {
			t.Fatalf("crash point %d: temporary files left: %v", n, temps)
		}
		return true
	})
	t.Logf("purge-sources: %d crash points", points)
	if points < 10 {
		t.Fatalf("only %d crash points: the hook is not reached", points)
	}
}

func TestCrashApply(t *testing.T) {
	crashBinary(t)
	ref := testfix.TempDir(t)
	src := filepath.Join(ref, "src")
	for rel, body := range map[string]string{"notes.txt": "alpha", "sub/r.pdf": "%PDF-1.4 r", "copy.txt": "alpha", "t.csv": "a,b\n", "x/y/z.txt": "zeta"} {
		must(t, testfix.File(filepath.Join(src, filepath.FromSlash(rel)), body, mtime))
	}
	m, p := filepath.Join(ref, "m.jsonl"), filepath.Join(ref, "p.jsonl")
	runAt(t, 0, "catalog", "-preset", "all", "-allow-mixed", "-host", "pc", "-o", m, src)
	runAt(t, 0, "plan", "-o", p, "-layout", "document", m)
	before, _ := treeOf(t, src)
	runAt(t, 0, "apply", "-execute", "-target", filepath.Join(ref, "G"), p)
	want, _ := treeOf(t, filepath.Join(ref, "G"))
	if len(want) != 4 {
		t.Fatalf("reference apply: %v", want)
	}
	points := everyPoint(t, func(n int, dir string) bool {
		g := filepath.Join(dir, "G")
		if stopped, _ := runAt(t, n, "apply", "-execute", "-j", "1", "-target", g, p); !stopped {
			return false
		}
		runAt(t, 0, "apply", "-execute", "-target", g, p)
		got, temps := treeOf(t, g)
		sameTree(t, "golden tree", n, got, want)
		if len(temps) > 0 {
			t.Fatalf("crash point %d: temporary files left: %v", n, temps)
		}
		return true
	})
	now, _ := treeOf(t, src)
	sameTree(t, "apply sources", 0, now, before)
	t.Logf("apply: %d crash points", points)
	if points < 8 {
		t.Fatalf("only %d crash points: the hook is not reached", points)
	}
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package dedupe

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mauriziofonte/onefold/internal/fsx"
	"github.com/mauriziofonte/onefold/internal/purge"
	"github.com/mauriziofonte/onefold/internal/sparsehash"
	"github.com/mauriziofonte/onefold/internal/testfix"
)

var (
	t2010 = time.Date(2010, 1, 1, 10, 0, 0, 0, time.Local)
	t2015 = time.Date(2015, 1, 1, 10, 0, 0, 0, time.Local)
	t2020 = time.Date(2020, 1, 1, 10, 0, 0, 0, time.Local)
)

// tree: one group of three copies (A/deep/x.jpg oldest, B/x.jpg newest,
// A/x.jpg shortest in A), one pair (A/doc.txt, B/doc.txt), one unique file.
func tree(t *testing.T, root string) (A, B string) {
	A, B = filepath.Join(root, "A"), filepath.Join(root, "B")
	photo := strings.Repeat("photo ", 2000)
	for _, f := range []struct {
		p, body string
		mt      time.Time
	}{
		{filepath.Join(A, "deep", "dir", "x.jpg"), photo, t2010},
		{filepath.Join(B, "x.jpg"), photo, t2020},
		{filepath.Join(A, "x.jpg"), photo, t2015},
		{filepath.Join(A, "doc.txt"), "the document", t2015},
		{filepath.Join(B, "doc.txt"), "the document", t2010},
		{filepath.Join(B, "unique.txt"), "only here", t2010},
	} {
		if err := testfix.File(f.p, f.body, f.mt); err != nil {
			t.Fatal(err)
		}
	}
	return
}

func keeps(res *Result) map[string]string {
	m := map[string]string{}
	for _, it := range res.Items {
		m[it.Extra] = it.Keep
	}
	return m
}

func TestPlanKeepRules(t *testing.T) {
	root := testfix.TempDir(t)
	A, B := tree(t, root)
	deep := filepath.Join(A, "deep", "dir", "x.jpg")
	for _, c := range []struct {
		keep, keepIn string
		photo, doc   string // the kept copies
		items        int
	}{
		{KeepOldest, "", deep, filepath.Join(B, "doc.txt"), 3},
		{KeepNewest, "", filepath.Join(B, "x.jpg"), filepath.Join(A, "doc.txt"), 3},
		{KeepShortest, "", filepath.Join(A, "x.jpg"), filepath.Join(B, "doc.txt"), 3}, // a tie on length: the older
		{KeepFirst, "", filepath.Join(A, "x.jpg"), filepath.Join(A, "doc.txt"), 3},
		{KeepOldest, B, filepath.Join(B, "x.jpg"), filepath.Join(B, "doc.txt"), 3},
		{KeepOldest, filepath.Join(A, "deep"), deep, "", 2}, // the doc pair has no copy there: left alone
	} {
		res, err := Plan(Options{Roots: []string{A, B}, Keep: c.keep, KeepIn: c.keepIn, Action: ActLink})
		if err != nil {
			t.Fatal(err)
		}
		k := keeps(res)
		if len(res.Items) != c.items || res.Groups != (c.items+1)/2 {
			t.Fatalf("%s %s: %d items in %d groups: %+v", c.keep, c.keepIn, len(res.Items), res.Groups, res.Items)
		}
		for extra, kept := range k {
			want := c.photo
			if strings.HasSuffix(extra, "doc.txt") {
				want = c.doc
			}
			if kept != want || extra == kept {
				t.Errorf("%s %s: %s keeps %s, want %s", c.keep, c.keepIn, extra, kept, want)
			}
		}
		if c.doc == "" && len(res.Skipped) != 2 {
			t.Errorf("skipped %+v", res.Skipped)
		}
	}
	if _, err := Plan(Options{Roots: []string{A}, Keep: "biggest", Action: ActLink}); err == nil {
		t.Error("an unknown keep rule was accepted")
	}
	if _, err := Plan(Options{Roots: []string{A}}); err == nil {
		t.Error("no action was accepted")
	}
}

func sameFile(t *testing.T, a, b string) bool {
	fa, err1 := os.Stat(a)
	fb, err2 := os.Stat(b)
	if err1 != nil || err2 != nil {
		t.Fatalf("stat: %v %v", err1, err2)
	}
	return os.SameFile(fa, fb)
}

func noLeftovers(t *testing.T, root string) {
	t.Helper()
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && (strings.HasSuffix(p, OldSuffix) || fsx.IsTemp(d.Name())) {
			t.Errorf("left behind: %s", p)
		}
		return nil
	})
}

func TestExecuteLink(t *testing.T) {
	root := testfix.TempDir(t)
	A, B := tree(t, root)
	o := Options{Roots: []string{A, B}, Keep: KeepShortest, Action: ActLink, JournalDir: filepath.Join(root, "j"), Expect: 2}
	o.Execute = true
	if _, err := Run(o); err == nil {
		t.Fatal("a wrong typed count was accepted")
	}
	if sameFile(t, filepath.Join(A, "x.jpg"), filepath.Join(B, "x.jpg")) {
		t.Fatal("a refused run changed files")
	}
	o.Expect = 3
	res, err := Run(o)
	if err != nil {
		t.Fatal(err)
	}
	if res.Done != 3 || len(res.Failed) != 0 || res.Journal == "" {
		t.Fatalf("done %d failed %+v journal %q", res.Done, res.Failed, res.Journal)
	}
	for extra, kept := range keeps(res) {
		if !sameFile(t, extra, kept) {
			t.Errorf("%s is not a link of %s", extra, kept)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(B, "unique.txt")); string(b) != "only here" {
		t.Error("a unique file changed")
	}
	noLeftovers(t, root)
	j, _ := os.ReadFile(res.Journal)
	if n := strings.Count(string(j), `"op":"done"`); n != 3 || !strings.Contains(string(j), `"op":"start"`) {
		t.Errorf("journal: %d done\n%s", n, j)
	}
	again, err := Run(Options{Roots: []string{A, B}, Action: ActLink, JournalDir: o.JournalDir, Expect: -1, Execute: true})
	if err != nil || len(again.Items) != 0 {
		t.Fatalf("a second run still finds %d copies (%v)", len(again.Items), err)
	}
}

// A copy that changed after the preview, or whose kept copy cannot be
// flushed, is left as it is.
func TestExecuteRefusals(t *testing.T) {
	root := testfix.TempDir(t)
	A, B := tree(t, root)
	o := Options{Roots: []string{A, B}, Keep: KeepShortest, Action: ActLink, JournalDir: filepath.Join(root, "j"), Expect: -1, Execute: true}
	res, err := Plan(o)
	if err != nil {
		t.Fatal(err)
	}
	extra := res.Items[0]
	if err := os.WriteFile(extra.Extra, bytes.Repeat([]byte("Z"), int(extra.Size)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := act(ActLink, extra, nil, &fsx.DirSet{}); err == nil || !strings.Contains(err.Error(), "NOT touched") {
		t.Fatalf("a changed copy: %v", err)
	}
	if sameFile(t, extra.Extra, extra.Keep) {
		t.Fatal("a changed copy was replaced")
	}
	defer func(f func(string, *fsx.DirSet) error) { durable = f }(durable)
	durable = func(string, *fsx.DirSet) error { return errors.New("injected") }
	r2, err := Run(o)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Done != 0 || len(r2.Failed) != len(r2.Items) {
		t.Fatalf("done %d failed %d of %d", r2.Done, len(r2.Failed), len(r2.Items))
	}
}

func TestExecuteClone(t *testing.T) {
	root := testfix.CloneDir(t)
	if root == "" {
		t.Skip("no clone-capable file system (ONEFOLD_CLONE_DIR names one)")
	}
	A, B := tree(t, root)
	res, err := Run(Options{Roots: []string{A, B}, Keep: KeepOldest, Action: ActClone, JournalDir: filepath.Join(root, "j"), Expect: 3, Execute: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Done != 3 || len(res.Failed) != 0 {
		t.Fatalf("done %d failed %+v", res.Done, res.Failed)
	}
	for _, it := range res.Items {
		fi, _ := os.Stat(it.Extra)
		if sameFile(t, it.Extra, it.Keep) {
			t.Errorf("%s became a hard link, not a clone", it.Extra)
		}
		if h, _ := sparsehash.FullFile(it.Extra); h != it.Content {
			t.Errorf("%s: content changed", it.Extra)
		}
		if strings.HasSuffix(it.Extra, filepath.Join("B", "x.jpg")) && !fi.ModTime().Equal(t2020) {
			t.Errorf("%s: mtime %v, want its own %v", it.Extra, fi.ModTime(), t2020)
		}
	}
	noLeftovers(t, root)
}

func TestScript(t *testing.T) {
	root := testfix.TempDir(t)
	A, B := tree(t, root)
	res, err := Run(Options{Roots: []string{A, B}, Keep: KeepShortest, Action: ActScript})
	if err != nil {
		t.Fatal(err)
	}
	var sh, ps bytes.Buffer
	if err := Script(&sh, res, false); err != nil {
		t.Fatal(err)
	}
	if err := Script(&ps, res, true); err != nil {
		t.Fatal(err)
	}
	if strings.Count(sh.String(), "\nrm -- '") != 3 || strings.Count(ps.String(), "Remove-Item -LiteralPath '") != 3 {
		t.Fatalf("scripts:\n%s\n%s", sh.String(), ps.String())
	}
	if !strings.Contains(sh.String(), filepath.Join(B, "x.jpg")) || strings.Contains(sh.String(), "unique.txt") {
		t.Fatalf("sh script:\n%s", sh.String())
	}
	for _, p := range []string{filepath.Join(B, "x.jpg"), filepath.Join(B, "doc.txt")} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("the script action deleted %s", p)
		}
	}
}

// An interrupted replacement: the original waits under its .onefold-dd
// name. Resume puts it back when the name is empty, removes it when the
// name holds the replacement.
func TestResume(t *testing.T) {
	root := testfix.TempDir(t)
	keep := filepath.Join(root, "keep.bin")
	body := "the content"
	if err := testfix.File(keep, body, t2010); err != nil {
		t.Fatal(err)
	}
	content, _ := sparsehash.FullFile(keep)
	j := &journal{path: filepath.Join(root, "dedupe-test.jsonl")}
	f, err := os.Create(j.path)
	if err != nil {
		t.Fatal(err)
	}
	j.f = f
	mk := func(name string, withNew bool) (extra, old string) {
		extra = filepath.Join(root, name)
		old = oldName(extra)
		if err := testfix.File(old, body, t2015); err != nil {
			t.Fatal(err)
		}
		if withNew {
			if err := os.Link(keep, extra); err != nil {
				t.Fatal(err)
			}
		}
		j.write(entry{Op: "replace", Action: ActLink, Extra: extra, Keep: keep, Content: content, Old: old, Tmp: fsx.TempFor(extra, false)})
		return
	}
	e1, o1 := mk("a.bin", false) // set aside, not yet replaced
	e2, o2 := mk("b.bin", true)  // replaced, original not yet removed
	j.close()
	fin, rest, problems, err := Resume(j.path)
	if err != nil || fin != 1 || rest != 1 || len(problems) != 0 {
		t.Fatalf("finished %d restored %d problems %v err %v", fin, rest, problems, err)
	}
	if b, _ := os.ReadFile(e1); string(b) != body {
		t.Error("a.bin not put back")
	}
	if !sameFile(t, e2, keep) {
		t.Error("b.bin lost its replacement")
	}
	for _, o := range []string{o1, o2} {
		if _, err := os.Lstat(o); err == nil {
			t.Errorf("%s left", o)
		}
	}
}

// The Trash action: the extra copies leave their folders, the kept ones and
// the unique file stay.
func TestTrashCopies(t *testing.T) {
	if os.Getenv("ONEFOLD_TEST_TRASH") == "" {
		t.Skip("writes into the Recycle Bin or the Trash: set ONEFOLD_TEST_TRASH=1")
	}
	t.Setenv("XDG_DATA_HOME", testfix.TempDir(t)) // Linux: a Trash of its own
	root := testfix.TempDir(t)
	A, B := tree(t, root)
	res, err := Run(Options{Roots: []string{A, B}, Keep: KeepNewest, Action: ActTrash, JournalDir: filepath.Join(root, "j"), Expect: 3, Execute: true})
	if errors.Is(err, purge.ErrNoTrash) {
		t.Skipf("no Trash on the test volume: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if res.Done != 3 || len(res.Failed) != 0 {
		t.Fatalf("done %d failed %+v", res.Done, res.Failed)
	}
	for extra, kept := range keeps(res) {
		if _, err := os.Lstat(extra); err == nil {
			t.Errorf("%s still in place", extra)
		}
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("kept copy %s: %v", kept, err)
		}
	}
	if _, err := os.Stat(filepath.Join(B, "unique.txt")); err != nil {
		t.Error("the unique file went to the Trash")
	}
}

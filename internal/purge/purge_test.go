// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package purge

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/merge"
	"github.com/mauriziofonte/onefold/internal/sparsehash"
)

var t0 = time.Date(2015, 6, 6, 10, 0, 0, 0, time.Local)

func write(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, t0, t0); err != nil {
		t.Fatal(err)
	}
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

// snap hashes every file under root except the onefold state dir.
func snap(t *testing.T, root string) map[string]string {
	t.Helper()
	m := map[string]string{}
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.Contains(p, string(filepath.Separator)+merge.CacheDir+string(filepath.Separator)) {
			return err
		}
		b, _ := os.ReadFile(p)
		h := sha256.Sum256(b)
		rel, _ := filepath.Rel(root, p)
		m[filepath.ToSlash(rel)] = hex.EncodeToString(h[:8])
		return nil
	})
	return m
}

// mergeDesktop merges a Desktop-like input into O and returns the result.
func mergeDesktop(t *testing.T, root string) (A, O string, r *merge.Result) {
	t.Helper()
	A, O = filepath.Join(root, "Desktop"), filepath.Join(root, "O")
	write(t, filepath.Join(O, "TXT", "old.txt"), "already there")
	write(t, filepath.Join(A, "old copy.txt"), "already there")
	write(t, filepath.Join(A, "Projects", "Alpha", "spec.txt"), "alpha spec")
	write(t, filepath.Join(A, "Projects", "Alpha", "desktop.ini"), "[.ShellClassInfo]")
	write(t, filepath.Join(A, "Projects", "Alpha", "Deep", "dup.txt"), "alpha spec")
	write(t, filepath.Join(A, "Projects", "Beta", "notes.txt"), "beta notes")
	write(t, filepath.Join(A, "Projects", "Beta", "~$notes.docx"), "lock") // transient: excluded, stays
	write(t, filepath.Join(A, "desktop.ini"), "[.ShellClassInfo]")
	write(t, filepath.Join(O, "BIN"), "a FILE where a directory is needed")
	write(t, filepath.Join(A, "Stuff", "blocked.bin"), "blocked content")
	write(t, filepath.Join(A, "later.txt"), "changes after the merge")
	o := merge.Options{Inputs: []string{A}, Output: O, Execute: true, Workers: 2, Naming: "{EXT}/{stem}<.{ext}>",
		Catalog: catalog.Options{Workers: 2, Coverage: sparsehash.DefaultCoverage, Sample: sparsehash.DefaultSampleSize, SkipTransient: true}}
	r, err := merge.Run(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Blocked) != 1 {
		t.Fatalf("blocked %v", r.Blocked)
	}
	return A, O, r
}

func TestPurge(t *testing.T) {
	root := t.TempDir()
	A, O, r := mergeDesktop(t, root)
	write(t, filepath.Join(A, "later.txt"), "changed after the merge") // must survive: content differs now
	outBefore := snap(t, O)
	o := Options{Items: r.Settled, Output: O, Rooms: r.Rooms[1:], Expect: -1}

	// dry run: the plan, nothing touched
	p, err := Run(o)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Candidates) != 5 || p.Journal != "" { // old copy, spec, dup, notes, later
		t.Fatalf("candidates %d %+v", len(p.Candidates), p.Candidates)
	}
	if !exists(filepath.Join(A, "old copy.txt")) {
		t.Fatal("dry run deleted")
	}

	// a wrong confirmation touches nothing
	o.Execute, o.Expect = true, 4
	if _, err := Run(o); err == nil || !exists(filepath.Join(A, "old copy.txt")) {
		t.Fatalf("expect mismatch: %v", err)
	}
	o.Expect = 5
	res, err := Run(o)
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 4 || len(res.Failed) != 1 || !strings.Contains(res.Failed[0], "later.txt") {
		t.Fatalf("deleted %d failed %v", res.Deleted, res.Failed)
	}
	for _, gone := range []string{"old copy.txt", "Projects/Alpha", "Projects/Beta/notes.txt"} {
		if exists(filepath.Join(A, filepath.FromSlash(gone))) {
			t.Errorf("%s still there", gone)
		}
	}
	for _, kept := range []string{"desktop.ini", "Stuff/blocked.bin", "later.txt", "Projects/Beta/~$notes.docx"} {
		if !exists(filepath.Join(A, filepath.FromSlash(kept))) {
			t.Errorf("%s deleted", kept)
		}
	}
	// Alpha held only desktop.ini once emptied: removed with it; Projects
	// keeps Beta (its excluded lock file); the room root stays
	if res.JunkRemoved != 1 || res.DirsRemoved != 2 { // Alpha/Deep, Alpha
		t.Errorf("junk %d dirs %d", res.JunkRemoved, res.DirsRemoved)
	}
	if !exists(A) || !exists(filepath.Join(A, "Projects", "Beta")) {
		t.Error("room root or a non-empty dir removed")
	}
	if after := snap(t, O); len(after) != len(outBefore) {
		t.Errorf("output changed: %d -> %d files", len(outBefore), len(after))
	} else {
		for k, v := range outBefore {
			if after[k] != v {
				t.Errorf("output file %s changed", k)
			}
		}
	}
	// the journal records every deletion
	f, err := os.Open(res.Journal)
	if err != nil {
		t.Fatal(err)
	}
	ops := map[string]int{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var op string
		if i := strings.Index(sc.Text(), `"op":"`); i >= 0 {
			op = strings.SplitN(sc.Text()[i+6:], `"`, 2)[0]
		}
		ops[op]++
	}
	f.Close()
	if ops["start"] != 1 || ops["delete"] != 4 || ops["fail"] != 1 || ops["junk"] != 1 || ops["rmdir"] != 2 || ops["end"] != 1 {
		t.Errorf("journal ops %v", ops)
	}

	// running it again (resume) finds the deleted files gone
	o.Expect = -1
	again, err := Run(o)
	if err != nil {
		t.Fatal(err)
	}
	if again.Gone != 4 || again.Deleted != 0 || len(again.Failed) != 1 {
		t.Errorf("again: gone %d deleted %d failed %v", again.Gone, again.Deleted, again.Failed)
	}
}

// Items outside the input folders, inside the output, or whose keeper is not
// in the output are refused, whatever the list says.
func TestPurgeRefusals(t *testing.T) {
	root := t.TempDir()
	A, O, r := mergeDesktop(t, root)
	elsewhere := filepath.Join(root, "Elsewhere", "x.txt")
	write(t, elsewhere, "alpha spec")
	keeper := r.Settled[0].Keeper
	items := []merge.Settle{
		{Src: elsewhere, Keeper: keeper, Size: 10},
		{Src: keeper, Keeper: keeper, Size: 10},
		{Src: filepath.Join(A, "later.txt"), Keeper: elsewhere, Size: 10},
		{Src: A, Keeper: keeper, Size: 10},
		{Src: "relative.txt", Keeper: keeper, Size: 10},
	}
	res, err := Run(Options{Items: items, Output: O, Rooms: r.Rooms[1:], Execute: true, Expect: -1})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Candidates) != 0 || len(res.Refused) != len(items) || res.Deleted != 0 {
		t.Fatalf("candidates %d refused %+v", len(res.Candidates), res.Refused)
	}
	if !exists(elsewhere) || !exists(keeper) {
		t.Fatal("a refused file was deleted")
	}
}

// The output inside an input room (pruned from it by merge): neither the
// output nor a directory holding it is ever removed.
func TestPurgeOutputInsideInput(t *testing.T) {
	root := t.TempDir()
	A := filepath.Join(root, "A")
	O := filepath.Join(A, "Archive", "OUT")
	write(t, filepath.Join(A, "Archive", "a.txt"), "content a")
	o := merge.Options{Inputs: []string{A}, Output: O, Execute: true, Workers: 2, Naming: "{stem}<.{ext}>",
		Catalog: catalog.Options{Workers: 2, Coverage: sparsehash.DefaultCoverage, Sample: sparsehash.DefaultSampleSize}}
	r, err := merge.Run(o)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(Options{Items: r.Settled, Output: O, Rooms: r.Rooms[1:], Execute: true, Expect: 1})
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 1 || res.DirsRemoved != 0 || !exists(filepath.Join(O, "a.txt")) || !exists(filepath.Join(A, "Archive")) {
		t.Fatalf("deleted %d dirs %d", res.Deleted, res.DirsRemoved)
	}
}

func TestPurgeReport(t *testing.T) {
	root := t.TempDir()
	_, O, r := mergeDesktop(t, root)
	p := filepath.Join(root, "report.json")
	b, _ := jsonMarshal(r)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	o, err := ReadReport(p)
	if err != nil {
		t.Fatal(err)
	}
	if o.Output != O || len(o.Rooms) != 1 || len(o.Items) != len(r.Settled) {
		t.Fatalf("%+v", o)
	}
}

func TestTrashOption(t *testing.T) {
	if runtime.GOOS == "windows" {
		if os.Getenv("ONEFOLD_TEST_TRASH") == "" {
			t.Skip("writes into the Recycle Bin: set ONEFOLD_TEST_TRASH=1")
		}
		root := t.TempDir()
		A, O, r := mergeDesktop(t, root)
		res, err := Run(Options{Items: r.Settled, Output: O, Rooms: r.Rooms[1:], Execute: true, Expect: -1, Trash: true})
		if err != nil {
			t.Fatal(err)
		}
		if res.Deleted != 5 || exists(filepath.Join(A, "old copy.txt")) {
			t.Fatalf("trash: deleted %d failed %v", res.Deleted, res.Failed)
		}
		return
	}
	if _, err := Plan(Options{Output: t.TempDir(), Trash: true}); !errors.Is(err, ErrTrashUnsupported) {
		t.Fatalf("trash on %s: %v", runtime.GOOS, err)
	}
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

// No journal, no deletion: a journal that stops being writable stops the purge.
func TestPurgeNeedsJournal(t *testing.T) {
	root := t.TempDir()
	_, O, r := mergeDesktop(t, root)
	res, err := Plan(Options{Items: r.Settled, Output: O, Rooms: r.Rooms[1:]})
	if err != nil {
		t.Fatal(err)
	}
	j, err := openJournal(res, false)
	if err != nil {
		t.Fatal(err)
	}
	j.f.Close() // every later write fails
	j.write(entry{Op: "probe"})
	deleted := remove(Options{Workers: 2}, res, j)
	if len(deleted) != 0 || res.Deleted != 0 || len(res.Failed) != len(res.Candidates) {
		t.Fatalf("deleted %v, failed %d of %d", deleted, len(res.Failed), len(res.Candidates))
	}
	for _, c := range res.Candidates {
		if !exists(c.Src) {
			t.Fatalf("%s deleted without a journal", c.Src)
		}
	}
}

// The Recycle Bin option is refused before anything is touched on a volume
// without a Recycle Bin, where Windows would delete for good.
func TestNoTrashReason(t *testing.T) {
	for _, c := range []struct {
		unc  bool
		dt   uint32
		want bool
	}{{false, driveFixed, false}, {true, driveFixed, true}, {false, driveRemote, true}, {false, driveRemovable, true},
		{false, driveCDROM, true}, {false, driveRAMDisk, true}, {false, driveUnknown, true}, {false, driveNoRootDir, true}} {
		if got := noTrashReason(c.unc, c.dt) != ""; got != c.want {
			t.Errorf("unc=%v type=%d: refused=%v, want %v", c.unc, c.dt, got, c.want)
		}
	}
	if runtime.GOOS != "windows" {
		return
	}
	if err := checkTrash([]string{t.TempDir()}); err != nil {
		t.Fatalf("local temp dir: %v", err)
	}
	share := `\\server\share\Desktop`
	if err := checkTrash([]string{t.TempDir(), share}); !errors.Is(err, ErrNoTrash) {
		t.Fatalf("network share: %v", err)
	}
	root := t.TempDir()
	_, O, r := mergeDesktop(t, root)
	if _, err := Plan(Options{Items: r.Settled, Output: O, Rooms: []string{share}, Trash: true}); !errors.Is(err, ErrNoTrash) {
		t.Fatalf("plan with a room on a share: %v", err)
	}
	if _, err := Plan(Options{Items: r.Settled, Output: O, Rooms: r.Rooms[1:], Trash: true}); err != nil {
		t.Fatalf("plan on a local drive: %v", err)
	}
}

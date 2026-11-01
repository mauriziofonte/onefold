// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package rearrange

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/mauriziofonte/onefold/internal/merge"
	"github.com/mauriziofonte/onefold/internal/runctl"
)

func journals(t *testing.T, root string) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(root, merge.CacheDir, "rearrange-*.jsonl"))
	return m
}

// A refused plan writes no journal and touches nothing.
func TestBeforeExecuteRefusal(t *testing.T) {
	root := scenario(t)
	before := tree(t, root)
	o := opts(root, true)
	refuse := errors.New("plan changed")
	o.BeforeExecute = func(*Result) error { return refuse }
	if _, err := Run(o); !errors.Is(err, refuse) {
		t.Fatalf("want refusal, got %v", err)
	}
	if !mapsEq(tree(t, root), before) || len(journals(t, root)) != 0 {
		t.Fatal("a refused plan touched the tree or wrote a journal")
	}
}

// Stopped right after the journal: nothing deleted or moved, the journal
// holds the whole run; RecoverWith completes deletions, moves and cleanup and
// the tree ends exactly as a straight run.
func TestStopThenRecoverCompletesRun(t *testing.T) {
	root := scenario(t)
	before := tree(t, root)
	o := opts(root, true)
	g := runctl.New(nil)
	o.Gate = g
	o.BeforeExecute = func(*Result) error { g.Stop(); return nil }
	r, err := Run(o)
	must(t, err)
	if !r.Stopped || r.Deleted != 0 || r.Moved != 0 {
		t.Fatalf("stopped run: %+v", r)
	}
	if !mapsEq(tree(t, root), before) {
		t.Fatal("stopped run changed files")
	}
	js := journals(t, root)
	if len(js) != 1 {
		t.Fatalf("journals: %v", js)
	}
	// a new run is allowed (no *.onefold-mv in flight) but resume is the right path
	rr, err := RecoverWith(js[0], false, RecoverOptions{})
	must(t, err)
	if rr.Failed != 0 || rr.Deleted != 1 || rr.Moved != len(r.Moves) {
		t.Fatalf("recover: %+v (moves %d)", rr, len(r.Moves))
	}
	var got []string
	for k := range tree(t, root) {
		got = append(got, k)
	}
	sort.Strings(got)
	want := append([]string(nil), wantAfter...)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("after recover:\n got %v\nwant %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("after recover:\n got %v\nwant %v", got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "OLD")); !os.IsNotExist(err) {
		t.Fatal("folder emptied by the run not removed on recover")
	}
	// a second recover is a no-op
	rr2, err := RecoverWith(js[0], false, RecoverOptions{})
	must(t, err)
	if rr2.Moved != 0 || rr2.Deleted != 0 || rr2.Failed != 0 {
		t.Fatalf("second recover: %+v", rr2)
	}
}

// A stopped gate during recovery parks nothing new; recovering again finishes.
func TestRecoverStopIsResumable(t *testing.T) {
	root := scenario(t)
	o := opts(root, true)
	g := runctl.New(nil)
	o.Gate = g
	o.BeforeExecute = func(*Result) error { g.Stop(); return nil }
	r, err := Run(o)
	must(t, err)
	js := journals(t, root)
	stop := runctl.New(nil)
	stop.Stop()
	rr, err := RecoverWith(js[0], false, RecoverOptions{Gate: stop})
	must(t, err)
	if !rr.Stopped || rr.Moved != 0 || rr.Deleted != 0 {
		t.Fatalf("stopped recover: %+v", rr)
	}
	rr, err = RecoverWith(js[0], false, RecoverOptions{})
	must(t, err)
	if rr.Moved != len(r.Moves) || rr.Deleted != 1 {
		t.Fatalf("final recover: %+v", rr)
	}
}

// Empty files are identified by their name (merge -keep-dotfiles): rearrange
// refuses that scope before touching anything, or two __init__.py would be
// "duplicates".
func TestRefusesKeepEmpty(t *testing.T) {
	root := t.TempDir()
	o := Options{Root: root}
	o.Catalog.KeepEmpty = true
	if _, err := Run(o); err == nil {
		t.Fatal("rearrange accepted the empty-file scope")
	}
}

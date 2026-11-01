// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package merge

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mauriziofonte/onefold/internal/runctl"
	"github.com/mauriziofonte/onefold/internal/testfix"
)

// A preview with a StateCache writes nothing into the output (not even
// .onefold) and leaves the cache in the state location.
func TestStateCachePreviewWritesNothing(t *testing.T) {
	O, B, C := scenario(t)
	before := snap(t, O)
	state := filepath.Join(testfix.TempDir(t), "job", "cache.jsonl")
	o := opts([]string{O, B, C}, O, false)
	o.StateCache = state
	run(t, o)
	if _, err := os.Stat(filepath.Join(O, CacheDir)); !os.IsNotExist(err) {
		t.Fatal("preview created the output cache dir")
	}
	if _, err := os.Stat(state); err != nil {
		t.Fatalf("state cache not written: %v", err)
	}
	after := snap(t, O)
	if len(after) != len(before) {
		t.Fatal("preview changed the output")
	}
	// execute with the same state cache: same plan, complete merge
	o.Execute = true
	r := run(t, o)
	if r.Missing() != 1 || len(r.Blocked) != 0 && r.Missing() != len(r.Blocked) {
		t.Logf("missing %d blocked %d", r.Missing(), len(r.Blocked))
	}
	if len(r.Failed) != 0 || r.Pending != 0 || r.Copied != len(r.Copies) {
		t.Fatalf("execute after cached preview: %+v", r)
	}
}

// BeforeExecute refusing the plan aborts with nothing written.
func TestBeforeExecuteRefusalWritesNothing(t *testing.T) {
	O, B, C := scenario(t)
	before := snap(t, O)
	o := opts([]string{O, B, C}, O, true)
	refuse := errors.New("plan differs from the confirmed one")
	o.BeforeExecute = func(r *Result) error {
		if len(r.Copies) == 0 {
			t.Fatal("BeforeExecute must see the plan")
		}
		return refuse
	}
	if _, err := Run(o); !errors.Is(err, refuse) {
		t.Fatalf("want refusal, got %v", err)
	}
	after := snap(t, O)
	for k, v := range before {
		if after[k] != v {
			t.Fatalf("output changed: %s", k)
		}
	}
	if len(after) != len(before) {
		t.Fatalf("files added: %d -> %d", len(before), len(after))
	}
}

// A stopped gate leaves every copy pending (nothing half-written); a new run
// with the same options completes the merge.
func TestStopThenResume(t *testing.T) {
	O, B, C := scenario(t)
	o := opts([]string{O, B, C}, O, true)
	g := runctl.New(nil)
	o.Gate = g
	o.BeforeExecute = func(*Result) error { g.Stop(); return nil }
	r := run(t, o)
	if !r.Stopped || r.Copied != 0 || r.Pending != len(r.Copies) || r.Missing() < r.Pending {
		t.Fatalf("stopped run: copied %d pending %d of %d", r.Copied, r.Pending, len(r.Copies))
	}
	r2 := run(t, opts([]string{O, B, C}, O, true))
	if r2.Copied != len(r.Copies) || r2.Pending != 0 {
		t.Fatalf("resume: copied %d of %d", r2.Copied, len(r.Copies))
	}
	if r3 := run(t, opts([]string{O, B, C}, O, true)); len(r3.Copies) != 0 {
		t.Fatalf("third run must copy nothing: %d", len(r3.Copies))
	}
}

// A paused gate blocks the copies until Resume.
func TestPauseBlocksCopies(t *testing.T) {
	O, B, C := scenario(t)
	o := opts([]string{O, B, C}, O, true)
	g := runctl.New(nil)
	o.Gate = g
	paused := make(chan struct{})
	o.BeforeExecute = func(*Result) error { g.Pause(); close(paused); return nil }
	done := make(chan *Result)
	go func() { r, _ := Run(o); done <- r }()
	<-paused
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(O, "PDF")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
		t.Fatal("run finished while paused")
	default:
	}
	g.Resume()
	r := <-done
	if r.Copied != len(r.Copies) || r.Pending != 0 {
		t.Fatalf("after resume: copied %d of %d", r.Copied, len(r.Copies))
	}
}

func TestParseCopyDelay(t *testing.T) {
	for in, want := range map[string]time.Duration{"": 0, "bad": 0, "-5ms": 0, "20ms": 20 * time.Millisecond} {
		if got := parseCopyDelay(in); got != want {
			t.Errorf("parseCopyDelay(%q) = %v, want %v", in, got, want)
		}
	}
}

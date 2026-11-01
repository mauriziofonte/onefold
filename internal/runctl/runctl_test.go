// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package runctl

import (
	"testing"
	"time"
)

func TestNilGate(t *testing.T) {
	var g *Gate
	if !g.Wait() || g.Stopped() || g.State() != Running {
		t.Fatal("nil gate must never pause or stop")
	}
	g.Pause()
	g.Stop()
	if !g.Wait() {
		t.Fatal("nil gate ignores Pause/Stop")
	}
}

func TestPauseResumeStop(t *testing.T) {
	var states []State
	g := New(func(s State) { states = append(states, s) })
	g.Pause()
	released := make(chan bool)
	go func() { released <- g.Wait() }()
	select {
	case <-released:
		t.Fatal("Wait returned while paused")
	case <-time.After(50 * time.Millisecond):
	}
	g.Resume()
	if ok := <-released; !ok {
		t.Fatal("Wait after Resume must return true")
	}
	g.Pause()
	go func() { released <- g.Wait() }()
	time.Sleep(20 * time.Millisecond)
	g.Stop()
	if ok := <-released; ok {
		t.Fatal("Wait after Stop must return false")
	}
	g.Resume() // Stop is final
	if g.Wait() || !g.Stopped() {
		t.Fatal("Stop must be final")
	}
	want := []State{Paused, Running, Paused, Stopped}
	if len(states) != len(want) {
		t.Fatalf("states %v want %v", states, want)
	}
	for i := range want {
		if states[i] != want[i] {
			t.Fatalf("states %v want %v", states, want)
		}
	}
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package runctl is the cooperative pause/stop control shared by the engine
// (catalog, verify, merge, rearrange, materialize) and the GUI. The engine
// calls Wait before starting each unit of work (a file to hash, a copy, a
// rename): while paused Wait blocks, once stopped it returns false and the
// caller skips the unit. Work already started always completes, so every
// pause or stop leaves the trees in a state the engine can resume from.
//
// A nil *Gate never pauses and never stops: the CLI passes nil.
package runctl

import (
	"errors"
	"sync"
)

// ErrStopped is returned by engine entry points that ended early because the
// gate was stopped.
var ErrStopped = errors.New("stopped")

// State of a gate.
type State int

const (
	Running State = iota
	Paused
	Stopped
)

func (s State) String() string {
	switch s {
	case Paused:
		return "paused"
	case Stopped:
		return "stopped"
	}
	return "running"
}

// Gate is safe for concurrent use.
type Gate struct {
	mu       sync.Mutex
	cond     *sync.Cond
	state    State
	onChange func(State)
}

// New returns a running gate. onChange (optional) is called after every state
// change, outside the lock.
func New(onChange func(State)) *Gate {
	g := &Gate{onChange: onChange}
	g.cond = sync.NewCond(&g.mu)
	return g
}

func (g *Gate) set(s State) {
	if g == nil {
		return
	}
	g.mu.Lock()
	if g.state == s || g.state == Stopped {
		g.mu.Unlock()
		return
	}
	g.state = s
	g.cond.Broadcast()
	g.mu.Unlock()
	if g.onChange != nil {
		g.onChange(s)
	}
}

// Pause makes Wait block until Resume or Stop.
func (g *Gate) Pause() { g.set(Paused) }

// Resume releases a paused gate.
func (g *Gate) Resume() { g.set(Running) }

// Stop is final: Wait returns false from now on.
func (g *Gate) Stop() { g.set(Stopped) }

// State reports the current state (Running for a nil gate).
func (g *Gate) State() State {
	if g == nil {
		return Running
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.state
}

// Stopped reports whether Stop was called.
func (g *Gate) Stopped() bool { return g.State() == Stopped }

// Wait blocks while the gate is paused. It returns true when the caller may
// start the next unit of work, false when the gate is stopped.
func (g *Gate) Wait() bool {
	if g == nil {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for g.state == Paused {
		g.cond.Wait()
	}
	return g.state != Stopped
}

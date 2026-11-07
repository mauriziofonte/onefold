// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package power keeps the computer from sleeping while a long run works, and
// lowers the run's priority on request, so a catalog or a copy of hours does
// not stop halfway at the first idle sleep nor slow the rest of the machine
// down.
//
//   - KeepAwake: Windows SetThreadExecutionState(ES_SYSTEM_REQUIRED) on a
//     thread of its own; macOS an IOPMAssertion PreventUserIdleSystemSleep
//     (IOKit through purego, no cgo); Linux a systemd-logind sleep lock held
//     by `systemd-inhibit` (an optional tool: without it, or without a
//     session allowed to take the lock, nothing happens). The display may
//     still turn off; a closed lid or a sleep asked by the user still
//     sleeps. Every lock ends with the process.
//   - SetLowPriority: Windows background mode (PROCESS_MODE_BACKGROUND_BEGIN:
//     low I/O, memory and CPU priority); macOS the throttled disk policy
//     (setiopolicy_np IOPOL_THROTTLE) and nice 10; Linux the lowest
//     best-effort I/O priority (ioprio_set, every thread) and nice 10.
package power

import (
	"sync"
	"sync/atomic"
)

// Reason names the lock where the system shows it (Activity Monitor,
// `powercfg /requests` lists the process, `systemd-inhibit --list`).
const Reason = "onefold is working on your files"

// keepAwakeFn is the platform lock; a test replaces it.
var keepAwakeFn = keepAwake

var (
	mu   sync.Mutex
	held int
	stop func()
)

// KeepAwake asks the system not to sleep on idle until release is called;
// nested calls share one lock. err says why it could not (release is then a
// no-op): the run goes on either way.
func KeepAwake(why string) (release func(), err error) {
	mu.Lock()
	defer mu.Unlock()
	if held == 0 {
		setProblem(nil)
		s, err := keepAwakeFn(why)
		if err != nil {
			setProblem(err)
			return func() {}, err
		}
		stop = s
	}
	held++
	var once sync.Once
	return func() {
		once.Do(func() {
			mu.Lock()
			defer mu.Unlock()
			held--
			if held == 0 && stop != nil {
				stop()
				stop = nil
			}
		})
	}, nil
}

var problem atomic.Value // error

func setProblem(err error) { problem.Store(&err) }

// Problem is why the last lock taken did not hold ("" while it holds, or
// before any): Linux learns it only after the start, when systemd-inhibit
// gives up.
func Problem() string {
	if p, _ := problem.Load().(*error); p != nil && *p != nil {
		return (*p).Error()
	}
	return ""
}

// SetLowPriority lowers (on) or restores (off) the I/O and CPU priority of
// the whole process.
func SetLowPriority(on bool) error { return setLowPriority(on) }

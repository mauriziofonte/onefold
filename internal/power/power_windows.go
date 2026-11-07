// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package power

import (
	"errors"
	"runtime"

	"golang.org/x/sys/windows"
)

const (
	esContinuous     = 0x80000000
	esSystemRequired = 0x00000001
)

var procSetThreadExecutionState = windows.NewLazySystemDLL("kernel32.dll").NewProc("SetThreadExecutionState")

// The execution state belongs to a thread: one locked to its goroutine holds
// it until release (or the end of the process).
func keepAwake(string) (func(), error) {
	if err := procSetThreadExecutionState.Find(); err != nil {
		return nil, err
	}
	started, done := make(chan error), make(chan struct{})
	go func() {
		runtime.LockOSThread() // never unlocked: the thread ends with the goroutine
		if r, _, _ := procSetThreadExecutionState.Call(esContinuous | esSystemRequired); r == 0 {
			started <- errors.New("SetThreadExecutionState refused")
			return
		}
		started <- nil
		<-done
		procSetThreadExecutionState.Call(esContinuous)
	}()
	if err := <-started; err != nil {
		return nil, err
	}
	return func() { close(done) }, nil
}

func setLowPriority(on bool) error {
	mode := uint32(windows.PROCESS_MODE_BACKGROUND_END)
	if on {
		mode = windows.PROCESS_MODE_BACKGROUND_BEGIN
	}
	err := windows.SetPriorityClass(windows.CurrentProcess(), mode)
	if errors.Is(err, windows.Errno(402)) || errors.Is(err, windows.Errno(403)) {
		return nil // ERROR_PROCESS_MODE_ALREADY_BACKGROUND / _NOT_BACKGROUND
	}
	return err
}

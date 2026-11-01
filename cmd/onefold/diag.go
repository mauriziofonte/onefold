// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"log"
	"sync/atomic"
)

// diagLog is the launch/GUI diagnostic log. On Windows it is a sidecar file
// next to the executable, on when the user's config says so (`onefold config
// set diag-log on`, the GUI Config view) or ONEFOLD_DIAG=1: a GUI process has
// no console, its errors would vanish. nil otherwise and elsewhere.
var diagLog atomic.Pointer[log.Logger] // switched at run time by the GUI Config view

func dlog(format string, a ...any) {
	if l := diagLog.Load(); l != nil {
		l.Printf(format, a...)
	}
}

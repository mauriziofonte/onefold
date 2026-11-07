// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !windows

package main

import (
	"os"
	"runtime"
	"syscall"
)

// maxRSSMiB: ru_maxrss is in KiB on Linux, in bytes on macOS.
func maxRSSMiB(ps *os.ProcessState) float64 {
	ru, ok := ps.SysUsage().(*syscall.Rusage)
	if !ok {
		return 0
	}
	if runtime.GOOS == "darwin" {
		return float64(ru.Maxrss) / (1 << 20)
	}
	return float64(ru.Maxrss) / 1024
}

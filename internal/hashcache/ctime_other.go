// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !windows && !linux && !darwin

package hashcache

import "syscall"

// ctime: other Unix systems are not release targets; without a known
// status-change field their files are not cached.
func ctime(*syscall.Stat_t) (int64, int64) { return -1, -1 }

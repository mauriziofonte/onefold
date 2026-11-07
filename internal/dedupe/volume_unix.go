// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !windows

package dedupe

import "syscall"

// sameVolume: both files on one device (a link or a clone cannot cross).
func sameVolume(a, b string) bool {
	var sa, sb syscall.Stat_t
	if syscall.Lstat(a, &sa) != nil || syscall.Lstat(b, &sb) != nil {
		return false
	}
	return sa.Dev == sb.Dev
}

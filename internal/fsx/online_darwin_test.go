// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package fsx

import (
	"syscall"
	"testing"
)

// SF_DATALESS is set only by the kernel for a file provider: the flag test
// uses a synthetic stat.
func TestOnlineOnlyFlags(t *testing.T) {
	for flags, want := range map[uint32]bool{0: false, 0x8000: false, sfDataless: true, sfDataless | 0x20: true} {
		if got := OnlineOnly(fakeInfo{&syscall.Stat_t{Flags: flags}}); got != want {
			t.Errorf("flags %#x: %v, want %v", flags, got, want)
		}
	}
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package hashcache

import "syscall"

func ctime(st *syscall.Stat_t) (int64, int64) {
	return int64(st.Ctimespec.Sec), int64(st.Ctimespec.Nsec)
}

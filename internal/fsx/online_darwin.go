// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package fsx

import (
	"io/fs"
	"syscall"
)

// sfDataless is SF_DATALESS of <sys/stat.h>: the file's content has to be
// materialized by its file provider before it can be read.
const sfDataless = 0x40000000

func onlineOnly(fi fs.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Flags&sfDataless != 0
}

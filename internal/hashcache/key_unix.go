// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !windows

package hashcache

import (
	"fmt"
	"io/fs"
	"syscall"
)

// keyOf: device, inode, size, modification and status-change times in
// nanoseconds. Any write or attribute change moves the status-change time,
// and a new file reusing an inode gets a new one.
func keyOf(_ string, fi fs.FileInfo) string {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	cs, cn := ctime(st)
	if cs < 0 {
		return ""
	}
	return fmt.Sprintf("u:%x:%x:%d:%d:%d.%d", uint64(st.Dev), uint64(st.Ino), fi.Size(), fi.ModTime().UnixNano(), cs, cn)
}

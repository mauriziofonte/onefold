// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build unix

package stats

import (
	"io/fs"
	"syscall"
)

// fileSys is what the OS tells about a file beyond fs.FileInfo.
type fileSys struct {
	dev, ino uint64
	links    uint64
	alloc    int64 // bytes allocated on disk
	allocOK  bool
}

func sysOf(fi fs.FileInfo) fileSys {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return fileSys{}
	}
	return fileSys{dev: uint64(st.Dev), ino: uint64(st.Ino), links: uint64(st.Nlink), alloc: int64(st.Blocks) * 512, allocOK: true}
}

// devOf is the device of the folder p (ok = false: unknown).
func devOf(p string) (uint64, bool) {
	var st syscall.Stat_t
	if err := syscall.Lstat(p, &st); err != nil {
		return 0, false
	}
	return uint64(st.Dev), true
}

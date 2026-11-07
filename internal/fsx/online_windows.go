// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package fsx

import (
	"io/fs"
	"syscall"
)

// File attributes of a file whose data is elsewhere (winnt.h).
const (
	fileAttributeOffline            = 0x1000
	fileAttributeRecallOnOpen       = 0x40000
	fileAttributeRecallOnDataAccess = 0x400000
	onlineOnlyAttrs                 = fileAttributeOffline | fileAttributeRecallOnOpen | fileAttributeRecallOnDataAccess
)

func onlineOnly(fi fs.FileInfo) bool {
	d, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	return ok && d.FileAttributes&onlineOnlyAttrs != 0
}

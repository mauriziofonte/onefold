// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package fsx

import (
	"os"

	"golang.org/x/sys/windows"
)

// OpenUncached opens p with FILE_FLAG_NO_BUFFERING: every read comes from the
// device. Reads must use buffers aligned to UncachedAlign with a length that
// is a multiple of it (AlignedBuffer); the last read returns the short tail.
func OpenUncached(p string) (*os.File, error) {
	name, err := windows.UTF16PtrFromString(fixLong(p))
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(name, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_NO_BUFFERING|windows.FILE_FLAG_SEQUENTIAL_SCAN, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: p, Err: err}
	}
	return os.NewFile(uintptr(h), p), nil
}

// UncachedAlign covers the sector sizes of current drives (512, 4096).
const UncachedAlign = 4096

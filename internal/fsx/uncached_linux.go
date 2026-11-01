// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package fsx

import (
	"os"

	"golang.org/x/sys/unix"
)

// OpenUncached opens p for a read that comes from the device, not the page
// cache: the file's cached pages are dropped first (they are clean once the
// file was fsync'ed; dirty pages are not dropped). Reads use a plain buffer.
func OpenUncached(p string) (*os.File, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	if err := unix.Fadvise(int(f.Fd()), 0, 0, unix.FADV_DONTNEED); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// UncachedAlign is the buffer alignment OpenUncached reads need (none here).
const UncachedAlign = 1

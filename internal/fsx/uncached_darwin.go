// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package fsx

import (
	"os"

	"golang.org/x/sys/unix"
)

// OpenUncached opens p for a read that comes from the device, not the cache.
// macOS has no fadvise: the file's cached pages are invalidated with
// msync(MS_INVALIDATE) on a read-only mapping (they are clean once the file
// was fsync'ed), then F_NOCACHE keeps this descriptor's reads out of the
// cache. Reads use a plain buffer.
func OpenUncached(p string) (*os.File, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*os.File, error) {
		f.Close()
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	if n := fi.Size(); n > 0 && int64(int(n)) == n {
		m, err := unix.Mmap(int(f.Fd()), 0, int(n), unix.PROT_READ, unix.MAP_SHARED)
		if err != nil {
			return fail(err)
		}
		err = unix.Msync(m, unix.MS_INVALIDATE)
		if uerr := unix.Munmap(m); err == nil {
			err = uerr
		}
		if err != nil {
			return fail(err)
		}
	}
	if _, err := unix.FcntlInt(f.Fd(), unix.F_NOCACHE, 1); err != nil {
		return fail(err)
	}
	return f, nil
}

// UncachedAlign is the buffer alignment OpenUncached reads need (none here).
const UncachedAlign = 1

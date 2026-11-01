// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build darwin

package fsx

import (
	"errors"

	"golang.org/x/sys/unix"
)

// renameNoReplace is renamex_np with RENAME_EXCL: one atomic call on APFS and
// HFS+. A file system without it (some exFAT, FAT and network drivers) gets
// the portable fallback of RenameNoReplace.
func renameNoReplace(src, dst string) error {
	err := unix.RenamexNp(src, dst, unix.RENAME_EXCL)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, unix.EEXIST):
		return ErrExists
	case errors.Is(err, unix.EINVAL), errors.Is(err, unix.ENOTSUP), errors.Is(err, unix.EOPNOTSUPP), errors.Is(err, unix.ENOSYS):
		return errUnsupported
	}
	return err
}

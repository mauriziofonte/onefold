// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package fsx

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func cloneFile(src, dst string) error {
	// NOOWNERCOPY: the clone belongs to the user running onefold, as a copy would
	if err := unix.Clonefile(src, dst, unix.CLONE_NOFOLLOW|unix.CLONE_NOOWNERCOPY); err != nil {
		// another volume, or HFS+, FAT, exFAT, SMB...
		if errors.Is(err, unix.EXDEV) || errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EINVAL) {
			return fmt.Errorf("%w (%v)", ErrNoClone, err)
		}
		return err
	}
	// clonefile copies the source's mode: a read-only source would give a
	// read-only result, unlike a byte copy
	return os.Chmod(dst, 0o644)
}

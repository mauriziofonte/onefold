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
	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()
	d, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if err := unix.IoctlFileClone(int(d.Fd()), int(s.Fd())); err != nil {
		d.Close()
		os.Remove(dst)
		// another volume, or a file system without shared extents (ext4,
		// tmpfs, FAT, network mounts...)
		if errors.Is(err, unix.EXDEV) || errors.Is(err, unix.EOPNOTSUPP) || errors.Is(err, unix.EINVAL) ||
			errors.Is(err, unix.ENOTTY) || errors.Is(err, unix.ENOSYS) {
			return fmt.Errorf("%w (%v)", ErrNoClone, err)
		}
		return err
	}
	return d.Close()
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package fsx

import (
	"errors"
	"fmt"
	"os"
)

// ErrCrossVolume is returned by LinkNoReplace when source and destination are
// on different volumes: a hard link cannot span them.
var ErrCrossVolume = errors.New("source and destination are on different volumes")

// LinkNoReplace gives the file src a second name dst (a hard link: the same
// bytes, no copy) and FAILS with ErrExists if dst exists. The link is created
// in one system call that refuses an existing name on every OS, so it is
// atomic and never replaces anything. A cross-volume pair returns
// ErrCrossVolume; a filesystem without hard links (FAT, exFAT, some network
// shares) returns the system error, wrapped with both paths.
func LinkNoReplace(src, dst string) error {
	CrashPoint("link")
	err := os.Link(src, dst)
	switch {
	case err == nil:
		return nil
	case os.IsExist(err):
		return ErrExists
	case isCrossDevice(err):
		return fmt.Errorf("hard link %s -> %s: %w", src, dst, ErrCrossVolume)
	}
	return fmt.Errorf("hard link %s -> %s (the filesystem may not support hard links): %w", src, dst, err)
}

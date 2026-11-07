// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !windows

package fsx

import (
	"errors"
	"os"
	"syscall"
)

// SyncFile flushes the data and metadata of the file at path to the device
// (fsync; on macOS Go's File.Sync is F_FULLFSYNC, past the drive cache).
func SyncFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	err = f.Sync()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// SyncDir flushes a directory's entries, so that a name created or renamed
// in it survives a power cut: without it a rename can be lost even though the
// file's bytes were flushed. File systems that cannot flush a directory
// (EINVAL, ENOTSUP: some FUSE and network mounts) are not an error: there is
// nothing more onefold can do there, and their own semantics apply.
func SyncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = f.Sync()
	f.Close()
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EBADF) {
		return nil
	}
	return err
}

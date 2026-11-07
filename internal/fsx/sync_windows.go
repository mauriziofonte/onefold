// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package fsx

import "os"

// SyncFile flushes the data and metadata of the file at path to the device
// (FlushFileBuffers, which needs a handle with write access; the file is
// opened without truncating and not written to). A read-only file cannot be
// flushed and returns its error: callers that delete another copy then refuse.
func SyncFile(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	err = f.Sync()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// SyncDir is a no-op on Windows: every onefold rename there is MoveFileEx
// with MOVEFILE_WRITE_THROUGH, which returns only once the move is on disk,
// and NTFS journals directory changes.
func SyncDir(dir string) error {
	_, err := os.Stat(dir)
	return err
}

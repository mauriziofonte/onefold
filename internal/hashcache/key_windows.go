// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package hashcache

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"syscall"

	"github.com/mauriziofonte/onefold/internal/names"
)

// keyOf: the directory listing has no file ID (reading it would open every
// file), so the folded absolute path, size, creation and last-write times.
// A file rewritten in place moves its last-write time; one replaced by a
// new file gets a new creation time.
func keyOf(path string, fi fs.FileInfo) string {
	d, ok := fi.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("w:%s:%d:%d:%d", names.FoldKey(abs), fi.Size(), d.CreationTime.Nanoseconds(), d.LastWriteTime.Nanoseconds())
}

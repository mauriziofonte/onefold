// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package stats

import "io/fs"

// fileSys is what the OS tells about a file beyond fs.FileInfo. A Windows
// directory listing carries no link count nor allocation: hard links are
// counted once per name and Result.Allocated stays 0 (documented).
type fileSys struct {
	dev, ino uint64
	links    uint64
	alloc    int64
	allocOK  bool
}

func sysOf(fs.FileInfo) fileSys { return fileSys{} }

// devOf: Windows walks never enter another volume (mounted folders are
// reparse points, not followed), so no device check is needed.
func devOf(string) (uint64, bool) { return 0, false }

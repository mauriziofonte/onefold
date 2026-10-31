// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build windows

package gui

import (
	"os"

	"golang.org/x/sys/windows"
)

// roots are the drives (C:\, D:\ ...) plus the user folder.
func roots() []FSEntry {
	var out []FSEntry
	mask, err := windows.GetLogicalDrives()
	if err == nil {
		for i := 0; i < 26; i++ {
			if mask&(1<<uint(i)) != 0 {
				d := string(rune('A'+i)) + `:\`
				out = append(out, FSEntry{Name: d, Path: d})
			}
		}
	}
	if h, err := os.UserHomeDir(); err == nil {
		out = append(out, FSEntry{Name: "~", Path: h})
	}
	return out
}

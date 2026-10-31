// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !windows

package gui

import (
	"os"
	"path/filepath"
)

// roots are the starting points of the folder picker.
func roots() []FSEntry {
	out := []FSEntry{{Name: "/", Path: "/"}}
	if h, err := os.UserHomeDir(); err == nil {
		out = append(out, FSEntry{Name: "~", Path: h})
	}
	for _, base := range []string{"/mnt", "/media"} {
		if ents, err := os.ReadDir(base); err == nil {
			for _, e := range ents {
				if e.IsDir() {
					out = append(out, FSEntry{Name: filepath.Join(base, e.Name()), Path: filepath.Join(base, e.Name())})
				}
			}
		}
	}
	return out
}

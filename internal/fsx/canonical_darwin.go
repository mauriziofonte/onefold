// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build darwin

package fsx

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/text/unicode/norm"
)

// diskNames caches resolved folders: a run asks for the same parents of
// many files, and a folder's stored name does not change under it.
var diskNames sync.Map // existing path -> its stored spelling

// onDiskName spells an existing path the way the volume stores it. APFS and
// HFS+ (in their default modes) find /users/Me/Café through /Users/me/Café
// in any case and either Unicode normalization, while string comparisons do
// not: each component takes the name its folder lists for it. A component
// whose folder cannot be read keeps the given spelling.
func onDiskName(p string) string {
	if v, ok := diskNames.Load(p); ok {
		return v.(string)
	}
	parent := filepath.Dir(p)
	if parent == p {
		return p // the root
	}
	dir := onDiskName(parent)
	base := filepath.Base(p)
	out := filepath.Join(dir, base)
	if ents, err := os.ReadDir(dir); err == nil {
		exact := false
		for _, e := range ents {
			if e.Name() == base {
				exact = true
				break
			}
		}
		if !exact {
			want := strings.ToLower(norm.NFC.String(base))
			for _, e := range ents {
				if strings.ToLower(norm.NFC.String(e.Name())) == want {
					out = filepath.Join(dir, e.Name())
					break
				}
			}
		}
	}
	diskNames.Store(p, out)
	return out
}

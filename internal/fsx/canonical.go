// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package fsx

import (
	"os"
	"path/filepath"
)

// Canonical returns the one spelling of p that every path comparison in
// onefold uses: absolute, clean, with symbolic links (and, on Windows, 8.3
// short names such as C:\Users\RUNNER~1) resolved. The same folder reached
// through a link, a short name or its real name must compare equal, or
// "output inside an input" checks, exclusions and purge refusals would miss.
//
// A path that does not exist yet (an output folder to create) is resolved
// through its longest existing ancestor, and the missing part is appended
// as given. Canonical never fails because of a missing path; it fails only
// when the path cannot be made absolute.
func Canonical(p string) (string, error) {
	a, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	a = filepath.Clean(a)
	head, tail := a, ""
	for {
		if r, err := filepath.EvalSymlinks(head); err == nil {
			return filepath.Clean(filepath.Join(r, tail)), nil
		}
		if _, err := os.Lstat(head); err == nil {
			// exists but cannot be resolved (permission denied on a parent):
			// keep the spelling the user gave
			return a, nil
		}
		parent := filepath.Dir(head)
		if parent == head {
			return a, nil // nothing of it exists, not even the volume
		}
		tail = filepath.Join(filepath.Base(head), tail)
		head = parent
	}
}

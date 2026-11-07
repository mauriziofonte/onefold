// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package fsx

import "errors"

// ErrNoClone: the file system (or this OS) cannot clone the file there;
// callers make a byte copy instead.
var ErrNoClone = errors.New("cloning not supported here")

// Clone creates dst (which must not exist) as a clone of src: a new file that
// shares src's blocks on the device until either is written (copy-on-write).
// Instant and without extra space, on one volume of a file system that
// supports it: APFS (macOS clonefile), Btrfs, XFS with reflink, bcachefs and
// others (Linux FICLONE). Anything else returns an error wrapping ErrNoClone
// with dst absent. A clone is a separate file: deleting or changing the
// source never touches it. It shares the device blocks, though: one bad
// sector damages both, so a backup never clones.
func Clone(src, dst string) error { return cloneFile(src, dst) }

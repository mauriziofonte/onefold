// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package fsx holds the filesystem primitives merge relies on for "never lose
// or clobber data": no-replace rename and onefold temp-file naming.
package fsx

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unsafe"
)

// TempSuffix marks onefold in-flight files. Only files with this suffix are
// ever removed by onefold (stale leftovers of an interrupted run).
const TempSuffix = ".onefold-tmp"

// ErrExists is returned when the rename target already exists.
var ErrExists = errors.New("target exists")

// IsTemp reports whether name is a onefold temp file: the mark at the end, or,
// for the dot-prefixed names of TempFor, anywhere (before the extension, as in
// `.VID_x.onefold-tmp.mp4`, or followed by a tool's own temp suffix, as in
// exiftool's `.x.jpg.onefold-tmp_exiftool_tmp`).
func IsTemp(name string) bool {
	return strings.HasSuffix(name, TempSuffix) || strings.HasPrefix(name, ".") && strings.Contains(name, TempSuffix)
}

// TempFor is the in-flight name of dst in its own directory. keepExt puts the
// mark before the extension (for tools that choose the format by extension).
// The leading dot keeps it out of listings and of every catalog.
func TempFor(dst string, keepExt bool) string {
	dir, base := filepath.Split(dst)
	if keepExt {
		ext := filepath.Ext(base)
		return filepath.Join(dir, "."+strings.TrimSuffix(base, ext)+TempSuffix+ext)
	}
	return filepath.Join(dir, "."+base+TempSuffix)
}

// RenameNoReplace moves src to dst and FAILS (ErrExists) if dst exists. It
// never overwrites. Platform strategy in rename_*.go; the portable fallback
// (hard link + unlink, then check-and-rename) is used where the kernel/FS does
// not support an atomic no-replace rename (e.g. WSL drvfs).
func RenameNoReplace(src, dst string) error {
	err := renameNoReplace(src, dst)
	if err == nil || errors.Is(err, ErrExists) || !errors.Is(err, errUnsupported) {
		return err
	}
	if lerr := os.Link(src, dst); lerr == nil {
		return os.Remove(src)
	} else if os.IsExist(lerr) {
		return ErrExists
	}
	if _, serr := os.Lstat(dst); serr == nil {
		return ErrExists
	}
	return os.Rename(src, dst)
}

var errUnsupported = errors.New("no-replace rename unsupported")

// AlignedBuffer returns a buffer of size n (rounded up to UncachedAlign)
// whose address is aligned to UncachedAlign, as OpenUncached reads need.
func AlignedBuffer(n int) []byte {
	n = (n + UncachedAlign - 1) / UncachedAlign * UncachedAlign
	b := make([]byte, n+UncachedAlign)
	off := int(uintptr(unsafe.Pointer(&b[0])) & uintptr(UncachedAlign-1))
	if off != 0 {
		off = UncachedAlign - off
	}
	return b[off : off+n]
}

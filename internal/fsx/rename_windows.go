// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build windows

package fsx

import (
	"errors"

	"golang.org/x/sys/windows"
)

// MoveFileEx without MOVEFILE_REPLACE_EXISTING fails if dst exists.
func renameNoReplace(src, dst string) error {
	from, err := windows.UTF16PtrFromString(fixLong(src))
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(fixLong(dst))
	if err != nil {
		return err
	}
	err = windows.MoveFileEx(from, to, windows.MOVEFILE_WRITE_THROUGH)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS) {
		return ErrExists
	}
	return err
}

// fixLong adds the \\?\ prefix to absolute paths so >260-char paths work.
func fixLong(p string) string {
	if len(p) < 248 || len(p) >= 4 && p[:4] == `\\?\` {
		return p
	}
	if len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') {
		return `\\?\` + p
	}
	if len(p) >= 2 && p[:2] == `\\` {
		return `\\?\UNC\` + p[2:]
	}
	return p
}

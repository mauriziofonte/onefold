// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package testfix

import "golang.org/x/sys/windows"

// SetOffline gives the file FILE_ATTRIBUTE_OFFLINE, the one "data is
// elsewhere" attribute an application may set: tests of online-only files use
// it in place of a real cloud placeholder (its bytes stay readable).
func SetOffline(path string) error {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	a, err := windows.GetFileAttributes(p)
	if err != nil {
		return err
	}
	return windows.SetFileAttributes(p, a|windows.FILE_ATTRIBUTE_OFFLINE)
}

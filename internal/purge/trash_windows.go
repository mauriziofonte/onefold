// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package purge

import (
	"fmt"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

const trashSupported = true

var procSHFileOperationW = windows.NewLazySystemDLL("shell32.dll").NewProc("SHFileOperationW")

// shFileOpStruct is SHFILEOPSTRUCTW (64-bit layout, natural alignment).
type shFileOpStruct struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

const (
	foDelete          = 0x0003
	fofSilent         = 0x0004
	fofNoConfirmation = 0x0010
	fofAllowUndo      = 0x0040
	fofNoErrorUI      = 0x0400
)

// moveToTrash sends one file or empty directory to the Recycle Bin. On a
// volume without a Recycle Bin (network shares) Windows deletes it.
func moveToTrash(p string) error {
	from, err := windows.UTF16FromString(p)
	if err != nil {
		return err
	}
	from = append(from, 0) // double-NUL terminated list
	op := shFileOpStruct{wFunc: foDelete, pFrom: &from[0], fFlags: fofAllowUndo | fofNoConfirmation | fofSilent | fofNoErrorUI}
	r, _, _ := procSHFileOperationW.Call(uintptr(unsafe.Pointer(&op)))
	if r != 0 {
		//lint:ignore ST1005 "Recycle Bin" is the name Windows shows, a proper noun
		return fmt.Errorf("Recycle Bin: SHFileOperation error 0x%x", r)
	}
	if op.fAnyOperationsAborted != 0 {
		//lint:ignore ST1005 "Recycle Bin" is the name Windows shows, a proper noun
		return fmt.Errorf("Recycle Bin: operation aborted")
	}
	return nil
}

// trashVolume reports the volume of p, whether it is a UNC share and its
// drive type (GetDriveTypeW on the volume root).
func trashVolume(p string) (vol string, unc bool, driveType uint32) {
	vol = filepath.VolumeName(p)
	if strings.HasPrefix(vol, `\\`) {
		return strings.ToLower(vol), true, driveRemote
	}
	root, err := windows.UTF16PtrFromString(vol + `\`)
	if err != nil {
		return strings.ToLower(vol), false, driveUnknown
	}
	return strings.ToLower(vol), false, windows.GetDriveType(root)
}

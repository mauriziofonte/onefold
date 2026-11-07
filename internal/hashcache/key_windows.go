// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package hashcache

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/mauriziofonte/onefold/internal/fsx"
)

// keyOf: volume serial and file ID, size, last-write and change times, as
// the Unix key. The directory listing carries neither the ID nor the change
// time, so the file is opened for its attributes only (no read access: no
// antivirus scan, nothing locked). The change time moves on every write and
// attribute change and is not set by the tools that restore a last-write
// time. FAT and exFAT keep no change time: their files are not cached.
func keyOf(path string, fi fs.FileInfo) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	p, err := windows.UTF16PtrFromString(fsx.LongPath(abs))
	if err != nil {
		return ""
	}
	h, err := windows.CreateFile(p, windows.FILE_READ_ATTRIBUTES,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	var bi struct {
		CreationTime, LastAccessTime, LastWriteTime, ChangeTime int64
		FileAttributes                                          uint32
		_                                                       uint32
	}
	if windows.GetFileInformationByHandleEx(h, windows.FileBasicInfo, (*byte)(unsafe.Pointer(&bi)), uint32(unsafe.Sizeof(bi))) != nil {
		return ""
	}
	var id struct {
		VolumeSerialNumber uint64
		FileID             [16]byte
	}
	if windows.GetFileInformationByHandleEx(h, windows.FileIdInfo, (*byte)(unsafe.Pointer(&id)), uint32(unsafe.Sizeof(id))) != nil {
		return "" // FAT, exFAT and older file systems
	}
	if !changeTimed(h, id.VolumeSerialNumber) {
		return ""
	}
	return fmt.Sprintf("w:%x:%x:%d:%d:%d", id.VolumeSerialNumber, id.FileID, fi.Size(), bi.LastWriteTime, bi.ChangeTime)
}

// volumes: serial -> whether its file system keeps a change time.
var volumes sync.Map

func changeTimed(h windows.Handle, serial uint64) bool {
	if v, ok := volumes.Load(serial); ok {
		return v.(bool)
	}
	var fsName [windows.MAX_PATH + 1]uint16
	ok := false
	if windows.GetVolumeInformationByHandle(h, nil, 0, nil, nil, nil, &fsName[0], uint32(len(fsName))) == nil {
		switch strings.ToUpper(windows.UTF16ToString(fsName[:])) {
		case "NTFS", "REFS":
			ok = true
		}
	}
	volumes.Store(serial, ok)
	return ok
}

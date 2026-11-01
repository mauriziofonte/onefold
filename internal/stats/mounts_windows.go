// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package stats

import (
	"golang.org/x/sys/windows"
)

// rawMounts lists the drive letters with a volume in them.
func rawMounts() []Mount {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil
	}
	var out []Mount
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		m, ok := driveInfo(root)
		if ok {
			out = append(out, m)
		}
	}
	return out
}

func driveInfo(root string) (Mount, bool) {
	r, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return Mount{}, false
	}
	dt := windows.GetDriveType(r)
	if dt == windows.DRIVE_NO_ROOT_DIR || dt == windows.DRIVE_UNKNOWN {
		return Mount{}, false
	}
	label := make([]uint16, windows.MAX_PATH+1)
	fsName := make([]uint16, windows.MAX_PATH+1)
	var serial, maxLen, flags uint32
	if err := windows.GetVolumeInformation(r, &label[0], uint32(len(label)), &serial, &maxLen, &flags, &fsName[0], uint32(len(fsName))); err != nil {
		return Mount{}, false // e.g. an empty card reader or optical drive
	}
	m := Mount{Path: root, Label: windows.UTF16ToString(label), FSType: windows.UTF16ToString(fsName),
		Remote: dt == windows.DRIVE_REMOTE, Removable: dt == windows.DRIVE_REMOVABLE || dt == windows.DRIVE_CDROM,
		ReadOnly: flags&windows.FILE_READ_ONLY_VOLUME != 0}
	var avail, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(r, &avail, &total, &free); err == nil {
		m.Total, m.Free = int64(total), int64(avail)
	}
	return m, true
}

// VolumeOf describes the volume holding p.
func VolumeOf(p string) Volume {
	v := Volume{Root: p}
	pp, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return v
	}
	buf := make([]uint16, windows.MAX_PATH+1)
	if err := windows.GetVolumePathName(pp, &buf[0], uint32(len(buf))); err != nil {
		return v
	}
	v.Mount = windows.UTF16ToString(buf)
	if m, ok := driveInfo(v.Mount); ok {
		v.FSType = m.FSType
	}
	var avail, total, free uint64
	if mp, err := windows.UTF16PtrFromString(v.Mount); err == nil && windows.GetDiskFreeSpaceEx(mp, &avail, &total, &free) == nil {
		v.Total, v.Free = int64(total), int64(avail)
	}
	return v
}

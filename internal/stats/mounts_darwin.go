// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package stats

import (
	"strings"

	"golang.org/x/sys/unix"
)

// rawMounts lists the mounted file systems (getfsstat). Of the system
// volumes of macOS only "/" and the Data volume, where the files of the
// people live, are kept.
func rawMounts() []Mount {
	n, err := unix.Getfsstat(nil, unix.MNT_NOWAIT)
	if err != nil || n <= 0 {
		return nil
	}
	buf := make([]unix.Statfs_t, n)
	n, err = unix.Getfsstat(buf, unix.MNT_NOWAIT)
	if err != nil {
		return nil
	}
	var out []Mount
	for _, st := range buf[:n] {
		mp, fstype := cstr(st.Mntonname[:]), cstr(st.Fstypename[:])
		if pseudoFS[fstype] {
			continue
		}
		if strings.HasPrefix(mp, "/System/Volumes/") && mp != "/System/Volumes/Data" {
			continue
		}
		if strings.HasPrefix(mp, "/private/var/vm") || strings.HasPrefix(mp, "/Library/Developer/CoreSimulator") {
			continue
		}
		out = append(out, Mount{Path: mp, FSType: fstype, Device: cstr(st.Mntfromname[:]),
			Total: int64(st.Blocks) * int64(st.Bsize), Free: int64(st.Bavail) * int64(st.Bsize),
			Remote: st.Flags&unix.MNT_LOCAL == 0 || remoteFS[fstype], ReadOnly: st.Flags&unix.MNT_RDONLY != 0,
			Removable: strings.HasPrefix(mp, "/Volumes/")})
	}
	return out
}

func cstr(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

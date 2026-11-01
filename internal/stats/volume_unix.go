// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux || darwin

package stats

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// VolumeOf describes the volume holding p: its mount point (the longest
// listed mount point above p), file system and space.
func VolumeOf(p string) Volume {
	v := Volume{Root: p}
	var st unix.Statfs_t
	if err := unix.Statfs(p, &st); err == nil {
		v.Total = int64(st.Blocks) * int64(st.Bsize)
		v.Free = int64(st.Bavail) * int64(st.Bsize)
	}
	best := ""
	for _, m := range rawMounts() {
		if (p == m.Path || strings.HasPrefix(p, strings.TrimSuffix(m.Path, "/")+"/")) && len(m.Path) > len(best) {
			best, v.FSType = m.Path, m.FSType
		}
	}
	v.Mount = best
	if v.Mount == "" {
		v.Mount = filepath.VolumeName(p) + "/"
	}
	return v
}

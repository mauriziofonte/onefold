// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package stats

import (
	"bufio"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// rawMounts reads /proc/self/mountinfo: real file systems, one per mount
// point (the last mounted wins), with their space.
func rawMounts() []Mount {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil
	}
	defer f.Close()
	return parseMountinfo(bufio.NewScanner(f), statSpace)
}

func statSpace(p string) (total, free int64, ok bool) {
	var st unix.Statfs_t
	if err := unix.Statfs(p, &st); err != nil {
		return 0, 0, false
	}
	return int64(st.Blocks) * int64(st.Bsize), int64(st.Bavail) * int64(st.Bsize), true
}

// parseMountinfo parses mountinfo lines:
// id parent major:minor root mountpoint options [optional...] - fstype source superoptions
func parseMountinfo(sc *bufio.Scanner, space func(string) (int64, int64, bool)) []Mount {
	byPath := map[string]int{}
	var out []Mount
	for sc.Scan() {
		left, right, ok := strings.Cut(sc.Text(), " - ")
		if !ok {
			continue
		}
		lf, rf := strings.Fields(left), strings.Fields(right)
		if len(lf) < 6 || len(rf) < 2 {
			continue
		}
		fstype, source := rf[0], rf[1]
		if pseudoFS[fstype] || strings.HasPrefix(fstype, "cgroup") {
			continue
		}
		mp := unescapeMount(lf[4])
		if strings.HasPrefix(mp, "/proc") || strings.HasPrefix(mp, "/sys") || strings.HasPrefix(mp, "/dev") ||
			strings.HasPrefix(mp, "/run/") || strings.HasPrefix(mp, "/snap/") || mp == "/boot/efi" {
			continue
		}
		m := Mount{Path: mp, FSType: fstype, Device: unescapeMount(source),
			Remote:   remoteFS[fstype] || strings.HasPrefix(fstype, "nfs"),
			ReadOnly: strings.HasPrefix(lf[5], "ro,") || lf[5] == "ro"}
		if t, fr, ok := space(mp); ok {
			m.Total, m.Free = t, fr
		}
		if i, ok := byPath[mp]; ok {
			out[i] = m
			continue
		}
		byPath[mp] = len(out)
		out = append(out, m)
	}
	return out
}

// unescapeMount decodes the octal escapes of mountinfo (\040 = space).
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package stats

import (
	"bufio"
	"strings"
	"testing"
)

func TestParseMountinfo(t *testing.T) {
	in := `22 1 8:1 / / rw,relatime shared:1 - ext4 /dev/sda1 rw
23 22 0:5 / /proc rw - proc proc rw
24 22 8:17 / /mnt/My\040Disk rw,nosuid - exfat /dev/sdb1 rw
25 22 0:40 / /srv/nas rw - nfs4 nas:/export rw
26 22 0:41 / /sys/fs/cgroup rw - cgroup2 cgroup rw
27 22 7:0 / /snap/core/1 ro - squashfs /dev/loop0 ro
28 22 8:1 / /mnt/My\040Disk ro - exfat /dev/sdb1 ro
`
	ms := parseMountinfo(bufio.NewScanner(strings.NewReader(in)), func(string) (int64, int64, bool) { return 100, 40, true })
	if len(ms) != 3 {
		t.Fatalf("mounts: %+v", ms)
	}
	if ms[1].Path != "/mnt/My Disk" || !ms[1].ReadOnly || ms[1].FSType != "exfat" {
		t.Fatalf("escaped, remounted: %+v", ms[1])
	}
	if !ms[2].Remote || ms[0].Remote {
		t.Fatalf("remote: %+v", ms)
	}
}

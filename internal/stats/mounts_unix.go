// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build linux || darwin

package stats

// pseudoFS are file systems that hold no person's files.
var pseudoFS = map[string]bool{
	"proc": true, "sysfs": true, "devtmpfs": true, "devpts": true, "tmpfs": true, "ramfs": true, "securityfs": true,
	"cgroup": true, "cgroup2": true, "pstore": true, "bpf": true, "debugfs": true, "tracefs": true, "configfs": true,
	"fusectl": true, "mqueue": true, "hugetlbfs": true, "autofs": true, "binfmt_misc": true, "nsfs": true,
	"efivarfs": true, "rpc_pipefs": true, "nfsd": true, "selinuxfs": true, "squashfs": true, "overlay": true,
	"devfs": true, "nullfs": true, "fdesc": true, "portal": true, "fuse.gvfsd-fuse": true,
	"fuse.portal": true, "fuse.lxcfs": true, "fuse.snapfuse": true, "erofs": true,
}

// remoteFS are network file systems.
var remoteFS = map[string]bool{"nfs": true, "nfs4": true, "cifs": true, "smb3": true, "smbfs": true, "afpfs": true,
	"webdav": true, "fuse.sshfs": true, "fuse.rclone": true}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package fsx

import "io/fs"

// OnlineOnly reports whether the file's content is not on this computer: a
// cloud placeholder (OneDrive Files On-Demand, iCloud Drive with Optimize
// Mac Storage, Dropbox online-only and other sync clients built on the
// system's cloud-file API) or a file moved to offline storage. Reading such a
// file downloads it, filling the disk and the network for a scan that only
// meant to look. Its size and times are the real file's; only its bytes are
// elsewhere.
//
//   - Windows: FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS, FILE_ATTRIBUTE_RECALL_ON_OPEN
//     or FILE_ATTRIBUTE_OFFLINE, as the directory listing reports them;
//   - macOS: the SF_DATALESS flag of stat (File Provider and iCloud Drive);
//   - Linux and others: there is no standard marker, so never true. Cloud
//     mounts there (rclone, FUSE clients) download on read: leave them out
//     with -exclude-path.
func OnlineOnly(fi fs.FileInfo) bool { return fi != nil && onlineOnly(fi) }

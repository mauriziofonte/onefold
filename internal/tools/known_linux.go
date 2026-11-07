// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package tools

// systemd-inhibit holds the sleep lock of a long run (internal/power).
func init() { Known = append(Known, "systemd-inhibit") }

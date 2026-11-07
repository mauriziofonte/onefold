// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !windows && !darwin

package fsx

import "io/fs"

func onlineOnly(fs.FileInfo) bool { return false }

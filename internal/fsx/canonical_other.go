// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !darwin

package fsx

// onDiskName is the identity outside macOS: Linux names are exact, Windows
// comparisons fold case themselves.
func onDiskName(p string) string { return p }

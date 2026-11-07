// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux && !darwin

package fsx

// cloneFile: Windows has block cloning only on ReFS and Dev Drives
// (FSCTL_DUPLICATE_EXTENTS_TO_FILE), which onefold does not use yet: no
// test system offers one. Every clone falls back to a byte copy there.
func cloneFile(string, string) error { return ErrNoClone }

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !windows

package purge

const trashSupported = false

func moveToTrash(string) error { return ErrTrashUnsupported }

func trashVolume(string) (vol string, unc bool, driveType uint32) { return "", false, driveFixed }

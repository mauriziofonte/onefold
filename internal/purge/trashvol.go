// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package purge

import (
	"errors"
	"fmt"
)

// ErrNoTrash: the Recycle Bin option on a volume that has no Recycle Bin.
// Windows would delete there for good, so the purge refuses to start.
var ErrNoTrash = errors.New("the Recycle Bin option needs a local fixed drive")

// Drive types of GetDriveTypeW.
const (
	driveUnknown   = 0
	driveNoRootDir = 1
	driveRemovable = 2
	driveFixed     = 3
	driveRemote    = 4
	driveCDROM     = 5
	driveRAMDisk   = 6
)

// noTrashReason says why a volume has no Recycle Bin ("" = it has one): a
// UNC share or any drive type but a local fixed drive (network drives,
// removable media and RAM disks delete for good).
func noTrashReason(unc bool, driveType uint32) string {
	if unc {
		return "a network share"
	}
	switch driveType {
	case driveFixed:
		return ""
	case driveRemote:
		return "a network drive"
	case driveRemovable:
		return "a removable drive"
	case driveCDROM:
		return "an optical drive"
	case driveRAMDisk:
		return "a RAM disk"
	}
	return "a drive of unknown type"
}

// checkTrash refuses the Recycle Bin option when a path lies on a volume
// without one (trashVolume is per OS).
func checkTrash(paths []string) error {
	seen := map[string]bool{}
	for _, p := range paths {
		vol, unc, dt := trashVolume(p)
		if seen[vol] {
			continue
		}
		seen[vol] = true
		if why := noTrashReason(unc, dt); why != "" {
			return fmt.Errorf("%w: %s is on %s (Windows would delete for good)", ErrNoTrash, p, why)
		}
	}
	return nil
}

// MoveToTrash sends one file to the Recycle Bin (Windows) or the Trash
// (macOS, Linux), as purge -trash does; CheckTrash first refuses paths on
// volumes without one. Used by the duplicate cleanup.
func MoveToTrash(p string) error { return moveToTrash(p) }

// CheckTrash refuses the Trash for paths on a volume without one (network
// shares, removable drives on Windows).
func CheckTrash(paths []string) error { return checkTrash(paths) }

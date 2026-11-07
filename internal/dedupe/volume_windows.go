// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package dedupe

import (
	"path/filepath"
	"strings"
)

// sameVolume: both paths on one drive or share (a hard link cannot cross;
// a folder mounted inside a drive is caught when the link fails).
func sameVolume(a, b string) bool {
	va, vb := filepath.VolumeName(a), filepath.VolumeName(b)
	return va != "" && strings.EqualFold(va, vb)
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux && !darwin && !windows

package stats

func rawMounts() []Mount { return nil }

// VolumeOf describes the volume holding p (no details on this OS).
func VolumeOf(p string) Volume { return Volume{Root: p, Mount: p} }

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !crashtest

package fsx

import "io"

// CrashBuild reports whether this binary has the crash points.
const CrashBuild = false

// CrashPoint does nothing outside a crashtest build.
func CrashPoint(string) {}

// CrashWriter is w itself outside a crashtest build (io.Copy keeps its fast
// paths).
func CrashWriter(w io.Writer) io.Writer { return w }

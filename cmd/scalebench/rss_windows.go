// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import "os"

// maxRSSMiB: not measured on Windows (the benchmark runs on Linux).
func maxRSSMiB(*os.ProcessState) float64 { return 0 }

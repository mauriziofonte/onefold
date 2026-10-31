// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !windows

package gui

import "github.com/mauriziofonte/onefold/internal/tools"

// OpenWindow shows the GUI. Outside Windows there is no embedded browser: it
// asks the desktop to open the default browser (xdg-open, when present) and
// reports false, so the caller serves until idle or interrupted.
func OpenWindow(url, dataDir, title string, opened <-chan struct{}) bool {
	if cmd := tools.Command("xdg-open", url); cmd != nil {
		cmd.Start()
	}
	return false
}

// OpenBrowser opens url in the default browser (best-effort).
func OpenBrowser(url string) {
	if cmd := tools.Command("xdg-open", url); cmd != nil {
		cmd.Start()
	}
}

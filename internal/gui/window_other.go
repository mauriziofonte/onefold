// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !windows

package gui

import (
	"runtime"

	"github.com/mauriziofonte/onefold/internal/tools"
)

// OpenWindow shows the GUI. Outside Windows there is no embedded browser: it
// asks the desktop to open the default browser and reports false, so the
// caller serves until idle or interrupted.
func OpenWindow(url, dataDir, title string, opened <-chan struct{}) bool {
	OpenBrowser(url)
	return false
}

// OpenBrowser opens url in the default browser (best-effort): `open` on
// macOS, xdg-open elsewhere, when present.
func OpenBrowser(url string) {
	if cmd := tools.Command(browserOpener(), url); cmd != nil {
		cmd.Start()
	}
}

// browserOpener is the desktop's command that opens a URL.
func browserOpener() string {
	if runtime.GOOS == "darwin" {
		return "open"
	}
	return "xdg-open"
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"os"
	"path/filepath"
	"strings"
)

// startedAsApp reports whether this binary is the executable of an
// application bundle (onefold.app/Contents/MacOS/onefold, internal/pack):
// macOS starts it from Finder, the Dock or Launchpad with no arguments, and
// nobody reads a terminal, so it opens the GUI like a double-click on
// Windows. The same binary run by its path from a terminal with no
// arguments opens the GUI too: it says so on standard error.
func startedAsApp() bool {
	exe, err := os.Executable()
	return err == nil && inAppBundle(exe)
}

// inAppBundle: exe is <name>.app/Contents/MacOS/<file>.
func inAppBundle(exe string) bool {
	macos := filepath.Dir(exe)
	contents := filepath.Dir(macos)
	return filepath.Base(macos) == "MacOS" && filepath.Base(contents) == "Contents" &&
		strings.HasSuffix(filepath.Base(filepath.Dir(contents)), ".app")
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package statedir resolves the per-user durable directory of onefold: GUI
// jobs, logs and caches, and the tools downloaded by `onefold tools install`.
package statedir

import (
	"os"
	"path/filepath"
	"runtime"
)

// Dir is $ONEFOLD_STATE_DIR, else %LOCALAPPDATA%\onefold on Windows, else
// $XDG_STATE_HOME/onefold, else ~/.local/state/onefold. Never a temp dir: a
// run must survive a reboot.
func Dir() (string, error) {
	if d := os.Getenv("ONEFOLD_STATE_DIR"); d != "" {
		return filepath.Abs(d)
	}
	if runtime.GOOS == "windows" {
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			return filepath.Join(d, "onefold"), nil
		}
	}
	if d := os.Getenv("XDG_STATE_HOME"); d != "" && filepath.IsAbs(d) {
		return filepath.Join(d, "onefold"), nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".local", "state", "onefold"), nil
}

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

// Dir is $ONEFOLD_STATE_DIR, else %LOCALAPPDATA%\onefold on Windows,
// ~/Library/Application Support/onefold on macOS, else
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
	if runtime.GOOS == "darwin" {
		h, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(h, "Library", "Application Support", "onefold"), nil
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

// DirMode and FileMode are the permissions of what onefold keeps in its state
// dir: runs and logs hold archive paths, the GUI lock holds the secret that
// opens a session. Other local accounts get no access (on Windows the folder
// under %LOCALAPPDATA% already inherits a per-user ACL; the modes there only
// set the read-only bit, never on these).
const (
	DirMode  os.FileMode = 0o700
	FileMode os.FileMode = 0o600
)

// Ensure creates dir (the state dir) with DirMode and removes the group and
// other bits from an existing one, created by an older version with 0755:
// with the root closed nothing below it is reachable by other accounts, so
// older files inside keep their modes.
func Ensure(dir string) error {
	if err := os.MkdirAll(dir, DirMode); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		return nil
	}
	fi, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if p := fi.Mode().Perm(); p&0o077 != 0 {
		return os.Chmod(dir, p&^0o077)
	}
	return nil
}

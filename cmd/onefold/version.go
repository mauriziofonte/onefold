// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// buildInfo returns the commit (12 hex digits, "+dirty" when the tree had
// uncommitted changes) and the commit date (YYYY-MM-DD) Go recorded in the
// binary; empty when it was built outside a git checkout.
func buildInfo() (commit, date string) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "", ""
	}
	dirty := false
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			commit = s.Value
			if len(commit) > 12 {
				commit = commit[:12]
			}
		case "vcs.time":
			if len(s.Value) >= 10 {
				date = s.Value[:10]
			}
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if commit != "" && dirty {
		commit += "+dirty"
	}
	return commit, date
}

// buildLabel is the build of this binary in words: "commit abc123def456,
// 2026-11-01" (empty when unknown).
func buildLabel() string {
	commit, date := buildInfo()
	switch {
	case commit != "" && date != "":
		return "commit " + commit + ", " + date
	case commit != "":
		return "commit " + commit
	}
	return ""
}

// versionLine is what `onefold version` prints.
func versionLine() string {
	s := "onefold " + version
	if b := buildLabel(); b != "" {
		s += " (" + b + ")"
	}
	return fmt.Sprintf("%s %s/%s %s", s, runtime.GOOS, runtime.GOARCH, runtime.Version())
}

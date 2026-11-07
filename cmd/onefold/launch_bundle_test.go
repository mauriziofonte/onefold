// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"path/filepath"
	"testing"
)

func TestInAppBundle(t *testing.T) {
	for p, want := range map[string]bool{
		"/Applications/onefold.app/Contents/MacOS/onefold":           true,
		"/Users/me/Downloads/onefold 2.app/Contents/MacOS/onefold":   true,
		"/usr/local/bin/onefold":                                     false,
		"/opt/homebrew/Cellar/onefold/0.1.0/bin/onefold":             false,
		"/tmp/onefold.app/onefold":                                   false,
		"/tmp/Contents/MacOS/onefold":                                false,
		"/tmp/x.app/Contents/Resources/onefold":                      false,
		"C:/Users/me/onefold.app/Contents/MacOS/onefold-windows.exe": true,
	} {
		if got := inAppBundle(filepath.FromSlash(p)); got != want {
			t.Errorf("%s: %t, want %t", p, got, want)
		}
	}
}

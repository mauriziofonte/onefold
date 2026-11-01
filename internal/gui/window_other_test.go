// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !windows

package gui

import (
	"runtime"
	"testing"

	"github.com/mauriziofonte/onefold/internal/tools"
)

// On macOS `onefold gui` must reach the browser through `open`, which every
// Mac has; elsewhere xdg-open is optional (the URL is printed anyway).
func TestBrowserOpener(t *testing.T) {
	name := browserOpener()
	if runtime.GOOS == "darwin" {
		if name != "open" {
			t.Fatalf("opener %q, want open", name)
		}
		if !tools.Has(name) && !tools.Disabled() {
			t.Fatal("open not found on a Mac")
		}
		return
	}
	if name != "xdg-open" {
		t.Fatalf("opener %q, want xdg-open", name)
	}
}

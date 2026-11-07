// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package gui

import (
	"testing"
)

// Without a WebView2 runtime (simulated: the official override points at an
// empty folder) no window is created, the default browser gets the URL and
// OpenWindow returns at once (browser mode).
func TestWindowFallsBackToBrowser(t *testing.T) {
	t.Setenv("WEBVIEW2_BROWSER_EXECUTABLE_FOLDER", t.TempDir())
	var got string
	openBrowser = func(u string) { got = u }
	defer func() { openBrowser = OpenBrowser }()
	opened := make(chan struct{})
	if OpenWindow("http://127.0.0.1:1/?c=x", t.TempDir(), "onefold test", opened) {
		t.Fatal("a window was created without a runtime")
	}
	if got != "http://127.0.0.1:1/?c=x" {
		t.Fatalf("browser got %q", got)
	}
}

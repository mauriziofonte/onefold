// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build windows

package gui

import (
	"runtime"
	"time"

	"github.com/jchv/go-webview2"
	"github.com/jchv/go-webview2/webviewloader"
	"golang.org/x/sys/windows"

	"github.com/mauriziofonte/onefold/internal/winres"
)

// navRetry is how often the window repeats its first navigation until the
// page has loaded (opened closed); navRetryFor bounds the attempts.
const (
	navRetry    = 2 * time.Second
	navRetryFor = 5 * time.Minute
)

// OpenWindow shows the GUI in a native WebView2 window and blocks until the
// user closes it (true). Without the WebView2 runtime it opens the default
// browser instead and returns false at once. The first navigation can be
// refused (ERR_NETWORK_ACCESS_DENIED on a just-bound loopback port, seen for
// tens of seconds; Chromium's own auto-reload waits 1 s, 5 s, 30 s): the
// window navigates again every navRetry until opened is closed.
func OpenWindow(url, dataDir, title string, opened <-chan struct{}) bool {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	v, err := webviewloader.GetInstalledVersion()
	Logf("window: WebView2 runtime version=%q err=%v, profile %s", v, err, dataDir)
	width, height := windowSize(1360, 900)
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		DataPath:  dataDir, // persistent profile: localStorage survives restarts
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{Title: title, Width: width, Height: height, Center: true,
			IconId: winres.IconID},
	})
	if w == nil {
		Logf("window: WebView2 window not created: falling back to the default browser")
		openBrowser(url)
		return false
	}
	defer w.Destroy()
	Logf("window: created, navigating")
	w.Navigate(url)
	closed := make(chan struct{})
	go retryNavigate(w, url, opened, closed)
	w.Run()
	close(closed)
	Logf("window: closed by the user")
	return true
}

// retryNavigate repeats the navigation until the page has loaded, the window
// is closed or navRetryFor has elapsed.
func retryNavigate(w webview2.WebView, url string, opened, closed <-chan struct{}) {
	t0 := time.Now()
	t := time.NewTicker(navRetry)
	defer t.Stop()
	for n := 1; ; n++ {
		select {
		case <-opened:
			Logf("window: page loaded after %s", time.Since(t0).Round(time.Millisecond))
			return
		case <-closed:
			return
		case <-t.C:
			if time.Since(t0) > navRetryFor {
				Logf("window: page not loaded after %s: giving up the retries", navRetryFor)
				return
			}
			Logf("window: page not loaded yet, navigating again (attempt %d)", n+1)
			w.Dispatch(func() { w.Navigate(url) })
		}
	}
}

// openBrowser is OpenBrowser; tests replace it so no real browser opens.
var openBrowser = OpenBrowser

// OpenBrowser opens url in the default browser through the shell (no
// external binary).
func OpenBrowser(url string) {
	verb, _ := windows.UTF16PtrFromString("open")
	target, _ := windows.UTF16PtrFromString(url)
	err := windows.ShellExecute(0, verb, target, nil, nil, windows.SW_SHOWNORMAL)
	Logf("browser: ShellExecute open -> err=%v", err)
}

var (
	user32           = windows.NewLazySystemDLL("user32.dll")
	getDpiForSystem  = user32.NewProc("GetDpiForSystem")
	getSystemMetrics = user32.NewProc("GetSystemMetrics")
)

// windowSize scales a size given at 96 DPI to the system DPI (the manifest
// makes the process per-monitor DPI aware, so window sizes are physical
// pixels) and keeps it within 90% of the primary screen: go-webview2 centers
// with unsigned arithmetic, a window larger than the screen would land
// off-screen.
func windowSize(w, h uint) (uint, uint) {
	dpi := uint(96)
	if getDpiForSystem.Find() == nil {
		if r, _, _ := getDpiForSystem.Call(); r > 0 {
			dpi = uint(r)
		}
	}
	w, h = w*dpi/96, h*dpi/96
	sw, _, _ := getSystemMetrics.Call(0) // SM_CXSCREEN
	sh, _, _ := getSystemMetrics.Call(1) // SM_CYSCREEN
	if m := uint(sw) * 9 / 10; m > 0 && w > m {
		w = m
	}
	if m := uint(sh) * 9 / 10; m > 0 && h > m {
		h = m
	}
	Logf("window: %dx%d at %d DPI (screen %dx%d)", w, h, dpi, sw, sh)
	return w, h
}

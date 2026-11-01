// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/mauriziofonte/onefold/internal/config"
	"github.com/mauriziofonte/onefold/internal/gui"
)

type guiOptions struct {
	port             int
	noWindow, noOpen bool
	keep             bool
	stateDir         string
}

// cmdGUI starts the local web GUI.
func cmdGUI(args []string) {
	fs := flag.NewFlagSet("gui", flag.ExitOnError)
	port := fs.Int("port", 0, fmt.Sprintf("listen port on 127.0.0.1 (0 = %d, random when busy)", gui.DefaultPort))
	noWindow := fs.Bool("no-window", false, "do not open the native window (Windows): serve and open the browser")
	noOpen := fs.Bool("no-open", false, "do not open any window or browser: print the URL only")
	keep := fs.Bool("keep", false, "browser mode: keep serving when no page is open (default: exit after 2 idle minutes)")
	state := fs.String("state", "", "state directory for jobs, logs and caches (default: per-user data dir, see `onefold gui -h`)")
	_ = fs.Parse(args)
	runGUI(guiOptions{port: *port, noWindow: *noWindow, noOpen: *noOpen, keep: *keep, stateDir: *state})
}

type guiLock struct {
	PID int    `json:"pid"`
	URL string `json:"url"`
}

// aliveGUI reports whether the session link of a running instance answers.
func aliveGUI(url string) bool {
	c := http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Get(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusSeeOther
}

// redact drops the session token from a URL before logging it.
func redact(url string) string {
	if i := strings.Index(url, "?t="); i >= 0 {
		return url[:i] + "?t=…"
	}
	return url
}

func runGUI(o guiOptions) {
	gui.Logf = dlog
	defer func() {
		if r := recover(); r != nil {
			dlog("PANIC: %v\n%s", r, debug.Stack())
			os.Exit(1)
		}
	}()
	dlog("gui: options %+v", o)
	dir := o.stateDir
	if dir == "" {
		d, err := gui.StateDir()
		if err != nil {
			fatal(1, err)
		}
		dir = d
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fatal(1, err)
	}
	dlog("gui: state dir %s", dir)
	lockPath := filepath.Join(dir, "gui.json")
	var lk guiLock
	if b, err := os.ReadFile(lockPath); err == nil && json.Unmarshal(b, &lk) == nil && lk.URL != "" && aliveGUI(lk.URL) {
		// one instance per state dir: show the running one
		dlog("gui: instance already running at %s (pid %d): opening it", redact(lk.URL), lk.PID)
		fmt.Fprintln(os.Stderr, "onefold GUI already running:", lk.URL)
		if !o.noOpen {
			gui.OpenBrowser(lk.URL)
		}
		return
	}
	srv, err := gui.New(gui.Options{Port: o.port, StateDir: dir, Version: version, Build: buildLabel(),
		OnConfig: func(c config.Config) { setDiag(c.DiagLog) }})
	if err != nil {
		fatal(1, err)
	}
	url := srv.URL()
	dlog("gui: listening on %s", redact(url))
	b, _ := json.Marshal(guiLock{PID: os.Getpid(), URL: url})
	os.WriteFile(lockPath, b, 0o600)
	defer os.Remove(lockPath)

	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				dlog("PANIC (server): %v\n%s", r, debug.Stack())
				os.Exit(1)
			}
		}()
		if err := srv.Serve(); err != nil {
			dlog("gui: serve: %v", err)
			fmt.Fprintln(os.Stderr, "gui:", err)
		}
		close(done)
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		s := <-sig
		dlog("gui: signal %v: pausing the active job and exiting", s)
		srv.Shutdown() // pauses and persists the running job: resumable at next start
	}()
	fmt.Fprintf(os.Stderr, "onefold %s GUI: %s\n(state: %s; Ctrl+C pauses the running job and exits)\n", version, url, dir)

	if !o.noWindow && !o.noOpen {
		if gui.OpenWindow(url, filepath.Join(dir, "webview2"), "onefold", srv.Opened()) {
			dlog("gui: window closed: pausing the active job and exiting")
			srv.Shutdown() // window closed
			<-done
			dlog("gui: exit")
			return
		}
		dlog("gui: no native window: browser mode")
	} else if o.noWindow && !o.noOpen {
		gui.OpenBrowser(url)
	}
	// browser mode: no window to close, exit when idle
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			if !o.keep && srv.Idle(2*time.Minute) {
				dlog("gui: idle (no page open for 2 minutes, no job running): exiting")
				srv.Shutdown()
				<-done
				return
			}
		}
	}
}

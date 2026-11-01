// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package tools is the registry of OPTIONAL external programs (exiftool,
// ffprobe, ffmpeg, pdfinfo, pdftotext, antiword, 7z, unrar). Every caller has a
// pure-Go fallback or degrades gracefully when a tool is missing, which is the
// normal case on Windows.
//
// Lookup order: PATH, the directory of the running executable, <exe dir>/tools,
// ManagedDir (tools downloaded by `onefold tools install` or the GUI).
// ONEFOLD_NO_EXTERNAL=1 disables every tool (parity testing).
package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"sync"

	"github.com/mauriziofonte/onefold/internal/perf"
	"github.com/mauriziofonte/onefold/internal/statedir"
)

// Known lists every tool onefold may use.
var Known = []string{"exiftool", "ffprobe", "ffmpeg", "pdfinfo", "pdftotext", "antiword", "7z", "unrar"}

var (
	mu    sync.Mutex
	cache = map[string]string{} // name -> resolved path ("" = missing)
)

// Disabled reports whether external tools are globally disabled.
func Disabled() bool { return os.Getenv("ONEFOLD_NO_EXTERNAL") == "1" }

// Path resolves a tool (cached). ok=false if missing or disabled.
func Path(name string) (string, bool) {
	if Disabled() {
		return "", false
	}
	mu.Lock()
	defer mu.Unlock()
	if p, hit := cache[name]; hit {
		return p, p != ""
	}
	p := resolve(name)
	cache[name] = p
	return p, p != ""
}

// ManagedDir is where downloaded tools live: <state dir>/tools ("" when the
// state dir cannot be resolved).
func ManagedDir() string {
	d, err := statedir.Dir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "tools")
}

func resolve(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe), filepath.Join(filepath.Dir(exe), "tools"))
	}
	if d := ManagedDir(); d != "" {
		dirs = append(dirs, d)
	}
	if runtime.GOOS == "darwin" {
		// Homebrew (Apple Silicon, then Intel): a program not started from a
		// login shell (Finder, launchd) does not have it on PATH
		dirs = append(dirs, "/opt/homebrew/bin", "/usr/local/bin")
	}
	cands := []string{name}
	if runtime.GOOS == "windows" {
		cands = []string{name + ".exe", name + ".bat", name + ".cmd"}
		if name == "exiftool" {
			cands = append(cands, "exiftool(-k).exe")
		}
	}
	for _, d := range dirs {
		for _, c := range cands {
			p := filepath.Join(d, c)
			if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
				return p
			}
		}
	}
	return ""
}

// Has reports whether the tool is available.
func Has(name string) bool { _, ok := Path(name); return ok }

// Command returns an *exec.Cmd for the tool, or nil if it is unavailable.
func Command(name string, args ...string) *exec.Cmd {
	p, ok := Path(name)
	if !ok {
		return nil
	}
	cmd := exec.Command(p, args...)
	hideWindow(cmd)
	return cmd
}

// Output runs the tool and returns stdout; ok=false if missing. err is the
// process error (callers usually treat non-empty output as usable).
func Output(env []string, name string, args ...string) (out []byte, ok bool, err error) {
	cmd := Command(name, args...)
	if cmd == nil {
		return nil, false, nil
	}
	if env != nil {
		cmd.Env = append(os.Environ(), env...)
	}
	t0 := perf.Start()
	out, err = cmd.Output()
	perf.End("spawn:"+name, t0, 0)
	return out, true, err
}

// Report maps every known tool to its resolved path ("" = missing).
func Report() map[string]string {
	out := map[string]string{}
	for _, n := range Known {
		p, _ := Path(n)
		out[n] = p
	}
	return out
}

// Names returns Known sorted.
func Names() []string {
	n := append([]string(nil), Known...)
	sort.Strings(n)
	return n
}

// Reset clears the cache (tests, after an install).
func Reset() {
	mu.Lock()
	cache = map[string]string{}
	mu.Unlock()
}

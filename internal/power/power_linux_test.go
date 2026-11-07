// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package power

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/mauriziofonte/onefold/internal/testfix"
	"github.com/mauriziofonte/onefold/internal/tools"
)

// A stand-in systemd-inhibit on PATH: it records its arguments, holds the
// "lock" while its stdin is open, and records the release.
func fakeInhibit(t *testing.T, body string) string {
	dir := testfix.TempDir(t)
	if err := os.WriteFile(filepath.Join(dir, "systemd-inhibit"), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	tools.Reset()
	t.Cleanup(tools.Reset)
	return dir
}

func TestKeepAwakeLinux(t *testing.T) {
	dir := fakeInhibit(t, `echo "$@" > "${0%/*}/args"; while read -r _; do :; done; echo gone > "${0%/*}/released"`+"\n")
	release, err := keepAwake(Reason)
	if err != nil {
		t.Fatal(err)
	}
	var args []byte
	for i := 0; i < 100 && len(args) == 0; i++ {
		time.Sleep(20 * time.Millisecond)
		args, _ = os.ReadFile(filepath.Join(dir, "args"))
	}
	if !strings.Contains(string(args), "--what=sleep --who=onefold --why="+Reason+" --mode=block cat") {
		t.Errorf("arguments: %q", args)
	}
	if _, err := os.Stat(filepath.Join(dir, "released")); err == nil {
		t.Fatal("released before release")
	}
	release()
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(filepath.Join(dir, "released")); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the lock outlived its release")
}

// A refused lock (no logind, no right) is an error, not a hang.
func TestKeepAwakeRefused(t *testing.T) {
	fakeInhibit(t, "echo 'Failed to inhibit: Access denied' >&2; exit 1\n")
	release, err := KeepAwake(Reason)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for i := 0; i < 100 && Problem() == ""; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(Problem(), "Access denied") {
		t.Fatalf("refused lock: %q", Problem())
	}
	t.Setenv("PATH", testfix.TempDir(t))
	tools.Reset()
	if _, err := keepAwake(Reason); err == nil {
		t.Fatal("no systemd-inhibit, no error")
	}
}

func ioprio(tid int) int {
	r, _, _ := unix.Syscall(unix.SYS_IOPRIO_GET, ioprioWhoProcess, uintptr(tid), 0)
	return int(r)
}

// Every thread gets the lowest best-effort I/O level and nice 10; off
// restores the default I/O level.
func TestLowPriorityLinux(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	before, _ := unix.Getpriority(unix.PRIO_PROCESS, unix.Gettid())
	if err := SetLowPriority(true); err != nil {
		t.Fatal(err)
	}
	tids, _ := threads()
	for _, tid := range tids {
		if p := ioprio(tid); p != ioprioClassBE<<ioprioClassShift|7 {
			t.Errorf("thread %d: ioprio %#x", tid, p)
		}
	}
	// getpriority returns 20 - nice
	if p, _ := unix.Getpriority(unix.PRIO_PROCESS, unix.Gettid()); 20-p < nicePrio && 20-before < nicePrio {
		t.Errorf("nice %d", 20-p)
	}
	if err := SetLowPriority(false); err != nil {
		t.Fatal(err)
	}
	if p := ioprio(unix.Gettid()); p != ioprioClassBE<<ioprioClassShift|4 {
		t.Errorf("restored ioprio %#x", p)
	}
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build windows

package tools

import (
	"io"
	"os/exec"
	"testing"
	"time"
)

// A launcher whose child keeps the stdout pipe open (cmd.exe -> ping.exe, like
// exiftool.exe -> perl.exe): killing the tree must close the pipe at once.
func TestKillTreeClosesChildPipes(t *testing.T) {
	cmd := exec.Command("cmd.exe", "/c", "ping -n 60 127.0.0.1 >nul & echo done")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	tree, err := StartTree(cmd)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()
	time.Sleep(500 * time.Millisecond) // let cmd.exe start ping
	read := make(chan struct{})
	go func() { io.Copy(io.Discard, out); close(read) }()
	tree.Kill()
	select {
	case <-read:
	case <-time.After(10 * time.Second):
		t.Fatal("stdout still open 10 s after the tree kill: the child survived")
	}
	_ = cmd.Wait()
}

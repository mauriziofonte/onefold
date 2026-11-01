// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !windows

package tools

import "os/exec"

// Tree is a started tool (see the Windows variant: elsewhere the tools run
// directly, without a launcher process, so killing the process is enough).
type Tree struct{ cmd *exec.Cmd }

// StartTree starts cmd.
func StartTree(cmd *exec.Cmd) (*Tree, error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &Tree{cmd: cmd}, nil
}

// Kill ends the tool.
func (t *Tree) Kill() { _ = t.cmd.Process.Kill() }

// Close releases what StartTree holds (nothing here).
func (t *Tree) Close() {}

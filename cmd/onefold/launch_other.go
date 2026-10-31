// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !windows

package main

// ownsConsole: only Windows opens a console for a double-clicked program.
func ownsConsole() bool { return false }

func detachConsole() {}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build crashtest

package fsx

import (
	"io"
	"os"
	"strconv"
	"sync/atomic"
)

// CrashBuild reports whether this binary has the crash points.
const CrashBuild = true

var (
	crashAt, _ = strconv.ParseInt(os.Getenv(CrashEnv), 10, 64)
	crashN     atomic.Int64
)

func crashHit() bool { return crashAt > 0 && crashN.Add(1) == crashAt }

func crash(op string) {
	os.Stderr.WriteString("onefold: crash test: stopped at " + op + " " + strconv.FormatInt(crashAt, 10) + "\n")
	os.Exit(CrashExit)
}

// CrashPoint stops the process when it is the crash point of the run.
func CrashPoint(op string) {
	if crashHit() {
		crash(op)
	}
}

type crashWriter struct{ w io.Writer }

func (c crashWriter) Write(p []byte) (int, error) {
	if crashHit() {
		c.w.Write(p[:len(p)/2]) // a torn write
		crash("write")
	}
	return c.w.Write(p)
}

// CrashWriter is w with a crash point before each write.
func CrashWriter(w io.Writer) io.Writer { return crashWriter{w} }

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package fsx

// Crash points: the writes, fsyncs, renames, links, clones and removals of
// the commands that change files (merge, rearrange, purge-sources, apply)
// pass through CrashPoint or CrashWriter. In a normal build they do nothing.
// A binary built with `-tags crashtest` stops at the Nth one when
// ONEFOLD_TEST_CRASH_AT=N, at once and with CrashExit, as a kill -9 would:
// no deferred cleanup runs and nothing buffered is written; the Nth write
// writes half its bytes first (a torn write). cmd/onefold's crash tests run
// a command with N = 1, 2, ... until it completes, rerun it after each
// stop, and check that no file was lost or damaged. A power cut is not
// simulated: what was written but not flushed survives a process crash.

// CrashExit is the exit code of a crash-test stop.
const CrashExit = 197

// CrashEnv names the crash point (1 = the first) a crashtest build stops at.
const CrashEnv = "ONEFOLD_TEST_CRASH_AT"

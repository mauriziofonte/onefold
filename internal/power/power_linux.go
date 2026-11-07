// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package power

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"

	"github.com/mauriziofonte/onefold/internal/tools"
)

// The lock is held by `systemd-inhibit ... cat`: cat reads the pipe onefold
// holds, so the lock ends when release closes it, or when onefold ends in
// any way (the pipe closes with the process). It starts without waiting:
// when systemd-inhibit gives up early (no logind, a session without the
// right), Problem says why.
func keepAwake(why string) (func(), error) {
	cmd := tools.Command("systemd-inhibit", "--what=sleep", "--who=onefold", "--why="+why, "--mode=block", "cat")
	if cmd == nil {
		return nil, errors.New("systemd-inhibit not found (no systemd): the computer may sleep during the run")
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	var stderr strings.Builder
	cmd.Stderr = &limited{w: &stderr, n: 4096}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	exited := make(chan struct{})
	var released atomic.Bool
	go func() {
		err := cmd.Wait()
		if !released.Load() {
			setProblem(fmt.Errorf("systemd-inhibit: %v: %s", err, strings.TrimSpace(stderr.String())))
		}
		close(exited)
	}()
	return func() {
		released.Store(true)
		in.Close()
		select {
		case <-exited:
		case <-time.After(2 * time.Second):
			cmd.Process.Kill()
		}
	}, nil
}

type limited struct {
	w io.Writer
	n int
}

func (l *limited) Write(p []byte) (int, error) {
	if l.n > 0 {
		k := min(len(p), l.n)
		l.w.Write(p[:k])
		l.n -= k
	}
	return len(p), nil
}

const (
	ioprioWhoProcess = 1
	ioprioClassShift = 13
	ioprioClassBE    = 2
	nicePrio         = 10
)

// I/O priority and nice values belong to threads on Linux: every thread of
// the process gets them; threads started later inherit them.
func setLowPriority(on bool) error {
	level, nice := 4, 0 // the best-effort default
	if on {
		level, nice = 7, nicePrio
	}
	tids, err := threads()
	if err != nil {
		return err
	}
	var first error
	for _, tid := range tids {
		v := ioprioClassBE<<ioprioClassShift | level
		if _, _, e := unix.Syscall(unix.SYS_IOPRIO_SET, ioprioWhoProcess, uintptr(tid), uintptr(v)); e != 0 && e != unix.ESRCH && first == nil {
			first = fmt.Errorf("ioprio_set: %w", e)
		}
		// raising the priority back needs privileges: off keeps the nice value
		if on {
			if err := unix.Setpriority(unix.PRIO_PROCESS, tid, nice); err != nil && err != unix.ESRCH && first == nil {
				first = fmt.Errorf("setpriority: %w", err)
			}
		}
	}
	return first
}

func threads() ([]int, error) {
	f, err := os.Open("/proc/self/task")
	if err != nil {
		return nil, err
	}
	defer f.Close()
	names, err := f.Readdirnames(-1)
	if err != nil {
		return nil, err
	}
	var out []int
	for _, n := range names {
		if id, err := strconv.Atoi(n); err == nil {
			out = append(out, id)
		}
	}
	return out, nil
}

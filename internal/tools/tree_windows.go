// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build windows

package tools

import (
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Tree is a started tool together with every process it starts: on Windows a
// launcher may run the real program as a child (exiftool.exe runs
// exiftool_files\perl.exe), and killing only the launcher would leave the
// child holding the pipes open.
type Tree struct {
	cmd *exec.Cmd
	job windows.Handle // 0 when the job object could not be set up
}

// StartTree starts cmd inside a job object that kills its processes when
// killed or closed (also when onefold itself exits).
func StartTree(cmd *exec.Cmd) (*Tree, error) {
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	t := &Tree{cmd: cmd}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return t, nil // plain process kill only
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE},
	}
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return t, nil
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		windows.CloseHandle(job)
		return t, nil
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		windows.CloseHandle(job)
		return t, nil
	}
	t.job = job
	return t, nil
}

// Kill ends the tool and every process it started.
func (t *Tree) Kill() {
	if t.job != 0 {
		_ = windows.TerminateJobObject(t.job, 1)
		return
	}
	_ = t.cmd.Process.Kill()
}

// Close releases the job after Wait (killing any process still in it).
func (t *Tree) Close() {
	if t.job != 0 {
		windows.CloseHandle(t.job)
		t.job = 0
	}
}

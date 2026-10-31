// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build windows

package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// openDiag opens onefold-gui.log next to the executable (fallback:
// %LOCALAPPDATA%\onefold when the exe folder is not writable) and routes the
// standard logger there too: go-webview2 reports its failures with
// log.Printf / log.Fatal, which a console-less process would lose.
func openDiag() {
	var f *os.File
	if exe, err := os.Executable(); err == nil {
		f, _ = os.OpenFile(filepath.Join(filepath.Dir(exe), "onefold-gui.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	}
	if f == nil {
		if d := os.Getenv("LOCALAPPDATA"); d != "" {
			os.MkdirAll(filepath.Join(d, "onefold"), 0o755)
			f, _ = os.OpenFile(filepath.Join(d, "onefold", "onefold-gui.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		}
	}
	if f == nil {
		return
	}
	diagFile = f
	flags := log.LstdFlags | log.Lmicroseconds
	diagLog.Store(log.New(f, fmt.Sprintf("[%d] ", os.Getpid()), flags))
	log.SetOutput(f)
	log.SetFlags(flags)
	log.SetPrefix(fmt.Sprintf("[%d] lib: ", os.Getpid()))
}

// diagFile is the open sidecar log (nil when off).
var diagFile *os.File

// setDiag switches the diagnostic log on or off while running (GUI Config).
func setDiag(on bool) {
	switch {
	case on && diagLog.Load() == nil:
		openDiag()
		dlog("diag: log on (version=%s)", version)
	case !on && diagLog.Load() != nil:
		dlog("diag: log off")
		diagLog.Store(nil)
		log.SetOutput(os.Stderr)
		log.SetPrefix("")
		diagFile.Close()
		diagFile = nil
	}
}

// processes maps pid -> (parent pid, exe name) from a toolhelp snapshot.
func processes() map[uint32][2]string {
	out := map[uint32][2]string{}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return out
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		out[e.ProcessID] = [2]string{fmt.Sprint(e.ParentProcessID), windows.UTF16ToString(e.ExeFile[:])}
	}
	return out
}

func parentName() (uint32, string) {
	ps := processes()
	me, ok := ps[uint32(os.Getpid())]
	if !ok {
		return 0, ""
	}
	var ppid uint32
	fmt.Sscan(me[0], &ppid)
	return ppid, ps[ppid][1]
}

// parentIsExplorer: started by the Windows shell (double-click, shortcut,
// Start menu), whatever terminal hosts the console.
func parentIsExplorer() bool {
	_, name := parentName()
	return strings.EqualFold(name, "explorer.exe")
}

func handleType(h windows.Handle) string {
	t, err := windows.GetFileType(h)
	if err != nil {
		return "err"
	}
	switch t {
	case windows.FILE_TYPE_CHAR:
		return "char"
	case windows.FILE_TYPE_PIPE:
		return "pipe"
	case windows.FILE_TYPE_DISK:
		return "disk"
	}
	return fmt.Sprint(t)
}

// launchInfo describes how the process was started: console processes,
// parent, terminal hints, standard handles.
func launchInfo() string {
	var ids [16]uint32
	n, _, _ := procGetConsoleProcessList.Call(uintptr(unsafe.Pointer(&ids[0])), uintptr(len(ids)))
	ps := processes()
	var cons []string
	for i := 0; i < int(n) && i < len(ids); i++ {
		cons = append(cons, fmt.Sprintf("%d:%s", ids[i], ps[ids[i]][1]))
	}
	ppid, pname := parentName()
	return fmt.Sprintf("consoleProcs=%d [%s] parent=%d:%s WT_SESSION=%q stdin=%s stdout=%s",
		n, strings.Join(cons, " "), ppid, pname, os.Getenv("WT_SESSION"),
		handleType(windows.Handle(os.Stdin.Fd())), handleType(windows.Handle(os.Stdout.Fd())))
}

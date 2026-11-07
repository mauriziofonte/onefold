// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package power

import (
	"fmt"
	"sync"

	"github.com/ebitengine/purego"
	"golang.org/x/sys/unix"
)

const (
	cfStringEncodingUTF8 = 0x08000100
	assertionLevelOn     = 255
	iopolTypeDisk        = 0
	iopolScopeProcess    = 0
	iopolDefault         = 0
	iopolThrottle        = 3
)

var (
	loadOnce sync.Once
	loadErr  error

	cfString  func(alloc uintptr, s string, enc uint32) uintptr
	cfRelease func(uintptr)
	assert    func(typ uintptr, level uint32, name uintptr, id *uint32) int32
	release   func(id uint32) int32
	setIOPol  func(typ, scope, policy int32) int32
)

// IOKit and CoreFoundation, loaded on the first use only.
func load() error {
	loadOnce.Do(func() {
		cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			loadErr = err
			return
		}
		iokit, err := purego.Dlopen("/System/Library/Frameworks/IOKit.framework/IOKit", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			loadErr = err
			return
		}
		libc, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			loadErr = err
			return
		}
		purego.RegisterLibFunc(&cfString, cf, "CFStringCreateWithCString")
		purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
		purego.RegisterLibFunc(&assert, iokit, "IOPMAssertionCreateWithName")
		purego.RegisterLibFunc(&release, iokit, "IOPMAssertionRelease")
		purego.RegisterLibFunc(&setIOPol, libc, "setiopolicy_np")
	})
	return loadErr
}

func keepAwake(why string) (func(), error) {
	if err := load(); err != nil {
		return nil, err
	}
	typ := cfString(0, "PreventUserIdleSystemSleep", cfStringEncodingUTF8)
	name := cfString(0, why, cfStringEncodingUTF8)
	defer cfRelease(typ)
	defer cfRelease(name)
	var id uint32
	if r := assert(typ, assertionLevelOn, name, &id); r != 0 {
		return nil, fmt.Errorf("IOPMAssertionCreateWithName: 0x%x", uint32(r))
	}
	return func() { release(id) }, nil
}

func setLowPriority(on bool) error {
	if err := load(); err != nil {
		return err
	}
	pol, nice := int32(iopolDefault), 0
	if on {
		pol, nice = iopolThrottle, 10
	}
	if r := setIOPol(iopolTypeDisk, iopolScopeProcess, pol); r != 0 {
		return fmt.Errorf("setiopolicy_np: %d", r)
	}
	// raising the priority back needs privileges: off keeps the nice value
	if on {
		return unix.Setpriority(unix.PRIO_PROCESS, 0, nice)
	}
	return nil
}

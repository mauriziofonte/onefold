// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package purge

import (
	"errors"
	"fmt"
	"runtime"
	"sync"

	"github.com/ebitengine/purego"
)

const trashSupported = true

// The Trash goes through -[NSFileManager trashItemAtURL:resultingItemURL:error:],
// the call Finder uses, so "Put Back" works. The Objective-C runtime is
// reached with purego (no cgo) and loaded on the first use only.
var (
	objcOnce sync.Once
	objcErr  error

	poolPush func() uintptr
	poolPop  func(uintptr)
	getClass func(name string) uintptr
	selName  func(name string) uintptr

	// objc_msgSend, once per signature used
	send      func(id, sel uintptr) uintptr
	sendStr   func(id, sel uintptr, s string) uintptr
	sendID    func(id, sel, a uintptr) uintptr
	sendCStr  func(id, sel uintptr) string
	sendTrash func(id, sel, url uintptr, result, nserr *uintptr) bool

	clsFileManager, clsString, clsURL                      uintptr
	selDefault, selWithUTF8, selFileURL, selTrash, selPath uintptr
	selUTF8, selDescription                                uintptr
)

func loadObjC() error {
	objcOnce.Do(func() {
		objc, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW)
		if err != nil {
			objcErr = err
			return
		}
		if _, err := purego.Dlopen("/System/Library/Frameworks/Foundation.framework/Foundation", purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
			objcErr = err
			return
		}
		msg, err := purego.Dlsym(objc, "objc_msgSend")
		if err != nil {
			objcErr = err
			return
		}
		purego.RegisterLibFunc(&poolPush, objc, "objc_autoreleasePoolPush")
		purego.RegisterLibFunc(&poolPop, objc, "objc_autoreleasePoolPop")
		purego.RegisterLibFunc(&getClass, objc, "objc_getClass")
		purego.RegisterLibFunc(&selName, objc, "sel_registerName")
		purego.RegisterFunc(&send, msg)
		purego.RegisterFunc(&sendStr, msg)
		purego.RegisterFunc(&sendID, msg)
		purego.RegisterFunc(&sendCStr, msg)
		purego.RegisterFunc(&sendTrash, msg)
		clsFileManager, clsString, clsURL = getClass("NSFileManager"), getClass("NSString"), getClass("NSURL")
		if clsFileManager == 0 || clsString == 0 || clsURL == 0 {
			objcErr = errors.New("objc: Foundation classes not found")
			return
		}
		selDefault = selName("defaultManager")
		selWithUTF8 = selName("stringWithUTF8String:")
		selFileURL = selName("fileURLWithPath:")
		selTrash = selName("trashItemAtURL:resultingItemURL:error:")
		selPath = selName("path")
		selUTF8 = selName("UTF8String")
		selDescription = selName("localizedDescription")
	})
	return objcErr
}

// moveToTrash sends one file or empty directory to the Trash of its volume.
// On a volume without a Trash (most network shares) macOS refuses and the
// file stays where it is.
func moveToTrash(p string) error {
	_, err := trashItem(p)
	return err
}

// trashItem moves p to the Trash and returns where it landed.
func trashItem(p string) (string, error) {
	if err := loadObjC(); err != nil {
		return "", fmt.Errorf("trash: %v", err)
	}
	// autorelease pools belong to a thread
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := poolPush()
	defer poolPop(pool)

	str := sendStr(clsString, selWithUTF8, p)
	if str == 0 {
		return "", errors.New("trash: path is not valid UTF-8")
	}
	url := sendID(clsURL, selFileURL, str)
	fm := send(clsFileManager, selDefault)
	var landed, nserr uintptr
	if !sendTrash(fm, selTrash, url, &landed, &nserr) {
		why := "unknown error"
		if nserr != 0 {
			why = sendCStr(send(nserr, selDescription), selUTF8)
		}
		return "", fmt.Errorf("trash: %s", why)
	}
	if landed == 0 {
		return "", nil
	}
	return sendCStr(send(landed, selPath), selUTF8), nil
}

// trashVolume: macOS finds the Trash of each volume itself and refuses,
// without deleting, where there is none, so every volume passes the check.
func trashVolume(string) (vol string, unc bool, driveType uint32) { return "", false, driveFixed }

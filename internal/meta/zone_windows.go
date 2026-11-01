// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build windows

package meta

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	icuDLL        = windows.NewLazySystemDLL("icu.dll")
	icuWinToIANA  = icuDLL.NewProc("ucal_getTimeZoneIDForWindowsID")
	userGeoName   = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetUserDefaultGeoName")
	errNoICUZone  = errors.New("icu.dll cannot map the Windows time zone")
	tzRegistryKey = `SYSTEM\CurrentControlSet\Control\TimeZoneInformation`
)

// hostZoneName maps the Windows zone (TimeZoneKeyName, e.g. "W. Europe
// Standard Time") to its IANA name through the ICU that ships with Windows 10
// 1903 and later (CLDR windowsZones), for the user's region: one Windows zone
// covers several countries ("W. Europe": Europe/Rome in Italy, Europe/Berlin
// by default). Without ICU the zone must be configured.
func hostZoneName() (string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, tzRegistryKey, registry.QUERY_VALUE)
	if err != nil {
		return "", err
	}
	defer k.Close()
	win, _, err := k.GetStringValue("TimeZoneKeyName")
	if err != nil {
		return "", err
	}
	if n, err := windowsToIANA(win, userRegion()); err == nil {
		return n, nil
	}
	return windowsToIANA(win, "")
}

// userRegion is the ISO 3166 code of the user's region ("IT"), "" when
// Windows does not say.
func userRegion() string {
	if userGeoName.Find() != nil {
		return ""
	}
	buf := make([]uint16, 16)
	n, _, _ := userGeoName.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	r := windows.UTF16ToString(buf)
	if len(r) != 2 { // a UN M.49 area code ("001"): no country
		return ""
	}
	return r
}

// windowsToIANA maps a Windows zone to IANA for region ("" = the CLDR
// default, "001").
func windowsToIANA(win, region string) (string, error) {
	if err := icuWinToIANA.Find(); err != nil {
		return "", errNoICUZone
	}
	in, err := windows.UTF16FromString(win)
	if err != nil {
		return "", err
	}
	var reg uintptr // const char*: NULL = default region
	if region != "" {
		b, err := windows.BytePtrFromString(region)
		if err != nil {
			return "", err
		}
		reg = uintptr(unsafe.Pointer(b))
	}
	out := make([]uint16, 128)
	var status int32 // UErrorCode: > 0 is a failure
	n, _, _ := icuWinToIANA.Call(
		uintptr(unsafe.Pointer(&in[0])), uintptr(len(in)-1), reg,
		uintptr(unsafe.Pointer(&out[0])), uintptr(len(out)), uintptr(unsafe.Pointer(&status)))
	if status > 0 || int32(n) <= 0 || int(int32(n)) > len(out) {
		return "", errNoICUZone
	}
	return windows.UTF16ToString(out[:int32(n)]), nil
}

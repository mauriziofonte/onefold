// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build windows

package meta

import "testing"

// TestWindowsZoneToIANA: the Windows zone of the registry becomes an IANA name
// through the ICU of Windows (CLDR windowsZones, default region).
func TestWindowsZoneToIANA(t *testing.T) {
	for win, want := range map[string]string{
		"W. Europe Standard Time": "Europe/Berlin", "Eastern Standard Time": "America/New_York",
		"Tokyo Standard Time": "Asia/Tokyo", "UTC": "Etc/UTC",
	} {
		if got, err := windowsToIANA(win, ""); err != nil || got != want {
			t.Errorf("%s -> %q %v, want %s", win, got, err, want)
		}
	}
	for region, want := range map[string]string{"IT": "Europe/Rome", "DE": "Europe/Berlin", "CH": "Europe/Zurich"} {
		if got, err := windowsToIANA("W. Europe Standard Time", region); err != nil || got != want {
			t.Errorf("W. Europe in %s -> %q %v, want %s", region, got, err, want)
		}
	}
	name, err := hostZoneName()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadZone(name); err != nil {
		t.Fatalf("host zone %q: %v", name, err)
	}
	t.Logf("host zone: %s (region %q)", name, userRegion())
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import (
	"runtime"
	"testing"
)

// TestResolveZone: configured zone first, then $TZ, then the host; never
// "Local" or a name another host could not load.
func TestResolveZone(t *testing.T) {
	t.Setenv("TZ", "Asia/Tokyo")
	if l, src, err := ResolveZone("America/New_York"); err != nil || l.String() != "America/New_York" || src != "config" {
		t.Fatalf("config: %v %s %v", l, src, err)
	}
	if l, src, err := ResolveZone(""); err != nil || l.String() != "Asia/Tokyo" || src != "TZ" {
		t.Fatalf("TZ: %v %s %v", l, src, err)
	}
	t.Setenv("TZ", ":/usr/share/zoneinfo/Europe/Paris")
	if l, _, err := ResolveZone(""); err != nil || l.String() != "Europe/Paris" {
		t.Fatalf("TZ path: %v %v", l, err)
	}
	t.Setenv("TZ", "Mars/Olympus")
	if _, _, err := ResolveZone(""); err == nil {
		t.Fatal("an unknown $TZ must fail, not fall back")
	}
	for _, bad := range []string{"", "Local", "local", "Nowhere/City"} {
		if _, err := LoadZone(bad); err == nil {
			t.Errorf("LoadZone(%q) must fail", bad)
		}
	}
	if _, err := LoadZone("UTC"); err != nil {
		t.Errorf("UTC: %v", err)
	}
}

// TestCanonicalVideoFollowsZone: a UTC creation time becomes a wall clock of
// the canonical zone; the same clip has another name in New York than in Rome.
func TestCanonicalVideoFollowsZone(t *testing.T) {
	defer SetZone(Zone())
	ny, _ := LoadZone("America/New_York")
	SetZone(ny)
	if got, _ := CanonicalVideo("2021-04-04T05:26:52Z", 10); got != "2021-04-04 01:26:42" {
		t.Errorf("New York: %s", got)
	}
	rome, _ := LoadZone("Europe/Rome")
	SetZone(rome)
	if got, _ := CanonicalVideo("2021-04-04T05:26:52Z", 10); got != "2021-04-04 07:26:42" {
		t.Errorf("Rome: %s", got)
	}
	if ZoneName() != "Europe/Rome" {
		t.Errorf("ZoneName %s", ZoneName())
	}
}

// TestDayLastDates: XX-XX-YYYY names give a date only when one reading is
// valid, both readings agree, or the configured order decides.
func TestDayLastDates(t *testing.T) {
	defer SetDateOrder("")
	cases := []struct{ name, order, want string }{
		{"scan 25-03-2020.jpg", "", "2020-03-25 12:00:00"},
		{"scan 03-25-2020.jpg", "", "2020-03-25 12:00:00"},
		{"scan 05-05-2020.jpg", "", "2020-05-05 12:00:00"},
		{"scan 03-04-2020.jpg", "", ""},
		{"scan 03-04-2020.jpg", DMY, "2020-04-03 12:00:00"},
		{"scan 03-04-2020.jpg", MDY, "2020-03-04 12:00:00"},
		{"scan 25-03-2020.jpg", MDY, "2020-03-25 12:00:00"},
		{"scan 31-31-2020.jpg", DMY, ""},
	}
	for _, c := range cases {
		SetDateOrder(c.order)
		got, ok := DeduceFromFilename(c.name)
		if got != c.want || ok != (c.want != "") {
			t.Errorf("%s (order %q) = %q %t, want %q", c.name, c.order, got, ok, c.want)
		}
	}
}

// On macOS and Windows the host always names its zone (macOS: the
// /etc/localtime link into /var/db/timezone/zoneinfo; Windows: the registry),
// and the name loads.
func TestHostZoneName(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("Linux images may lack /etc/localtime: ErrNoZone there is covered by TestResolveZone")
	}
	n, err := hostZoneName()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LoadZone(n); err != nil {
		t.Fatalf("host zone %q: %v", n, err)
	}
	if got := zoneNameFromPath("/var/db/timezone/zoneinfo/Europe/Rome"); got != "Europe/Rome" {
		t.Fatalf("macOS link target: %q", got)
	}
}

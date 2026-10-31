// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	_ "time/tzdata" // embedded zoneinfo (Windows has none)
)

// The canonical zone: every "2006-01-02 15:04:05" time of a manifest or
// a plan is a wall clock in this zone, and every artifact records its name.
// One zone per process: the CLI and the GUI resolve it at start (ResolveZone,
// SetZone); a library user that never sets it gets the host zone.

var (
	zone     atomic.Pointer[time.Location]
	zoneOnce sync.Once
	zoneErr  error
)

// ErrNoZone: the host zone could not be named as an IANA zone and none is
// configured. Never a silent fallback: a wrong zone shifts every name.
var ErrNoZone = errors.New("time zone unknown: set it with `onefold config set timezone Area/City` (IANA name, e.g. Europe/Rome, America/New_York)")

// Zone is the canonical wall-clock zone of every manifest and plan time
// ("2006-01-02 15:04:05" strings carry no offset): consumers that turn such a
// string back into an instant (mtime, sidecar dates) must parse it here, never
// in the host zone. It panics when no zone is set and the host zone cannot be
// determined: commands check ZoneReady first.
func Zone() *time.Location {
	if err := ZoneReady(); err != nil {
		panic("onefold: " + err.Error())
	}
	return zone.Load()
}

// ZoneReady sets the host zone when none was set and reports whether a
// canonical zone is available.
func ZoneReady() error {
	if zone.Load() != nil {
		return nil
	}
	zoneOnce.Do(func() {
		l, _, err := ResolveZone("")
		if err != nil {
			zoneErr = err
			return
		}
		zone.CompareAndSwap(nil, l)
	})
	if zone.Load() != nil {
		return nil
	}
	return zoneErr
}

// SetZone makes l the canonical zone of this process.
func SetZone(l *time.Location) { zone.Store(l) }

// ZoneName is the IANA name of the canonical zone, as written in manifests
// and plans.
func ZoneName() string { return Zone().String() }

// LoadZone loads an IANA zone by name. "Local" and "" are refused: they name
// no zone another host could load.
func LoadZone(name string) (*time.Location, error) {
	if name == "" || strings.EqualFold(name, "Local") {
		return nil, fmt.Errorf("time zone %q: use an IANA name (Area/City, e.g. Europe/Rome) or UTC", name)
	}
	l, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("time zone %q: not an IANA zone (Area/City, e.g. Europe/Rome) or UTC", name)
	}
	return l, nil
}

// ResolveZone picks the canonical zone: the configured one, else $TZ, else the
// host zone. source says which ("config", "TZ", "host").
func ResolveZone(configured string) (l *time.Location, source string, err error) {
	if configured != "" {
		l, err = LoadZone(configured)
		return l, "config", err
	}
	if tz, ok := os.LookupEnv("TZ"); ok && tz != "" {
		name := zoneNameFromPath(strings.TrimPrefix(tz, ":"))
		if l, err = LoadZone(name); err != nil {
			return nil, "TZ", fmt.Errorf("$TZ: %w", err)
		}
		return l, "TZ", nil
	}
	name, err := hostZoneName()
	if err != nil || name == "" {
		return nil, "host", ErrNoZone
	}
	if l, err = LoadZone(name); err != nil {
		return nil, "host", ErrNoZone
	}
	return l, "host", nil
}

// zoneNameFromPath turns a zoneinfo file path (/usr/share/zoneinfo/Europe/Rome,
// /var/db/timezone/zoneinfo/Europe/Rome) into its zone name; other values are
// returned unchanged.
func zoneNameFromPath(p string) string {
	if i := strings.LastIndex(p, "zoneinfo/"); i >= 0 {
		return p[i+len("zoneinfo/"):]
	}
	return p
}

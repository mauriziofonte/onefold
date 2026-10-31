// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !windows

package meta

import (
	"errors"
	"os"
	"strings"
)

// hostZoneName reads the IANA name of the system zone: the target of the
// /etc/localtime link (Linux, macOS), else /etc/timezone (Debian family).
func hostZoneName() (string, error) {
	if t, err := os.Readlink("/etc/localtime"); err == nil {
		if n := zoneNameFromPath(t); n != t {
			return n, nil
		}
	}
	if b, err := os.ReadFile("/etc/timezone"); err == nil {
		if n := strings.TrimSpace(string(b)); n != "" {
			return n, nil
		}
	}
	return "", errors.New("no /etc/localtime link nor /etc/timezone")
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import (
	"os"
	"testing"
)

// TestMain pins the canonical zone: the expected times of this package are
// Europe/Rome wall clocks, whatever the zone of the machine running them.
func TestMain(m *testing.M) {
	l, err := LoadZone("Europe/Rome")
	if err != nil {
		panic(err)
	}
	SetZone(l)
	os.Exit(m.Run())
}

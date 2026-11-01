// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package gui

import (
	"os"
	"testing"

	"github.com/mauriziofonte/onefold/internal/meta"
)

// TestMain stops the exiftool readers the tests started: they do not exit
// with the test binary.
func TestMain(m *testing.M) {
	code := m.Run()
	meta.CloseExifReaders()
	os.Exit(code)
}

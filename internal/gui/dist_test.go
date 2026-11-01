// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package gui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The embedded dist must be built from the current frontend sources: after
// editing internal/gui/web, run `make web` and commit dist/.
func TestWebDistInSync(t *testing.T) {
	want, err := SourceHash("web")
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join("web", "dist", ".source-hash"))
	if err != nil {
		t.Fatalf("web/dist/.source-hash missing: run `make web` (%v)", err)
	}
	if strings.TrimSpace(string(got)) != want {
		t.Fatalf("web/dist is stale (built from other sources): run `make web` and commit internal/gui/web/dist")
	}
	if _, err := distFS().Open("index.html"); err != nil {
		t.Fatalf("embedded dist has no index.html: %v", err)
	}
}

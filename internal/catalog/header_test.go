// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package catalog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mauriziofonte/onefold/internal/meta"
)

// TestManifestZone: a manifest records the canonical zone of its run; loading
// it in another zone, or without the header, is refused.
func TestManifestZone(t *testing.T) {
	defer meta.SetZone(meta.Zone())
	rome, _ := meta.LoadZone("Europe/Rome")
	ny, _ := meta.LoadZone("America/New_York")
	meta.SetZone(rome)
	d := t.TempDir()
	room := filepath.Join(d, "room")
	os.MkdirAll(room, 0o755)
	os.WriteFile(filepath.Join(room, "a.txt"), []byte("hello"), 0o644)
	out := filepath.Join(d, "m.jsonl")
	if _, err := Build([]string{room}, Options{Out: out, Workers: 1}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(out)
	if !strings.HasPrefix(string(b), `{"onefold":"manifest","zone":"Europe/Rome"}`+"\n") {
		t.Fatalf("header: %q", strings.SplitN(string(b), "\n", 2)[0])
	}
	if recs, err := LoadManifest(out); err != nil || len(recs) != 1 {
		t.Fatalf("same zone: %v %d", err, len(recs))
	}
	meta.SetZone(ny)
	if _, err := LoadManifest(out); err == nil || !strings.Contains(err.Error(), "Europe/Rome") {
		t.Fatalf("other zone must be refused: %v", err)
	}
	if _, err := LoadManifestByPath(out); err == nil {
		t.Fatal("a cache of another zone must not be reused")
	}
	meta.SetZone(rome)
	os.WriteFile(out, []byte(strings.SplitN(string(b), "\n", 2)[1]), 0o644)
	if _, err := LoadManifest(out); err == nil || !strings.Contains(err.Error(), "no onefold manifest header") {
		t.Fatalf("headerless manifest must be refused: %v", err)
	}
	if recs, zone, err := LoadManifestRecords(out); err != nil || zone != "" || len(recs) != 1 {
		t.Fatalf("records without zone: %v %q %d", err, zone, len(recs))
	}
}

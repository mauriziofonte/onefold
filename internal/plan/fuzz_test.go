// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package plan

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mauriziofonte/onefold/internal/meta"
)

// apply creates what a plan names: a plan whose destination leaves the
// target is refused before anything is written.
func TestReadRefusesPathsOutsideTarget(t *testing.T) {
	hdr := `{"onefold":"plan","zone":"` + meta.ZoneName() + `"}` + "\n"
	bad := []string{"../x.jpg", "PHOTOS/../../x.jpg", "/etc/x", "", "."}
	if filepath.Separator == '\\' {
		bad = append(bad, `C:\x.jpg`, `\\host\share\x.jpg`)
	}
	for _, rel := range bad {
		a, _ := json.Marshal(Action{ID: "a", Kind: "text", CanonRel: rel})
		if _, err := read(strings.NewReader(hdr+string(a)+"\n"), "p"); err == nil {
			t.Errorf("canonRel %q accepted", rel)
		}
		a, _ = json.Marshal(Action{ID: "a", Kind: "photo", CanonRel: "PHOTOS/2020/a.jpg", Sidecars: []Sidecar{{Path: "/s/a.xmp", Rel: rel}}})
		if _, err := read(strings.NewReader(hdr+string(a)+"\n"), "p"); err == nil {
			t.Errorf("sidecar rel %q accepted", rel)
		}
	}
	a, _ := json.Marshal(Action{ID: "a", Kind: "photo", CanonRel: "PHOTOS/2020/a..b.jpg", Sidecars: []Sidecar{{Path: "/s/a.xmp", Rel: "PHOTOS/2020/a..b.jpg.xmp"}}})
	if acts, err := read(strings.NewReader(hdr+string(a)+"\n"), "p"); err != nil || len(acts) != 1 {
		t.Fatalf("a name with dots refused: %v", err)
	}
}

// The plan reader never panics and never returns a destination outside the
// target.
func FuzzReadPlan(f *testing.F) {
	hdr := `{"onefold":"plan","zone":"` + meta.ZoneName() + `"}` + "\n"
	a, _ := json.Marshal(Action{ID: "a", Kind: "photo", CanonRel: "PHOTOS/2020/a.jpg", Time: "2020-01-02 03:04:05",
		Drops: []Drop{{Host: "h", Path: "/s/b.jpg"}}, Sidecars: []Sidecar{{Path: "/s/a.xmp", Rel: "PHOTOS/2020/a.jpg.xmp"}}})
	f.Add([]byte(hdr + string(a) + "\n"))
	f.Add([]byte(hdr + `{"id":"x","canonRel":"../x"}` + "\n"))
	f.Add([]byte(`{"onefold":"manifest","zone":"UTC"}` + "\n"))
	f.Fuzz(func(t *testing.T, b []byte) {
		acts, err := read(bytes.NewReader(b), "p")
		if err != nil {
			return
		}
		for _, a := range acts {
			if !filepath.IsLocal(filepath.FromSlash(a.CanonRel)) {
				t.Fatalf("canonRel %q", a.CanonRel)
			}
			for _, s := range a.Sidecars {
				if !filepath.IsLocal(filepath.FromSlash(s.Rel)) {
					t.Fatalf("sidecar rel %q", s.Rel)
				}
			}
		}
	})
}

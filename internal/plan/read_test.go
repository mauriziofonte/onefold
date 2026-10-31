// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package plan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/meta"
)

func TestReadStrict(t *testing.T) {
	d := t.TempDir()
	good, _ := json.Marshal(Action{ID: "a", Kind: "text", CanonRel: "FILES/TXT/2020/a.txt"})
	p := filepath.Join(d, "plan.jsonl")
	planHdr := []byte(`{"onefold":"plan","zone":"` + meta.ZoneName() + `"}` + "\n")
	manHdr := []byte(`{"onefold":"manifest","zone":"` + meta.ZoneName() + `"}` + "\n")
	os.WriteFile(p, append(append(append(planHdr, good...), "\n\n"...), good...), 0o644)
	acts, err := Read(p)
	if err != nil || len(acts) != 2 {
		t.Fatalf("blank lines must be ignored: %v %d", err, len(acts))
	}
	os.WriteFile(p, append(append(append(planHdr, good...), '\n'), `{"id":"trunc`...), 0o644)
	if _, err := Read(p); err == nil || !strings.Contains(err.Error(), "1 malformed line(s), first at line 3") {
		t.Fatalf("a truncated line must fail the read, got %v", err)
	}
	if _, err := catalog.LoadManifest(p); err == nil || !strings.Contains(err.Error(), "not a manifest") {
		t.Fatalf("a plan is not a manifest, got %v", err)
	}
	os.WriteFile(p, append(append(append(manHdr, good...), '\n'), `{"id":"trunc`...), 0o644)
	if _, err := catalog.LoadManifest(p); err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("a truncated manifest line must fail the read, got %v", err)
	}
	// reuse sources (caches, -reuse, an interrupted index) keep every readable line
	rec, _ := json.Marshal(catalog.Record{Path: "/r/a.txt", Size: 3})
	os.WriteFile(p, append(append(append(manHdr, rec...), '\n'), `{"path":"/r/b.t`...), 0o644)
	if m, err := catalog.LoadManifestByPath(p); err != nil || len(m) != 1 || m["/r/a.txt"].Size != 3 {
		t.Fatalf("reuse load must skip the truncated line: %v %v", err, m)
	}
}

func TestArtistOption(t *testing.T) {
	recs := []catalog.Record{
		{Room: "r", Path: "/r/p.jpg", Size: 10, Hash: "P", Host: "h", Meta: meta.Meta{Kind: "photo", Time: "2020-01-02 03:04:05"}},
		{Room: "r", Path: "/r/d.txt", Size: 10, Hash: "D", Host: "h", Meta: meta.Meta{Kind: "text", Time: "2020-01-02 03:04:05"}},
	}
	for _, c := range []struct{ opt, want string }{{"", ""}, {"Jane Doe", "Jane Doe"}} {
		acts := BuildActionsWith(PlanWith(recs, Options{Artist: c.opt}), Options{Artist: c.opt})
		if a := onlyAction(t, acts, "P"); a.Artist != c.want {
			t.Errorf("artist %q: photo got %q want %q", c.opt, a.Artist, c.want)
		}
		if a := onlyAction(t, acts, "D"); a.Artist != "" {
			t.Errorf("artist %q: generic kinds are never stamped, got %q", c.opt, a.Artist)
		}
	}
}

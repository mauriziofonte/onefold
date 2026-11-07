// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package planreport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/htmlreport"
	"github.com/mauriziofonte/onefold/internal/meta"
	"github.com/mauriziofonte/onefold/internal/plan"
)

func rec(p, hash, kind string, m meta.Meta) catalog.Record {
	m.Kind = kind
	if m.Time == "" {
		m.Time = "2019-01-01 00:00:00"
	}
	return catalog.Record{Room: "/r", Path: p, Size: 100, Hash: hash, Host: "h", Meta: m}
}

func TestReport(t *testing.T) {
	recs := []catalog.Record{
		rec("/r/a.txt", "A", "text", meta.Meta{FileCategory: "text"}),
		rec("/r/b/a.txt", "A", "text", meta.Meta{FileCategory: "text"}),
		rec("/r/v1.docx", "D1", "document", meta.Meta{Title: "Budget", Author: "Ann", Pages: 2}),
		rec("/r/v2.docx", "D2", "document", meta.Meta{Title: "budget ", Author: "ann", Pages: 2}),
		rec("/r/v3.docx", "D3", "document", meta.Meta{Title: "Budget", Author: "Ann", Pages: 3}),
		rec("/r/fake.jpg", "F", "photo", meta.Meta{ExtMismatch: true, SniffExt: "png", Prefix: "IMG"}),
	}
	o := plan.Options{}
	r := plan.PlanWith(recs, o)
	acts := plan.BuildActionsWith(r, o)
	rep := Build([]string{"m.jsonl"}, recs, r, acts, o, true)
	if rep.Totals.InputFiles != 6 || rep.Totals.Golden != 5 || rep.Totals.ExactDupes != 1 {
		t.Fatalf("totals %+v", rep.Totals)
	}
	if rep.ByKind["text"].Files != 2 || rep.ByKind["text"].Golden != 1 || rep.ByKind["text"].Dropped != 1 {
		t.Fatalf("byKind %+v", rep.ByKind)
	}
	if len(rep.DupGroups) != 1 || len(rep.DupGroups[0].Drops) != 1 {
		t.Fatalf("dup groups %+v", rep.DupGroups)
	}
	if len(rep.NearDupDocs) != 1 || len(rep.NearDupDocs[0].Members) != 2 {
		t.Fatalf("near dups %+v", rep.NearDupDocs)
	}
	if len(rep.Mismatches) != 1 || rep.Mismatches[0].SniffExt != "png" {
		t.Fatalf("mismatches %+v", rep.Mismatches)
	}
	if len(rep.Files) != 5 {
		t.Fatalf("extensive files %d", len(rep.Files))
	}
	if Build(nil, recs, r, acts, o, false).Files != nil {
		t.Fatal("file listing must be extensive-only")
	}

	d := t.TempDir()
	jp, mp, hp := filepath.Join(d, "r.json"), filepath.Join(d, "r.md"), filepath.Join(d, "r.HTML")
	if err := Write(rep, jp); err != nil {
		t.Fatal(err)
	}
	if err := Write(rep, mp); err != nil {
		t.Fatal(err)
	}
	if err := Write(rep, hp); err != nil {
		t.Fatal(err)
	}
	var back Report
	b, _ := os.ReadFile(jp)
	if err := json.Unmarshal(b, &back); err != nil || back.Totals.Golden != 5 {
		t.Fatalf("json roundtrip: %v", err)
	}
	md, _ := os.ReadFile(mp)
	for _, want := range []string{"# onefold plan report", "## By kind", "Budget", "fake.jpg", "## Golden files"} {
		if !strings.Contains(string(md), want) {
			t.Fatalf("markdown missing %q", want)
		}
	}
	h, _ := os.ReadFile(hp)
	if err := htmlreport.Check(string(h)); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<title>onefold plan report</title>", "By kind", "Budget", "fake.jpg", "Golden files"} {
		if !strings.Contains(string(h), want) {
			t.Fatalf("html missing %q", want)
		}
	}
}

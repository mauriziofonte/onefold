// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package planreport renders the `plan --report` artifact: aggregate stats,
// duplicate groups, document near-duplicates and extension mismatches, as JSON,
// Markdown or one self-contained HTML file with charts. Read-only: computed
// from manifests + plan result.
package planreport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/kinds"
	"github.com/mauriziofonte/onefold/internal/plan"
	"github.com/mauriziofonte/onefold/internal/report"
)

// Cap on list sections when not --extensive.
const topN = 50

type Bucket struct {
	Files int   `json:"files"`
	Bytes int64 `json:"bytes"`
}

type KindStats struct {
	Files   int   `json:"files"`
	Bytes   int64 `json:"bytes"`
	Golden  int   `json:"golden"`
	Dropped int   `json:"dropped"`
}

type Member struct {
	Host   string `json:"host"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Reason string `json:"reason,omitempty"`
}

type DupGroup struct {
	Kind    string   `json:"kind"`
	Winner  Member   `json:"winner"`
	Target  string   `json:"target"`
	Drops   []Member `json:"drops"`
	Reclaim int64    `json:"reclaim"`
}

type NearDup struct {
	Title   string   `json:"title"`
	Author  string   `json:"author"`
	Pages   int      `json:"pages"`
	Members []Member `json:"members"` // distinct golden files (different bytes), never auto-dropped
}

type Mismatch struct {
	Path     string `json:"path"`
	Ext      string `json:"ext"`
	SniffExt string `json:"sniffExt,omitempty"` // "" = content family known but ambiguous
}

type GoldenFile struct {
	Kind   string `json:"kind"`
	Src    string `json:"src"`
	Target string `json:"target"`
}

type Totals struct {
	InputFiles       int   `json:"inputFiles"`
	Golden           int   `json:"golden"`
	ExactDupes       int   `json:"exactDupes"`
	LowerRes         int   `json:"lowerRes"`
	SmallerVariants  int   `json:"smallerVariants"`
	GoldenBytes      int64 `json:"goldenBytes"`
	ReclaimBytes     int64 `json:"reclaimBytes"`
	VerifiedGroups   int   `json:"verifiedGroups"`
	VerifySplits     int   `json:"verifySplits"`
	VerifyUnreadable int   `json:"verifyUnreadable"`
}

type Report struct {
	Manifests    []string             `json:"manifests"`
	Options      plan.Options         `json:"options"`
	Extensive    bool                 `json:"extensive"`
	Totals       Totals               `json:"totals"`
	ByKind       map[string]KindStats `json:"byKind"`
	ByCategory   map[string]Bucket    `json:"byCategory"`
	ByExt        map[string]Bucket    `json:"byExt"`
	ByYear       map[string]Bucket    `json:"byYear"`
	DupGroups    []DupGroup           `json:"dupGroups"`
	DupGroupsAll int                  `json:"dupGroupsTotal"`
	NearDupDocs  []NearDup            `json:"nearDupDocs"`
	Mismatches   []Mismatch           `json:"extMismatches"`
	MismatchAll  int                  `json:"extMismatchesTotal"`
	Files        []GoldenFile         `json:"files,omitempty"` // --extensive only
}

// Build computes the report.
func Build(manifests []string, recs []catalog.Record, r *plan.Result, acts []plan.Action, o plan.Options, extensive bool) *Report {
	rep := &Report{
		Manifests: manifests, Options: o, Extensive: extensive,
		Totals: Totals{InputFiles: r.InputFiles, Golden: r.Assets, ExactDupes: r.ExactDupes, LowerRes: r.LowerRes,
			SmallerVariants: r.SmallerVar, GoldenBytes: r.GoldenBytes, ReclaimBytes: r.ReclaimBytes,
			VerifiedGroups: r.VerifiedGroups, VerifySplits: r.VerifySplits, VerifyUnreadable: r.VerifyUnreadable},
		ByKind: map[string]KindStats{}, ByCategory: map[string]Bucket{}, ByExt: map[string]Bucket{}, ByYear: map[string]Bucket{},
	}
	add := func(m map[string]Bucket, k string, size int64) {
		b := m[k]
		b.Files++
		b.Bytes += size
		m[k] = b
	}
	for _, rec := range recs {
		k := rec.Meta.Kind
		ks := rep.ByKind[k]
		ks.Files++
		ks.Bytes += rec.Size
		rep.ByKind[k] = ks
		add(rep.ByCategory, orNone(rec.Meta.FileCategory), rec.Size)
		add(rep.ByExt, orNone(kinds.Ext(rec.Path)), rec.Size)
		y := "(none)"
		if len(rec.Meta.Time) >= 4 {
			y = rec.Meta.Time[:4]
		}
		add(rep.ByYear, y, rec.Size)
		if rec.Meta.ExtMismatch {
			rep.Mismatches = append(rep.Mismatches, Mismatch{Path: rec.Path, Ext: kinds.Ext(rec.Path), SniffExt: rec.Meta.SniffExt})
		}
	}
	for _, a := range acts {
		ks := rep.ByKind[a.Kind]
		ks.Golden++
		ks.Dropped += len(a.Drops)
		rep.ByKind[a.Kind] = ks
		if len(a.Drops) > 0 {
			g := DupGroup{Kind: a.Kind, Winner: Member{Host: a.SrcHost, Path: a.SrcPath, Size: a.SrcSize}, Target: a.CanonRel}
			for _, d := range a.Drops {
				g.Drops = append(g.Drops, Member{Host: d.Host, Path: d.Path, Size: d.Size, Reason: d.Reason})
				g.Reclaim += d.Size
			}
			rep.DupGroups = append(rep.DupGroups, g)
		}
		if extensive {
			rep.Files = append(rep.Files, GoldenFile{Kind: a.Kind, Src: a.SrcHost + ":" + a.SrcPath, Target: a.CanonRel})
		}
	}
	sort.Slice(rep.DupGroups, func(i, j int) bool {
		if rep.DupGroups[i].Reclaim != rep.DupGroups[j].Reclaim {
			return rep.DupGroups[i].Reclaim > rep.DupGroups[j].Reclaim
		}
		return rep.DupGroups[i].Winner.Path < rep.DupGroups[j].Winner.Path
	})
	sort.Slice(rep.Mismatches, func(i, j int) bool { return rep.Mismatches[i].Path < rep.Mismatches[j].Path })
	sort.Slice(rep.Files, func(i, j int) bool { return rep.Files[i].Target < rep.Files[j].Target })
	rep.DupGroupsAll, rep.MismatchAll = len(rep.DupGroups), len(rep.Mismatches)
	rep.NearDupDocs = nearDups(r)
	if !extensive {
		if len(rep.DupGroups) > topN {
			rep.DupGroups = rep.DupGroups[:topN]
		}
		if len(rep.Mismatches) > topN {
			rep.Mismatches = rep.Mismatches[:topN]
		}
	}
	return rep
}

// nearDups groups DISTINCT golden documents sharing title+author+pages: likely
// re-exports/re-saves of the same document. Report-only, never dropped.
func nearDups(r *plan.Result) []NearDup {
	groups := map[string]*NearDup{}
	var keys []string
	winners := make([]catalog.Record, 0, len(r.Golden))
	for _, a := range r.Golden {
		winners = append(winners, a.Winner)
	}
	sort.Slice(winners, func(i, j int) bool { return winners[i].Path < winners[j].Path })
	for _, w := range winners {
		m := w.Meta
		if m.Kind != "document" || strings.TrimSpace(m.Title) == "" {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(m.Title)) + "\x00" + strings.ToLower(strings.TrimSpace(m.Author)) + "\x00" + fmt.Sprint(m.Pages)
		g, ok := groups[k]
		if !ok {
			g = &NearDup{Title: m.Title, Author: m.Author, Pages: m.Pages}
			groups[k] = g
			keys = append(keys, k)
		}
		g.Members = append(g.Members, Member{Host: w.Host, Path: w.Path, Size: w.Size})
	}
	sort.Strings(keys)
	var out []NearDup
	for _, k := range keys {
		if g := groups[k]; len(g.Members) > 1 {
			out = append(out, *g)
		}
	}
	return out
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// Write renders the report to path: .md -> Markdown, .html or .htm -> HTML
// with charts, anything else -> JSON.
func Write(rep *Report, path string) error {
	var b []byte
	switch e := strings.ToLower(filepath.Ext(path)); {
	case e == ".md":
		b = []byte(Markdown(rep))
	case e == ".html" || e == ".htm":
		var sb strings.Builder
		if err := HTML(&sb, rep); err != nil {
			return err
		}
		b = []byte(sb.String())
	default:
		var err error
		if b, err = json.MarshalIndent(rep, "", "  "); err != nil {
			return err
		}
	}
	tmp := path + ".part"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Markdown renders the report as tables.
func Markdown(rep *Report) string {
	var sb strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&sb, f, a...) }
	t := rep.Totals
	w("# onefold plan report\n\n")
	w("Manifests: %s\n\n", strings.Join(rep.Manifests, ", "))
	w("| Metric | Value |\n|---|---|\n")
	w("| input files | %d |\n| golden | %d (%s) |\n| exact dupes | %d |\n| lower-res | %d |\n| smaller variants | %d |\n| reclaimable | %s |\n",
		t.InputFiles, t.Golden, report.HumanBytes(t.GoldenBytes), t.ExactDupes, t.LowerRes, t.SmallerVariants, report.HumanBytes(t.ReclaimBytes))
	if t.VerifiedGroups > 0 {
		w("| verify-full groups / splits / unreadable | %d / %d / %d |\n", t.VerifiedGroups, t.VerifySplits, t.VerifyUnreadable)
	}
	w("\n## By kind\n\n| Kind | Files | Size | Golden | Dropped |\n|---|---|---|---|---|\n")
	for _, k := range sortedKeys(rep.ByKind) {
		s := rep.ByKind[k]
		w("| %s | %d | %s | %d | %d |\n", k, s.Files, report.HumanBytes(s.Bytes), s.Golden, s.Dropped)
	}
	bucketTable := func(title, col string, m map[string]Bucket, byCount bool) {
		w("\n## %s\n\n| %s | Files | Size |\n|---|---|---|\n", title, col)
		keys := sortedKeys(m)
		if byCount {
			sort.SliceStable(keys, func(i, j int) bool { return m[keys[i]].Files > m[keys[j]].Files })
			if !rep.Extensive && len(keys) > topN {
				keys = keys[:topN]
			}
		}
		for _, k := range keys {
			w("| %s | %d | %s |\n", mdEsc(k), m[k].Files, report.HumanBytes(m[k].Bytes))
		}
	}
	bucketTable("By category", "Category", rep.ByCategory, true)
	bucketTable("By extension", "Ext", rep.ByExt, true)
	bucketTable("By year", "Year", rep.ByYear, false)
	w("\n## Duplicate groups (%d of %d, by reclaimable size)\n\n", len(rep.DupGroups), rep.DupGroupsAll)
	for _, g := range rep.DupGroups {
		w("- KEEP `%s:%s` -> `%s` (%s)\n", g.Winner.Host, mdEsc(g.Winner.Path), mdEsc(g.Target), g.Kind)
		for _, d := range g.Drops {
			w("  - drop `%s:%s` [%s] %s\n", d.Host, mdEsc(d.Path), d.Reason, report.HumanBytes(d.Size))
		}
	}
	w("\n## Document near-duplicates (report only, never dropped)\n\n")
	if len(rep.NearDupDocs) == 0 {
		w("(none)\n")
	}
	for _, n := range rep.NearDupDocs {
		w("- \"%s\" / %s / %d pages\n", mdEsc(n.Title), mdEsc(orNone(n.Author)), n.Pages)
		for _, m := range n.Members {
			w("  - `%s:%s` %s\n", m.Host, mdEsc(m.Path), report.HumanBytes(m.Size))
		}
	}
	w("\n## Extension mismatches (%d of %d)\n\n| Path | Ext | Content |\n|---|---|---|\n", len(rep.Mismatches), rep.MismatchAll)
	for _, m := range rep.Mismatches {
		w("| %s | %s | %s |\n", mdEsc(m.Path), m.Ext, orNone(m.SniffExt))
	}
	if len(rep.Files) > 0 {
		w("\n## Golden files\n\n| Kind | Source | Target |\n|---|---|---|\n")
		for _, f := range rep.Files {
			w("| %s | %s | %s |\n", f.Kind, mdEsc(f.Src), mdEsc(f.Target))
		}
	}
	return sb.String()
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func mdEsc(s string) string { return strings.ReplaceAll(s, "|", "\\|") }

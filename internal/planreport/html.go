// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package planreport

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/mauriziofonte/onefold/internal/htmlreport"
	"github.com/mauriziofonte/onefold/internal/report"
)

// HTML writes rep as one self-contained HTML file (htmlreport): the totals,
// the breakdowns as charts with their tables, then the lists of the
// Markdown report.
func HTML(w io.Writer, rep *Report) error {
	hb := report.HumanBytes
	t := rep.Totals
	d := htmlreport.New("onefold plan report", "Manifests: "+strings.Join(rep.Manifests, ", "))
	figs := []htmlreport.Figure{
		{Label: "Input files", Value: strconv.Itoa(t.InputFiles)},
		{Label: "Golden", Value: strconv.Itoa(t.Golden), Note: hb(t.GoldenBytes)},
		{Label: "Exact duplicates", Value: strconv.Itoa(t.ExactDupes)},
		{Label: "Lower resolution", Value: strconv.Itoa(t.LowerRes)},
		{Label: "Smaller variants", Value: strconv.Itoa(t.SmallerVariants)},
		{Label: "Reclaimable", Value: hb(t.ReclaimBytes)},
	}
	if t.VerifiedGroups > 0 {
		figs = append(figs, htmlreport.Figure{Label: "Verified groups", Value: strconv.Itoa(t.VerifiedGroups),
			Note: fmt.Sprintf("%d split, %d unreadable", t.VerifySplits, t.VerifyUnreadable)})
	}
	d.Figures(figs)

	d.Section("Breakdown")
	var kinds []htmlreport.Bar
	for _, k := range sortedKeys(rep.ByKind) {
		s := rep.ByKind[k]
		kinds = append(kinds, htmlreport.Bar{Label: k, Value: s.Bytes, Text: hb(s.Bytes),
			Extra: []string{strconv.Itoa(s.Files), strconv.Itoa(s.Golden), strconv.Itoa(s.Dropped)}})
	}
	sort.SliceStable(kinds, func(i, j int) bool { return kinds[i].Value > kinds[j].Value })
	d.Bars("By kind (size)", []string{"Kind", "Size", "Files", "Golden", "Dropped"}, kinds)
	bars := func(title, col string, m map[string]Bucket, bySize bool) {
		keys := sortedKeys(m)
		if bySize {
			sort.SliceStable(keys, func(i, j int) bool { return m[keys[i]].Bytes > m[keys[j]].Bytes })
			if !rep.Extensive && len(keys) > topN {
				keys = keys[:topN]
			}
		}
		var rows []htmlreport.Bar
		for _, k := range keys {
			rows = append(rows, htmlreport.Bar{Label: k, Value: m[k].Bytes, Text: hb(m[k].Bytes), Extra: []string{strconv.Itoa(m[k].Files)}})
		}
		d.Bars(title, []string{col, "Size", "Files"}, rows)
	}
	bars("By category (size)", "Category", rep.ByCategory, true)
	bars("By extension (size)", "Extension", rep.ByExt, true)
	bars("By year (size)", "Year", rep.ByYear, false)

	d.Section(fmt.Sprintf("Duplicate groups (%d of %d, by reclaimable size)", len(rep.DupGroups), rep.DupGroupsAll))
	var groups [][]string
	for _, g := range rep.DupGroups {
		var drops []string
		for _, m := range g.Drops {
			drops = append(drops, fmt.Sprintf("%s:%s [%s] %s", m.Host, m.Path, m.Reason, hb(m.Size)))
		}
		groups = append(groups, []string{g.Winner.Host + ":" + g.Winner.Path, g.Target, g.Kind, strings.Join(drops, " | "), hb(g.Reclaim)})
	}
	d.Table("", []string{"Kept", "Golden name", "Kind", "Dropped", "Reclaim"}, groups, 4)

	d.Section("Document near-duplicates (listed, never dropped)")
	var near [][]string
	for _, n := range rep.NearDupDocs {
		var ms []string
		for _, m := range n.Members {
			ms = append(ms, fmt.Sprintf("%s:%s %s", m.Host, m.Path, hb(m.Size)))
		}
		near = append(near, []string{n.Title, orNone(n.Author), strconv.Itoa(n.Pages), strings.Join(ms, " | ")})
	}
	d.Table("", []string{"Title", "Author", "Pages", "Files"}, near, 0)

	d.Section(fmt.Sprintf("Extension mismatches (%d of %d)", len(rep.Mismatches), rep.MismatchAll))
	var mm [][]string
	for _, m := range rep.Mismatches {
		mm = append(mm, []string{m.Path, m.Ext, orNone(m.SniffExt)})
	}
	d.Table("", []string{"Path", "Extension", "Content"}, mm, 0)
	if len(rep.Files) > 0 {
		d.Section("Golden files")
		var fs [][]string
		for _, f := range rep.Files {
			fs = append(fs, []string{f.Kind, f.Src, f.Target})
		}
		d.Table("", []string{"Kind", "Source", "Golden name"}, fs, 0)
	}
	_, err := d.WriteTo(w)
	return err
}

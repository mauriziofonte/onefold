// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package stats

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/mauriziofonte/onefold/internal/htmlreport"
	"github.com/mauriziofonte/onefold/internal/report"
)

// FamilyColors are the chart colors of the file families, the GUI's.
var FamilyColors = map[string]string{
	"photos": "#4e79a7", "videos": "#e15759", "audio": "#b07aa1", "documents": "#f28e2b", "archives": "#9c755f",
	"code": "#59a14f", "apps": "#76b7b2", "design": "#edc948", "games": "#ff9da7", "data": "#86bcb6", "other": "#bab0ac",
}

func count(n int64) string {
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 && s[i-1] != '-' {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

func pct(part, whole int64) string {
	if whole <= 0 {
		return ""
	}
	return fmt.Sprintf("%.1f%%", 100*float64(part)/float64(whole))
}

// HTML writes r as one self-contained HTML file (htmlreport): the figures,
// the breakdowns as charts with their tables, the heaviest folders, the
// largest files and the duplicates. top rows per list.
func HTML(w io.Writer, r *Result, top int) error {
	top = max(top, 1)
	hb := report.HumanBytes
	d := htmlreport.New("onefold statistics", fmt.Sprintf("%s, measured %s in %s", strings.Join(r.Roots, ", "),
		r.Started.Format("2006-01-02 15:04"), (time.Duration(r.Elapsed*float64(time.Second))).Round(time.Second)))
	figs := []htmlreport.Figure{{Label: "Files", Value: count(r.Files)}, {Label: "Folders", Value: count(r.Dirs)}, {Label: "Size", Value: hb(r.Bytes)}}
	if r.Allocated > 0 {
		figs = append(figs, htmlreport.Figure{Label: "On disk", Value: hb(r.Allocated)})
	}
	figs = append(figs, htmlreport.Figure{Label: "Empty files", Value: count(r.Empty)},
		htmlreport.Figure{Label: "OS droppings", Value: count(r.Junk.Files), Note: hb(r.Junk.Bytes)},
		htmlreport.Figure{Label: "Unreadable", Value: count(r.Errors)})
	if r.OnlineOnly.Files > 0 {
		figs = append(figs, htmlreport.Figure{Label: "Only in the cloud", Value: count(r.OnlineOnly.Files), Note: hb(r.OnlineOnly.Bytes) + ", not on this disk"})
	}
	if r.HardLinks.Files > 0 {
		figs = append(figs, htmlreport.Figure{Label: "Extra hard links", Value: count(r.HardLinks.Files), Note: "counted once"})
	}
	if r.Dups != nil {
		figs = append(figs, htmlreport.Figure{Label: "Wasted by duplicates", Value: hb(r.Dups.Wasted), Note: pct(r.Dups.Wasted, r.Bytes) + " of the size"})
	}
	if r.Stopped || r.Partial {
		d.Para("This measurement was stopped before the end: the numbers cover what was read.")
	}
	d.Figures(figs)
	if len(r.Volumes) > 0 {
		var rows [][]string
		for _, v := range r.Volumes {
			rows = append(rows, []string{v.Root, v.Mount, v.FSType, hb(v.Total), hb(v.Free)})
		}
		d.Table("Volumes", []string{"Folder", "Volume", "File system", "Size", "Free"}, rows, 3)
	}

	d.Section("What fills it")
	var fam []htmlreport.Bar
	for _, b := range r.Families {
		if b.Files > 0 {
			fam = append(fam, htmlreport.Bar{Label: b.Key, Value: b.Bytes, Text: hb(b.Bytes), Color: FamilyColors[b.Key],
				Extra: []string{pct(b.Bytes, r.Bytes), count(b.Files)}})
		}
	}
	d.Bars("By family (size)", []string{"Family", "Size", "Share", "Files"}, fam)
	d.Bars("By category (size)", []string{"Category", "Size", "Files"}, buckets(r.Categories, top, func(b Bucket) []string { return []string{count(b.Files)} }))
	d.Bars("By extension (size)", []string{"Extension", "Size", "Family", "Files"}, buckets(r.Exts, top, func(b Bucket) []string { return []string{b.Family, count(b.Files)} }))
	if len(r.Unknown) > 0 {
		var rows [][]string
		for i, b := range r.Unknown {
			if i >= top {
				break
			}
			name := "." + b.Key
			if b.Key == "" {
				name = "(no extension)"
			}
			rows = append(rows, []string{name, hb(b.Bytes), count(b.Files)})
		}
		d.Table("Extensions not in the type registry", []string{"Extension", "Size", "Files"}, rows, 1)
	}
	var years []htmlreport.Bar
	for _, y := range r.Years {
		label := strconv.Itoa(y.Year)
		if y.Year == 0 {
			label = "?"
		}
		years = append(years, htmlreport.Bar{Label: label, Value: y.Bytes, Text: hb(y.Bytes), Extra: []string{count(y.Files)}})
	}
	d.Bars("By year of last change (size)", []string{"Year", "Size", "Files"}, years)
	var sizes []htmlreport.Bar
	for _, s := range r.Sizes {
		sizes = append(sizes, htmlreport.Bar{Label: s.Label, Value: s.Files, Text: count(s.Files), Extra: []string{hb(s.Bytes)}})
	}
	d.Bars("By file size (files)", []string{"Size band", "Files", "Size"}, sizes)

	d.Section("Where")
	var folders [][]string
	for _, t := range r.Tree {
		for i, c := range t.Children {
			if i >= top {
				break
			}
			folders = append(folders, []string{c.Path, hb(c.Bytes), pct(c.Bytes, r.Bytes), count(c.Files)})
		}
	}
	d.Table("Heaviest folders", []string{"Folder", "Size", "Share", "Files"}, folders, 1)
	var largest [][]string
	for i, f := range r.Largest {
		if i >= top {
			break
		}
		largest = append(largest, []string{f.Path, f.Family, hb(f.Size), time.Unix(f.MTime, 0).Format("2006-01-02")})
	}
	d.Table("Largest files", []string{"File", "Family", "Size", "Changed"}, largest, 2)

	d.Section("Duplicates")
	dp := r.Dups
	if dp == nil {
		d.Para("Not measured (-dups off).")
	} else {
		how := "Same size and same sampled content; -dups full confirms them on the whole content."
		if dp.Mode == DupsFull {
			how = "Confirmed on the whole content."
		}
		d.Para(how)
		d.Figures([]htmlreport.Figure{{Label: "Groups", Value: count(dp.Groups)}, {Label: "Extra copies", Value: count(dp.Extra)},
			{Label: "Wasted", Value: hb(dp.Wasted)}, {Label: "Unreadable", Value: count(dp.Errors)}})
		var bf []htmlreport.Bar
		for _, b := range dp.ByFamily {
			bf = append(bf, htmlreport.Bar{Label: b.Key, Value: b.Wasted, Text: hb(b.Wasted), Color: FamilyColors[b.Key], Extra: []string{count(b.Files)}})
		}
		d.Bars("Wasted by family", []string{"Family", "Wasted", "Files in groups"}, bf)
		var pairs [][]string
		for i, p := range dp.Pairs {
			if i >= top {
				break
			}
			b := p.B
			if p.A == p.B {
				b = "(copies inside the same folder)"
			}
			pairs = append(pairs, []string{p.A, b, count(p.Files), hb(p.Bytes)})
		}
		d.Table("Folders holding the same files", []string{"Folder", "and", "Files", "Size"}, pairs, 2)
		var groups [][]string
		for i, g := range dp.Top {
			if i >= top {
				break
			}
			paths := g.Paths
			more := ""
			if len(paths) > 4 {
				paths, more = paths[:4], fmt.Sprintf(" … %d more", g.Count-4)
			}
			groups = append(groups, []string{strings.Join(paths, " | ") + more, fmt.Sprintf("%d × %s", g.Count, hb(g.Size)), hb(g.Wasted)})
		}
		d.Table("Largest groups", []string{"Copies", "Count × size", "Wasted"}, groups, 1)
	}
	if len(r.Problems) > 0 {
		var rows [][]string
		for i, p := range r.Problems {
			if i >= top {
				break
			}
			rows = append(rows, []string{p.Path, p.Err})
		}
		d.Table("Could not be read", []string{"Path", "Error"}, rows, 0)
	}
	_, err := d.WriteTo(w)
	return err
}

func buckets(bs []Bucket, top int, extra func(Bucket) []string) []htmlreport.Bar {
	var out []htmlreport.Bar
	for i, b := range bs {
		if i >= top {
			break
		}
		out = append(out, htmlreport.Bar{Label: b.Key, Value: b.Bytes, Text: report.HumanBytes(b.Bytes), Color: FamilyColors[b.Family], Extra: extra(b)})
	}
	return out
}

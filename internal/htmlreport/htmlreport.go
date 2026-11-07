// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package htmlreport writes a report as one self-contained HTML file: inline
// CSS (light and dark, from the system setting), SVG bar charts each followed
// by its numbers as a table, no script, and a Content-Security-Policy that
// forbids every request: the file opens offline and sends nothing anywhere.
// `stats -html` and `plan -report x.html` use it.
package htmlreport

import (
	"fmt"
	"html"
	"io"
	"strings"
)

// Doc is a report being built.
type Doc struct {
	title string
	b     strings.Builder
}

// New starts a report with its title and a line under it.
func New(title, subtitle string) *Doc {
	d := &Doc{title: title}
	fmt.Fprintf(&d.b, "<header><h1>%s</h1><p class=\"sub\">%s</p></header>\n", esc(title), esc(subtitle))
	return d
}

func esc(s string) string { return html.EscapeString(s) }

// Section starts a titled section.
func (d *Doc) Section(title string) {
	fmt.Fprintf(&d.b, "<h2>%s</h2>\n", esc(title))
}

// Para adds a paragraph of plain text.
func (d *Doc) Para(text string) {
	fmt.Fprintf(&d.b, "<p>%s</p>\n", esc(text))
}

// Figure is one key number.
type Figure struct {
	Label, Value string
	Note         string // smaller text under the value ("" = none)
}

// Figures adds a row of key numbers.
func (d *Doc) Figures(fs []Figure) {
	d.b.WriteString("<div class=\"figs\">")
	for _, f := range fs {
		fmt.Fprintf(&d.b, "<div class=\"fig\"><div class=\"fl\">%s</div><div class=\"fv\">%s</div>", esc(f.Label), esc(f.Value))
		if f.Note != "" {
			fmt.Fprintf(&d.b, "<div class=\"fn\">%s</div>", esc(f.Note))
		}
		d.b.WriteString("</div>")
	}
	d.b.WriteString("</div>\n")
}

// Bar is one row of a bar chart.
type Bar struct {
	Label string
	Value int64
	Text  string // the value as shown ("" = the number)
	Color string // a CSS color ("" = the accent)
	Extra []string
}

// Bars adds a horizontal bar chart of rows, and under it the same rows as a
// table with head (the label column, the value column, then one column per
// Extra). An empty rows list writes "(none)".
func (d *Doc) Bars(title string, head []string, rows []Bar) {
	fmt.Fprintf(&d.b, "<h3>%s</h3>\n", esc(title))
	if len(rows) == 0 {
		d.b.WriteString("<p class=\"none\">(none)</p>\n")
		return
	}
	var peak int64
	for _, r := range rows {
		peak = max(peak, r.Value)
	}
	const rowH, labelW, chartW, valueW = 22, 190, 420, 110
	h := rowH*len(rows) + 4
	var alt []string
	for i, r := range rows {
		if i == 3 {
			alt = append(alt, "...")
			break
		}
		alt = append(alt, r.Label+" "+text(r))
	}
	fmt.Fprintf(&d.b, "<svg class=\"chart\" viewBox=\"0 0 %d %d\" width=\"%d\" height=\"%d\" role=\"img\" aria-label=\"%s: %s\">\n",
		labelW+chartW+valueW, h, labelW+chartW+valueW, h, esc(title), esc(strings.Join(alt, ", ")))
	for i, r := range rows {
		y := 2 + i*rowH
		w := 0.0
		if peak > 0 {
			w = float64(chartW-8) * float64(r.Value) / float64(peak)
		}
		if r.Value > 0 && w < 1 {
			w = 1
		}
		fill := r.Color
		if fill == "" {
			fill = "var(--accent)"
		}
		fmt.Fprintf(&d.b, "<text x=\"%d\" y=\"%d\" text-anchor=\"end\">%s</text>", labelW-8, y+15, esc(clip(r.Label, 28)))
		fmt.Fprintf(&d.b, "<rect x=\"%d\" y=\"%d\" width=\"%.1f\" height=\"%d\" rx=\"3\" style=\"fill:%s\"/>", labelW, y+3, w, rowH-6, esc(fill))
		fmt.Fprintf(&d.b, "<text x=\"%.1f\" y=\"%d\" class=\"v\">%s</text>\n", float64(labelW)+w+6, y+15, esc(text(r)))
	}
	d.b.WriteString("</svg>\n")
	var trs [][]string
	for _, r := range rows {
		trs = append(trs, append([]string{r.Label, text(r)}, r.Extra...))
	}
	d.b.WriteString("<details><summary>Table</summary>\n")
	d.table(head, trs, 1)
	d.b.WriteString("</details>\n")
}

func text(r Bar) string {
	if r.Text != "" {
		return r.Text
	}
	return fmt.Sprint(r.Value)
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// Table adds a table; the columns from num on are right-aligned (numbers).
func (d *Doc) Table(title string, head []string, rows [][]string, num int) {
	if title != "" {
		fmt.Fprintf(&d.b, "<h3>%s</h3>\n", esc(title))
	}
	if len(rows) == 0 {
		d.b.WriteString("<p class=\"none\">(none)</p>\n")
		return
	}
	d.table(head, rows, num)
}

func (d *Doc) table(head []string, rows [][]string, num int) {
	d.b.WriteString("<div class=\"tw\"><table><thead><tr>")
	for i, h := range head {
		fmt.Fprintf(&d.b, "<th scope=\"col\"%s>%s</th>", alignAt(i, num), esc(h))
	}
	d.b.WriteString("</tr></thead><tbody>\n")
	for _, r := range rows {
		d.b.WriteString("<tr>")
		for i, c := range r {
			fmt.Fprintf(&d.b, "<td%s>%s</td>", alignAt(i, num), esc(c))
		}
		d.b.WriteString("</tr>\n")
	}
	d.b.WriteString("</tbody></table></div>\n")
}

func alignAt(i, num int) string {
	if num > 0 && i >= num {
		return " class=\"n\""
	}
	return ""
}

// WriteTo writes the whole HTML file.
func (d *Doc) WriteTo(w io.Writer) (int64, error) {
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"en\"><head><meta charset=\"utf-8\">\n")
	b.WriteString("<meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; style-src 'unsafe-inline'\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	b.WriteString("<meta name=\"color-scheme\" content=\"light dark\">\n")
	fmt.Fprintf(&b, "<title>%s</title>\n<style>%s</style></head><body><main>\n", esc(d.title), css)
	b.WriteString(d.b.String())
	b.WriteString("<footer>Written by onefold. This file makes no network request.</footer>\n</main></body></html>\n")
	n, err := io.WriteString(w, b.String())
	return int64(n), err
}

// css: the GUI's brand teal and slate grays; dark from the system setting.
const css = `
:root{--bg:#f8fafc;--card:#fff;--fg:#0f172a;--muted:#64748b;--line:#e2e8f0;--accent:#0f766e;--accent-soft:#d7eeea}
@media (prefers-color-scheme:dark){:root{--bg:#0b1220;--card:#111a2b;--fg:#e2e8f0;--muted:#94a3b8;--line:#1e293b;--accent:#46a094;--accent-soft:#0b4d48}}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--fg);font:14px/1.45 system-ui,-apple-system,"Segoe UI",Roboto,sans-serif}
main{max-width:1060px;margin:0 auto;padding:24px 16px 48px}
h1{font-size:24px;margin:0 0 4px}h2{font-size:18px;margin:32px 0 8px;padding-bottom:4px;border-bottom:1px solid var(--line)}h3{font-size:15px;margin:20px 0 6px}
.sub,.none,footer{color:var(--muted)}footer{margin-top:40px;font-size:12px}
.figs{display:grid;grid-template-columns:repeat(auto-fill,minmax(160px,1fr));gap:10px}
.fig{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:10px 12px}
.fl{color:var(--muted);font-size:12px}.fv{font-size:20px;font-weight:600}.fn{color:var(--muted);font-size:12px}
svg.chart{display:block;max-width:100%;height:auto;background:var(--card);border:1px solid var(--line);border-radius:10px;padding:6px 0}
svg.chart text{fill:var(--fg);font-size:12px}svg.chart text.v{fill:var(--muted)}
details{margin:6px 0 4px}summary{cursor:pointer;color:var(--accent)}
.tw{overflow-x:auto}table{border-collapse:collapse;width:100%;background:var(--card);border:1px solid var(--line);border-radius:10px;font-size:13px}
th,td{text-align:left;padding:5px 10px;border-bottom:1px solid var(--line);vertical-align:top;overflow-wrap:anywhere}th{color:var(--muted);font-weight:600}
td.n,th.n{text-align:right;white-space:nowrap}
`

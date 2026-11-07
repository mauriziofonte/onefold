// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/report"
	"github.com/mauriziofonte/onefold/internal/stats"
)

// cmdStats measures folders or whole volumes: what kinds of files, how much
// space, where, and how much of it is duplicated. Read-only.
func cmdStats(args []string) {
	fs := flag.NewFlagSet("stats", flag.ExitOnError)
	mounts := fs.Bool("mounts", false, "list the volumes (drives, mounted file systems) and exit")
	dups := fs.String("dups", stats.DupsFast, "duplicates: off | fast (same size and sampled content) | full (confirmed on the whole content)")
	dupMin := fs.String("dup-min", "1", "smallest file size checked for duplicates (e.g. 1M)")
	oneVol := fs.Bool("one-volume", true, "do not enter other volumes mounted below a folder (Linux, macOS)")
	depth := fs.Int("depth", 6, "folder levels kept in the tree (JSON) below each folder")
	top := fs.Int("top", 10, "rows printed per section (the JSON keeps more)")
	jsonOut := fs.String("json", "", "write the whole result as JSON to FILE (- = standard output, no tables)")
	readOnline := fs.Bool("read-online-only", false, "duplicates: also read cloud files that are not on this computer (OneDrive, iCloud, Dropbox placeholders), which downloads them")
	workers := fs.Int("j", 0, "workers (0 = NumCPU)")
	heartbeat := fs.Duration("heartbeat", report.DefaultHeartbeat, "progress log interval when output is not a terminal")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: onefold stats [flags] FOLDER|MOUNT...   |   onefold stats -mounts")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)
	if *mounts {
		printMounts(os.Stdout, stats.Mounts())
		return
	}
	if fs.NArg() == 0 {
		fs.Usage()
		os.Exit(2)
	}
	dm, err := catalog.ParseSize(*dupMin)
	if err != nil {
		fatal(2, fmt.Errorf("-dup-min: %w", err))
	}
	switch *dups {
	case stats.DupsOff, stats.DupsFast, stats.DupsFull:
	default:
		fatal(2, fmt.Errorf("-dups %q: use off, fast or full", *dups))
	}
	o := stats.Options{Roots: fs.Args(), Workers: *workers, OneVolume: *oneVol, Dups: *dups, DupMin: dm, Depth: *depth,
		Top: max(100, *top), ReadOnlineOnly: *readOnline}
	o.ProgressFor = func(phase string) (stats.Progress, func()) {
		label, unit := "stats scan", report.Items
		if phase == "dups" {
			label = "stats duplicates"
		}
		r := report.New(report.Options{Label: label, Unit: unit, Growing: phase == "scan", Heartbeat: *heartbeat})
		return r, r.Stop
	}
	res, err := stats.Run(o)
	if res == nil {
		fatal(1, err)
	}
	if *jsonOut != "" {
		if werr := writeStatsJSON(*jsonOut, res); werr != nil {
			fatal(1, werr)
		}
		if *jsonOut == "-" {
			if err != nil {
				fatal(1, err)
			}
			return
		}
	}
	printStats(os.Stdout, res, *top)
	if err != nil {
		fatal(1, err)
	}
}

func writeStatsJSON(path string, res *stats.Result) error {
	b, err := json.MarshalIndent(res, "", " ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if path == "-" {
		_, err = os.Stdout.Write(b)
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func printMounts(w io.Writer, ms []stats.Mount) {
	fmt.Fprintf(w, "%-30s %-10s %12s %12s %5s  %s\n", "VOLUME", "FS", "SIZE", "FREE", "USED", "NOTES")
	for _, m := range ms {
		var notes []string
		if m.Label != "" {
			notes = append(notes, m.Label)
		}
		if m.Remote {
			notes = append(notes, "network")
		}
		if m.Removable {
			notes = append(notes, "removable")
		}
		if m.ReadOnly {
			notes = append(notes, "read-only")
		}
		fmt.Fprintf(w, "%-30s %-10s %12s %12s %4.0f%%  %s\n", m.Path, m.FSType, report.HumanBytes(m.Total),
			report.HumanBytes(m.Free), 100*float64(m.Total-m.Free)/float64(max(m.Total, 1)), strings.Join(notes, ", "))
	}
}

// bar draws share (0..1) in width cells.
func bar(share float64, width int) string {
	n := int(share*float64(width) + 0.5)
	n = min(max(n, 0), width)
	if n == 0 && share > 0 {
		n = 1
	}
	return strings.Repeat("█", n) + strings.Repeat("░", width-n)
}

func sharePct(a, b int64) string {
	if b <= 0 {
		return "  –"
	}
	return fmt.Sprintf("%3.0f%%", 100*float64(a)/float64(b))
}

func thousands(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

func printStats(w io.Writer, r *stats.Result, top int) {
	for _, v := range r.Volumes {
		fmt.Fprintf(w, "\n%s  (volume %s", v.Root, v.Mount)
		if v.FSType != "" {
			fmt.Fprintf(w, ", %s", v.FSType)
		}
		if v.Total > 0 {
			fmt.Fprintf(w, ", %s, %s free", report.HumanBytes(v.Total), report.HumanBytes(v.Free))
		}
		fmt.Fprintln(w, ")")
	}
	fmt.Fprintf(w, "\nfiles %s   folders %s   size %s", thousands(r.Files), thousands(r.Dirs), report.HumanBytes(r.Bytes))
	if r.Allocated > 0 {
		fmt.Fprintf(w, "   on disk %s", report.HumanBytes(r.Allocated))
	}
	fmt.Fprintf(w, "   in %s\n", (time.Duration(r.Elapsed * float64(time.Second))).Round(time.Second))
	fmt.Fprintf(w, "empty %s   OS droppings %s (%s)   symbolic links %s (not followed)", thousands(r.Empty),
		thousands(r.Junk.Files), report.HumanBytes(r.Junk.Bytes), thousands(r.Symlinks))
	if r.HardLinks.Files > 0 {
		fmt.Fprintf(w, "   extra hard links %s (counted once)", thousands(r.HardLinks.Files))
	}
	fmt.Fprintf(w, "   unreadable %s\n", thousands(r.Errors))
	if r.OnlineOnly.Files > 0 {
		fmt.Fprintf(w, "only in the cloud: %s files, %s (counted, not on this disk, not read for duplicates)\n",
			thousands(r.OnlineOnly.Files), report.HumanBytes(r.OnlineOnly.Bytes))
	}
	if len(r.OtherVolumes) > 0 {
		fmt.Fprintf(w, "other volumes not entered: %s\n", strings.Join(r.OtherVolumes, ", "))
	}

	fmt.Fprintf(w, "\nBY FAMILY%26s %12s %14s\n", "", "SIZE", "FILES")
	for _, b := range r.Families {
		if b.Files == 0 {
			continue
		}
		fmt.Fprintf(w, "  %-10s %s %12s %s %14s\n", b.Key, bar(float64(b.Bytes)/float64(max(r.Bytes, 1)), 24),
			report.HumanBytes(b.Bytes), sharePct(b.Bytes, r.Bytes), thousands(b.Files))
	}

	fmt.Fprintf(w, "\nTOP EXTENSIONS (by size)\n")
	for i, b := range r.Exts {
		if i >= top {
			break
		}
		fmt.Fprintf(w, "  %-10s %-10s %12s %s %14s\n", "."+b.Key, b.Family, report.HumanBytes(b.Bytes), sharePct(b.Bytes, r.Bytes), thousands(b.Files))
	}
	if len(r.Unknown) > 0 {
		var parts []string
		for i, b := range r.Unknown {
			if i >= top {
				break
			}
			name := "." + b.Key
			if b.Key == "" {
				name = "(no extension)"
			}
			parts = append(parts, fmt.Sprintf("%s %s", name, report.HumanBytes(b.Bytes)))
		}
		fmt.Fprintf(w, "  not in the type registry: %s\n", strings.Join(parts, ", "))
	}

	fmt.Fprintf(w, "\nBY YEAR (last modified)\n")
	var maxY int64 = 1
	for _, y := range r.Years {
		maxY = max(maxY, y.Bytes)
	}
	for _, y := range r.Years {
		label := strconv.Itoa(y.Year)
		if y.Year == 0 {
			label = "?"
		}
		fmt.Fprintf(w, "  %-5s %s %12s %14s\n", label, bar(float64(y.Bytes)/float64(maxY), 30), report.HumanBytes(y.Bytes), thousands(y.Files))
	}

	fmt.Fprintf(w, "\nBY SIZE\n")
	for _, s := range r.Sizes {
		fmt.Fprintf(w, "  %-12s %14s files %12s\n", s.Label, thousands(s.Files), report.HumanBytes(s.Bytes))
	}

	fmt.Fprintf(w, "\nHEAVIEST FOLDERS\n")
	for _, t := range r.Tree {
		for i, c := range t.Children {
			if i >= top {
				break
			}
			fmt.Fprintf(w, "  %12s %s %10s files  %s\n", report.HumanBytes(c.Bytes), sharePct(c.Bytes, r.Bytes), thousands(c.Files), c.Path)
		}
	}

	fmt.Fprintf(w, "\nLARGEST FILES\n")
	for i, f := range r.Largest {
		if i >= top {
			break
		}
		fmt.Fprintf(w, "  %12s  %s\n", report.HumanBytes(f.Size), f.Path)
	}

	d := r.Dups
	if d == nil {
		fmt.Fprintln(w, "\nDUPLICATES: not measured (-dups off)")
		return
	}
	how := "same size and same sampled content; -dups full confirms on the whole content"
	if d.Mode == stats.DupsFull {
		how = "confirmed on the whole content"
	}
	fmt.Fprintf(w, "\nDUPLICATES: %s\n", how)
	fmt.Fprintf(w, "  %s groups, %s extra copies, %s wasted (%s of the size)", thousands(d.Groups), thousands(d.Extra),
		report.HumanBytes(d.Wasted), strings.TrimSpace(sharePct(d.Wasted, r.Bytes)))
	if d.Errors > 0 {
		fmt.Fprintf(w, ", %s unreadable", thousands(d.Errors))
	}
	fmt.Fprintln(w)
	for _, b := range d.ByFamily {
		fmt.Fprintf(w, "  %-10s %s %12s wasted %14s files in groups\n", b.Key, bar(float64(b.Wasted)/float64(max(d.Wasted, 1)), 24),
			report.HumanBytes(b.Wasted), thousands(b.Files))
	}
	if len(d.Pairs) > 0 {
		fmt.Fprintf(w, "\n  FOLDERS HOLDING THE SAME FILES\n")
		for i, p := range d.Pairs {
			if i >= top {
				break
			}
			if p.A == p.B {
				fmt.Fprintf(w, "  %12s %8s files  copies inside %s\n", report.HumanBytes(p.Bytes), thousands(p.Files), p.A)
				continue
			}
			fmt.Fprintf(w, "  %12s %8s files  %s\n  %22s  %s\n", report.HumanBytes(p.Bytes), thousands(p.Files), p.A, "and", p.B)
		}
	}
	if len(d.Top) > 0 {
		fmt.Fprintf(w, "\n  LARGEST GROUPS\n")
		for i, g := range d.Top {
			if i >= top {
				break
			}
			fmt.Fprintf(w, "  %12s wasted  %d × %s\n", report.HumanBytes(g.Wasted), g.Count, report.HumanBytes(g.Size))
			for j, p := range g.Paths {
				if j >= 4 {
					fmt.Fprintf(w, "      … %d more\n", g.Count-4)
					break
				}
				fmt.Fprintf(w, "      %s\n", p)
			}
		}
	}
}

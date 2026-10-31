// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/mauriziofonte/onefold/internal/backup"
	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/config"
	"github.com/mauriziofonte/onefold/internal/golden"
	"github.com/mauriziofonte/onefold/internal/plan"
	"github.com/mauriziofonte/onefold/internal/reclaim"
	"github.com/mauriziofonte/onefold/internal/report"
	"github.com/mauriziofonte/onefold/internal/seed"
	"github.com/mauriziofonte/onefold/internal/similar"
)

// cmdGoldenIndex: onefold golden-index -o INDEX [-manifest M]... <plan.jsonl>...
// Indexes what the golden tree holds from the plans applied to it, never from
// the tree itself (its bytes were changed by silvering).
func cmdGoldenIndex(args []string) {
	fs := flag.NewFlagSet("golden-index", flag.ExitOnError)
	out := fs.String("o", "", "write the golden index (JSONL) to this file (required)")
	var manifests multiFlag
	fs.Var(&manifests, "manifest", "manifest the plans were made from (repeat): metadata for plans without it, hashes of the drops")
	_ = fs.Parse(args)
	if fs.NArg() < 1 || *out == "" {
		fmt.Fprintln(os.Stderr, "usage: onefold golden-index -o INDEX [-manifest M]... <applied plan.jsonl>...")
		os.Exit(2)
	}
	var plans [][]plan.Action
	for _, p := range fs.Args() {
		acts, err := plan.Read(p)
		if err != nil {
			fatal(1, err)
		}
		plans = append(plans, acts)
	}
	var recs []catalog.Record
	if len(manifests) > 0 {
		var err error
		if recs, err = plan.LoadManifests(manifests); err != nil {
			fatal(1, err)
		}
	}
	es, st := golden.Build(plans, recs)
	writeFile(*out, func(f *os.File) error { return golden.Write(f, es) })
	fmt.Printf("golden index: %s\n", *out)
	fmt.Printf("  assets %d (from %d plan(s)); metadata from the manifests %d; matched by bytes only %d; repeated in a later plan %d\n",
		st.Entries, len(plans), st.Joined, st.Unjoined, st.Repeated)
	if st.Unjoined > 0 {
		fmt.Printf("  %d photos/videos have no metadata (plan without it, no -manifest): a re-encoded copy of them is not recognized\n", st.Unjoined)
	}
}

// cmdMatch: onefold match -index INDEX -o SUBPLAN [-report FILE] <manifest.jsonl>...
// Plans the new files and keeps only what the golden tree does not hold.
func cmdMatch(args []string) {
	fs := flag.NewFlagSet("match", flag.ExitOnError)
	index := fs.String("index", "", "golden index (golden-index -o) of the tree (required)")
	out := fs.String("o", "", "write the sub-plan of the new assets to this file (apply it like a plan)")
	reportp := fs.String("report", "", "write the duplicates and the upgrades (JSON)")
	videoTier := fs.Bool("video-tier", false, "videos under VIDEOS/<YEAR>/<tier>/ (as the tree was planned)")
	videoTol := fs.Float64("video-tolerance", 0, "seconds: a video of the same start time within this duration difference is the same shot (as plan -video-tolerance)")
	filenameSeq := fs.Bool("filename-seq", false, "as plan -filename-seq (as the tree was planned)")
	layout := fs.String("layout", "dedupe", "generic-kind naming, as the tree was planned: "+strings.Join(plan.Layouts, "|")+", or a preset/template")
	fixExt := fs.Bool("fix-ext", false, "generic kinds: extension implied by the content (as the tree was planned)")
	artist := fs.String("artist", "", "owner stamped on photo/video copies (default: `onefold config` artist; \""+config.None+"\" = none)")
	_ = fs.Parse(args)
	if fs.NArg() < 1 || *index == "" {
		fmt.Fprintln(os.Stderr, "usage: onefold match -index INDEX [-o SUBPLAN] [-report FILE] <manifest.jsonl>...")
		os.Exit(2)
	}
	if !plan.ValidLayout(*layout) {
		fatal(2, fmt.Errorf("unknown layout %q (valid: %s, or a naming preset/template)", *layout, strings.Join(plan.Layouts, ", ")))
	}
	ix, err := golden.Load(*index)
	if err != nil {
		fatal(1, err)
	}
	recs, err := plan.LoadManifests(fs.Args())
	if err != nil {
		fatal(1, err)
	}
	cfg := loadConfig()
	o := plan.Options{VideoTier: *videoTier, VideoTolerance: *videoTol, FilenameSeq: *filenameSeq, Layout: *layout, FixExt: *fixExt, SelfContained: true, Artist: config.Override(*artist, cfg.Artist)}
	m := golden.Match(ix, recs, o)
	if *out != "" {
		writeFile(*out, func(f *os.File) error { return plan.Write(f, m.New) })
		fmt.Fprintf(os.Stderr, "sub-plan: %s (%d actions)\n", *out, len(m.New))
	}
	if *reportp != "" {
		writeFile(*reportp, func(f *os.File) error {
			enc := json.NewEncoder(f)
			enc.SetIndent("", "  ")
			return enc.Encode(map[string]any{"index": *index, "manifests": fs.Args(), "new": len(m.New), "renamed": m.Renamed,
				"dupes": m.Dupes, "upgrades": m.Upgrades})
		})
		fmt.Fprintf(os.Stderr, "report: %s\n", *reportp)
	}
	fmt.Printf("input files:  %d -> assets %d (golden index: %d)\n", m.Plan.InputFiles, m.Plan.Assets, len(ix.Entries))
	fmt.Printf("new:          %d (names already in the tree, given a _NN suffix: %d)\n", len(m.New), m.Renamed)
	fmt.Printf("in the tree:  %d\n", len(m.Dupes))
	fmt.Printf("upgrades:     %d (same shot, more pixels: listed in -report, never applied; the tree only grows)\n", len(m.Upgrades))
}

func writeFile(p string, write func(*os.File) error) {
	f, err := os.Create(p)
	if err != nil {
		fatal(1, err)
	}
	if err := write(f); err != nil {
		f.Close()
		fatal(1, fmt.Errorf("write %s: %w", p, err))
	}
	if err := f.Close(); err != nil {
		fatal(1, fmt.Errorf("write %s: %w", p, err))
	}
}

// cmdSimilar: onefold similar [-max-distance N] [-only-host H] [-o FILE] <plan.jsonl>
// Lists golden photos that look alike for a manual review; drops nothing.
func cmdSimilar(args []string) {
	fs := flag.NewFlagSet("similar", flag.ExitOnError)
	maxDist := fs.Int("max-distance", 6, "pairs at most this many of 64 bits apart (0..16; higher = more pairs, more false positives)")
	onlyHost := fs.String("only-host", "", "hash only the photos whose srcHost equals this label (run where they are readable)")
	out := fs.String("o", "", "write every pair (JSON) to this file")
	workers := fs.Int("j", 0, "parallel decoders (0 = NumCPU)")
	docs := fs.Bool("docs", false, "also group documents whose extracted text is the same (docx/odt/rtf/doc/xls/ppt in pure Go, pdf with pdftotext)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: onefold similar [-max-distance N] [-docs] [-only-host H] [-o FILE] <plan.jsonl>")
		os.Exit(2)
	}
	acts, err := plan.Read(fs.Arg(0))
	if err != nil {
		fatal(1, err)
	}
	so := similar.Options{MaxDistance: *maxDist, OnlyHost: *onlyHost, Workers: *workers}
	r, err := similar.Find(acts, so)
	if err != nil {
		fatal(2, err)
	}
	var groups []similar.DocGroup
	nDocs := 0
	if *docs {
		if groups, nDocs, err = similar.FindDocs(acts, so); err != nil {
			fatal(1, err)
		}
	}
	if *out != "" {
		writeFile(*out, func(f *os.File) error {
			enc := json.NewEncoder(f)
			enc.SetIndent("", "  ")
			return enc.Encode(struct {
				*similar.Result
				Docs []similar.DocGroup `json:"docGroups,omitempty"`
			}{r, groups})
		})
		fmt.Fprintf(os.Stderr, "pairs: %s\n", *out)
	}
	fmt.Printf("photos hashed %d, plain (not compared) %d, skipped %d (no pure-Go decoder: HEIC, RAW, WebP, TIFF...), unreadable %d\n",
		r.Hashed, r.Flat, r.Skipped, len(r.Failed))
	fmt.Printf("look-alike pairs (distance <= %d): %d; review them, nothing is dropped\n", *maxDist, len(r.Pairs))
	for i, p := range r.Pairs {
		if i == 20 {
			fmt.Printf("  ... %d more (-o FILE lists them all)\n", len(r.Pairs)-20)
			break
		}
		fmt.Printf("  %2d  %s  ~  %s\n", p.Distance, p.A.CanonRel, p.B.CanonRel)
	}
	if *docs {
		fmt.Printf("documents read %d; groups with the same text: %d (review them, nothing is dropped)\n", nDocs, len(groups))
		for _, g := range groups {
			fmt.Printf("  %d chars, %s:\n", g.Chars, strings.Join(g.Formats, "+"))
			for _, m := range g.Members {
				fmt.Printf("    %s\n", m.CanonRel)
			}
		}
	}
}

// cmdReclaim: onefold reclaim -target G [-only-host H] [-o REPORT] [-script FILE] <plan.jsonl>
// Verifies the drops of an applied plan and writes a deletion script for
// the exact duplicates; onefold itself deletes nothing.
func cmdReclaim(args []string) {
	fs := flag.NewFlagSet("reclaim", flag.ExitOnError)
	target := fs.String("target", "", "golden tree root the plan was applied to (required)")
	onlyHost := fs.String("only-host", "", "drops of this host only (run where they and their winners are readable)")
	out := fs.String("o", "", "write the report (JSON): safe, review, skipped")
	script := fs.String("script", "", "write the deletion script of the verified duplicates; review it, then run it yourself")
	ps := fs.Bool("ps", runtime.GOOS == "windows", "the script is PowerShell (default on Windows; -ps=false: POSIX sh)")
	workers := fs.Int("j", 0, "parallel readers (0 = NumCPU)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 || *target == "" {
		fmt.Fprintln(os.Stderr, "usage: onefold reclaim -target G [-only-host H] [-o REPORT] [-script FILE] <applied plan.jsonl>")
		os.Exit(2)
	}
	acts, err := plan.Read(fs.Arg(0))
	if err != nil {
		fatal(1, err)
	}
	r, err := reclaim.Check(acts, reclaim.Options{Target: *target, OnlyHost: *onlyHost, Workers: *workers})
	if err != nil {
		fatal(2, err)
	}
	if *out != "" {
		writeFile(*out, func(f *os.File) error {
			enc := json.NewEncoder(f)
			enc.SetIndent("", "  ")
			return enc.Encode(r)
		})
		fmt.Fprintf(os.Stderr, "report: %s\n", *out)
	}
	if *script != "" {
		writeFile(*script, func(f *os.File) error {
			return reclaim.Script(f, r.Safe, *ps)
		})
		fmt.Fprintf(os.Stderr, "script: %s (nothing was deleted: review it, then run it)\n", *script)
	}
	fmt.Printf("exact duplicates verified now: %d (%s): in the script\n", len(r.Safe), report.HumanBytes(r.SafeBytes))
	fmt.Printf("not verified: %d (see the report: other host, missing golden copy, changed content...)\n", len(r.Skipped))
	fmt.Printf("for review (lower-res, smaller, variants: distinct content, never in the script): %d\n", len(r.Review))
}

// cmdBackup: onefold backup [-execute] SRC DST | onefold backup -check DST
// Cold-storage copy of a tree with its content manifest, and its audit.
func cmdBackup(args []string) {
	fs := flag.NewFlagSet("backup", flag.ExitOnError)
	execute := fs.Bool("execute", false, "ACTUALLY copy (default: dry run, nothing written)")
	check := fs.Bool("check", false, "audit a copy: read every file again against its manifests (one argument: DST)")
	workers := fs.Int("j", 0, "parallel copies or reads (0 = half the CPUs)")
	_ = fs.Parse(args)
	if *check {
		if fs.NArg() != 1 {
			fmt.Fprintln(os.Stderr, "usage: onefold backup -check DST")
			os.Exit(2)
		}
		a, err := backup.Check(fs.Arg(0), *workers, nil)
		if err != nil {
			fatal(1, err)
		}
		fmt.Printf("manifests %d; files ok %d (%s)\n", a.Manifests, a.OK, report.HumanBytes(a.Bytes))
		for _, l := range []struct {
			name  string
			paths []string
		}{{"CHANGED (bit rot or edited)", a.Changed}, {"MISSING", a.Missing}, {"UNREADABLE", a.Failed}, {"untracked (not from a backup run)", a.Untracked}} {
			if len(l.paths) > 0 {
				fmt.Printf("%s: %d\n", l.name, len(l.paths))
				for _, p := range l.paths {
					fmt.Printf("  %s\n", p)
				}
			}
		}
		if !a.Clean() {
			os.Exit(1)
		}
		return
	}
	if fs.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: onefold backup [-execute] [-j N] SRC DST  |  onefold backup -check DST")
		os.Exit(2)
	}
	r, err := backup.Run(fs.Arg(0), fs.Arg(1), backup.Options{Execute: *execute, Workers: *workers})
	if err != nil {
		fatal(1, err)
	}
	verb := "to copy"
	if *execute {
		verb = "copied"
	}
	fmt.Printf("%s %d (%s); already in the copy %d; conflicts (another file under the same path, never replaced) %d; failed %d\n",
		verb, r.Copied, report.HumanBytes(r.CopiedBytes), r.Present, len(r.Conflicts), len(r.Failed))
	for _, l := range append(append([]string(nil), r.Conflicts...), r.Failed...) {
		fmt.Printf("  %s\n", l)
	}
	if r.Manifest != "" {
		fmt.Printf("manifest: %s (audit later with: onefold backup -check %s)\n", r.Manifest, fs.Arg(1))
	} else if !*execute {
		fmt.Println("(dry run: add -execute)")
	}
	if len(r.Conflicts)+len(r.Failed) > 0 {
		os.Exit(1)
	}
}

// cmdSeed: onefold seed -from STAGING -to DEST -o SCRIPT [-format rsync|robocopy] <plan.jsonl>
// Writes the bulk-copy script of a materialized tree; onefold runs nothing.
func cmdSeed(args []string) {
	fs := flag.NewFlagSet("seed", flag.ExitOnError)
	from := fs.String("from", "", "staging tree the plan was applied to (apply --execute --target)")
	to := fs.String("to", "", "destination: a path, or user@host:path for rsync")
	out := fs.String("o", "", "script to write (required): sh for rsync, cmd for robocopy")
	list := fs.String("list", "", "rsync: file list to write (default: the script path + \"-files\")")
	format := fs.String("format", "rsync", "rsync | robocopy")
	onlyHost := fs.String("only-host", "", "only the actions whose srcHost equals this label")
	_ = fs.Parse(args)
	if fs.NArg() != 1 || *from == "" || *to == "" || *out == "" || (*format != "rsync" && *format != "robocopy") {
		fmt.Fprintln(os.Stderr, "usage: onefold seed -from STAGING -to DEST -o SCRIPT [-format rsync|robocopy] [-list FILE] [-only-host H] <plan.jsonl>")
		os.Exit(2)
	}
	acts, err := plan.Read(fs.Arg(0))
	if err != nil {
		fatal(1, err)
	}
	rels, res := seed.Collect(acts, *from, *onlyHost)
	if *format == "robocopy" {
		writeFile(*out, func(f *os.File) error { return seed.Robocopy(f, rels, *from, *to) })
	} else {
		lp := *list
		if lp == "" {
			lp = *out + "-files"
		}
		var sh, l strings.Builder
		if err := seed.Rsync(&sh, &l, rels, *from, *to, lp); err != nil {
			fatal(1, err)
		}
		writeFile(lp, func(f *os.File) error { _, err := f.WriteString(l.String()); return err })
		writeFile(*out, func(f *os.File) error { _, err := f.WriteString(sh.String()); return err })
		fmt.Fprintf(os.Stderr, "file list: %s\n", lp)
	}
	fmt.Printf("seed script: %s (%s): %d files, %s; run it yourself, existing files at the destination are never replaced\n",
		*out, *format, res.Files, report.HumanBytes(res.Bytes))
	if len(res.Missing) > 0 {
		fmt.Printf("not in the staging tree (apply --execute first): %d\n", len(res.Missing))
	}
}

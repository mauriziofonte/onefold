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

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/dedupe"
	"github.com/mauriziofonte/onefold/internal/hashcache"
	"github.com/mauriziofonte/onefold/internal/report"
	"github.com/mauriziofonte/onefold/internal/stats"
)

// cmdDedupe cleans up the duplicate copies in folders: one copy of each
// group stays, the others go to the Trash, become hard links or clones of
// it, or are listed in a deletion script. Dry-run unless -execute, which
// needs -expect N.
func cmdDedupe(args []string) {
	fs := flag.NewFlagSet("dedupe", flag.ExitOnError)
	keep := fs.String("keep", dedupe.KeepOldest, "the copy that stays: oldest | newest (modification time) | shortest (path) | first (under the first folder given)")
	keepIn := fs.String("keep-in", "", "keep the copy inside this folder (the -keep rule picks among several there); groups without one are left alone")
	action := fs.String("action", "", "what happens to the other copies: trash (Recycle Bin / Trash) | link (hard links) | clone (clones: APFS, Btrfs, XFS) | script (a deletion script, onefold deletes nothing)")
	script := fs.String("script", "", "with -action script: the script to write; review it, then run it yourself")
	ps := fs.Bool("ps", runtime.GOOS == "windows", "the script is PowerShell (default on Windows; -ps=false: POSIX sh)")
	minSize := fs.String("min-size", "1", "smaller files are left alone (e.g. 1M)")
	execute := fs.Bool("execute", false, "ACTUALLY act (default: dry-run, nothing touched); needs -expect")
	expect := fs.Int("expect", -1, "with -execute: the number of copies the dry run listed (typed confirmation)")
	resume := fs.String("resume", "", "finish or undo the replacements an interrupted run left half done, from its journal")
	jsonOut := fs.String("json", "", "write the result as JSON to FILE (- = standard output, no text)")
	list := fs.Bool("list", false, "print every copy acted on and every one left alone (TSV)")
	workers := fs.Int("j", 0, "workers (0 = NumCPU/2)")
	heartbeat := fs.Duration("heartbeat", report.DefaultHeartbeat, "progress log interval when output is not a terminal")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: onefold dedupe -action trash|link|clone|script [-keep RULE] [-keep-in DIR] [-script FILE] [-execute -expect N] FOLDER...\n       onefold dedupe -resume JOURNAL")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)
	if *resume != "" {
		if fs.NArg() > 0 {
			fs.Usage()
			os.Exit(2)
		}
		fin, rest, problems, err := dedupe.Resume(*resume)
		if err != nil {
			fatal(1, err)
		}
		fmt.Printf("dedupe resume: %d replacements finished, %d originals put back\n", fin, rest)
		for _, p := range problems {
			fmt.Printf("  PROBLEM %s\n", p)
		}
		if len(problems) > 0 {
			os.Exit(1)
		}
		return
	}
	if fs.NArg() == 0 || *action == "" {
		fs.Usage()
		os.Exit(2)
	}
	switch *keep {
	case dedupe.KeepOldest, dedupe.KeepNewest, dedupe.KeepShortest, dedupe.KeepFirst:
	default:
		fatal(2, fmt.Errorf("-keep %q: use oldest, newest, shortest or first", *keep))
	}
	switch *action {
	case dedupe.ActTrash, dedupe.ActLink, dedupe.ActClone, dedupe.ActScript:
	default:
		fatal(2, fmt.Errorf("-action %q: use trash, link, clone or script", *action))
	}
	ms, err := catalog.ParseSize(*minSize)
	if err != nil {
		fatal(2, fmt.Errorf("-min-size: %w", err))
	}
	if *action == dedupe.ActScript {
		if *script == "" {
			fatal(2, fmt.Errorf("-action script needs -script FILE"))
		}
		if *execute {
			fatal(2, fmt.Errorf("-action script runs nothing: drop -execute and run the script yourself"))
		}
	} else if *script != "" {
		fatal(2, fmt.Errorf("-script goes with -action script"))
	}
	if *execute && *expect < 0 {
		fatal(2, fmt.Errorf("-execute needs -expect N: run the dry run first and pass the number of copies it lists"))
	}
	o := dedupe.Options{Roots: fs.Args(), Keep: *keep, KeepIn: *keepIn, Action: *action, MinSize: ms,
		Execute: *execute, Expect: *expect, Workers: *workers}
	o.ProgressFor = func(phase string) (stats.Progress, func()) {
		label, unit := "dedupe scan", report.Items
		switch phase {
		case "dups":
			label = "dedupe duplicates"
		case "dedupe":
			label = "dedupe " + *action
		}
		r := report.New(report.Options{Label: label, Unit: unit, Growing: phase == "scan", Heartbeat: *heartbeat})
		return r, r.Stop
	}
	o.Cache = hashcache.Open()
	res, err := dedupe.Run(o)
	o.Cache.Close()
	if res == nil {
		fatal(1, err) // a folder that cannot be read
	}
	if err == nil && *action == dedupe.ActScript {
		writeFile(*script, func(f *os.File) error { return dedupe.Script(f, res, *ps) })
	}
	if *jsonOut != "" {
		b, jerr := json.MarshalIndent(res, "", " ")
		if jerr == nil {
			b = append(b, '\n')
			if *jsonOut == "-" {
				_, jerr = os.Stdout.Write(b)
			} else {
				jerr = os.WriteFile(*jsonOut, b, 0o644)
			}
		}
		if jerr != nil {
			fatal(1, jerr)
		}
	}
	if *jsonOut != "-" {
		printDedupe(res, *execute, *list, *script)
	}
	if err != nil {
		fatal(1, err)
	}
	if len(res.Failed) > 0 {
		os.Exit(1)
	}
}

var dedupeHow = map[string]string{
	dedupe.ActTrash:  "moved to the Trash",
	dedupe.ActLink:   "replaced by hard links of the kept copy",
	dedupe.ActClone:  "replaced by clones of the kept copy",
	dedupe.ActScript: "listed in the deletion script",
}

func printDedupe(res *dedupe.Result, executed, list bool, script string) {
	how := dedupeHow[res.Action]
	if res.Action == dedupe.ActTrash && runtime.GOOS == "windows" {
		how = "moved to the Recycle Bin"
	}
	mode := "DRY-RUN (nothing touched; add -execute -expect N)"
	switch {
	case res.Action == dedupe.ActScript:
		mode = "SCRIPT (onefold deletes nothing: review the script, then run it)"
	case executed:
		mode = "EXECUTED"
	}
	fmt.Printf("\ndedupe %s\n", mode)
	fmt.Printf("folders:          %v\n", res.Roots)
	keep := res.Keep
	if res.KeepIn != "" {
		keep += ", inside " + res.KeepIn
	}
	fmt.Printf("kept copy:        %s\n", keep)
	fmt.Printf("other copies:     %s\n", how)
	fmt.Printf("groups:           %d\n", res.Groups)
	fmt.Printf("copies:           %d, %s (confirmed on the whole content; read again right before each one)\n", len(res.Items), report.HumanBytes(res.Bytes))
	fmt.Printf("left alone:       %d\n", len(res.Skipped))
	if list {
		verb := strings.ToUpper(res.Action)
		for _, it := range res.Items {
			fmt.Printf("%s\t%s\t(kept: %s)\n", verb, it.Extra, it.Keep)
		}
		for _, it := range res.Skipped {
			fmt.Printf("LEFT\t%s\t%s\n", it.Extra, it.Why)
		}
	}
	if res.Action == dedupe.ActScript && script != "" {
		fmt.Printf("script:           %s (%d copies verified now, %s)\n", script, res.Done, report.HumanBytes(res.DoneBytes))
	} else if res.Journal != "" {
		fmt.Printf("%-18s%d copies, %s\n", how+":", res.Done, report.HumanBytes(res.DoneBytes))
		fmt.Printf("journal:          %s\n", res.Journal)
	}
	for _, f := range res.Failed {
		fmt.Printf("  FAILED %s: %s\n", f.Extra, f.Why)
	}
	if res.Stopped {
		fmt.Println("stopped before the end: run the dry run again for what is left")
	}
}

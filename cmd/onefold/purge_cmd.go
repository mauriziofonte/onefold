// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/mauriziofonte/onefold/internal/purge"
	"github.com/mauriziofonte/onefold/internal/report"
)

// cmdPurgeSources deletes the input files of an executed merge whose content
// is in the output: dry-run unless -execute, which needs -expect N.
func cmdPurgeSources(args []string) {
	fs := flag.NewFlagSet("purge-sources", flag.ExitOnError)
	from := fs.String("from", "", "merge report (JSON) of an executed merge: onefold merge -execute -report FILE")
	execute := fs.Bool("execute", false, "ACTUALLY delete (default: dry-run, nothing touched); needs -expect")
	expect := fs.Int("expect", -1, "with -execute: the number of files the dry run listed (typed confirmation)")
	trash := fs.Bool("trash", false, "move the files to the Recycle Bin instead of deleting them (Windows, local fixed drives only)")
	workers := fs.Int("j", 0, "verify/delete workers (0 = NumCPU)")
	list := fs.Bool("list", false, "print every file to delete and every refused one (TSV)")
	heartbeat := fs.Duration("heartbeat", report.DefaultHeartbeat, "progress log interval when output is not a terminal")
	_ = fs.Parse(args)
	if *from == "" || fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "usage: onefold purge-sources -from MERGE-REPORT.json [-execute -expect N] [-trash] [-list]")
		os.Exit(2)
	}
	if *execute && *expect < 0 {
		fatal(2, fmt.Errorf("-execute needs -expect N: run the dry run first and pass the number of files it lists"))
	}
	o, err := purge.ReadReport(*from)
	if err != nil {
		fatal(2, err)
	}
	o.Execute, o.Expect, o.Trash, o.Workers, o.Log, o.Heartbeat = *execute, *expect, *trash, *workers, report.Writer, *heartbeat
	res, err := purge.Run(o)
	if res == nil {
		fatal(1, err)
	}
	mode := "DRY-RUN (nothing deleted; add -execute -expect N)"
	if *execute {
		mode = "EXECUTED"
	}
	how := "deleted"
	if *trash {
		how = "moved to the Recycle Bin"
	}
	fmt.Printf("\npurge-sources %s\n", mode)
	fmt.Printf("output:            %s\n", res.Output)
	fmt.Printf("input folders:     %v\n", res.Rooms)
	fmt.Printf("to delete:         %d files, %s (content verified in the output again right before each one)\n", len(res.Candidates), report.HumanBytes(res.Bytes))
	fmt.Printf("refused:           %d\n", len(res.Refused))
	if *list {
		for _, c := range res.Candidates {
			fmt.Printf("DELETE\t%s\t(copy: %s)\n", c.Src, c.Keeper)
		}
		for _, r := range res.Refused {
			fmt.Printf("REFUSED\t%s\t%s\n", r.Src, r.Reason)
		}
	}
	if res.Journal != "" {
		fmt.Printf("%-19s%d files, %s; already gone %d\n", how+":", res.Deleted, report.HumanBytes(res.DeletedBytes), res.Gone)
		fmt.Printf("empty dirs:        %d removed (%d OS droppings with them)\n", res.DirsRemoved, res.JunkRemoved)
		fmt.Printf("journal:           %s\n", res.Journal)
		for _, f := range res.Failed {
			fmt.Printf("  FAILED %s\n", f)
		}
	}
	if err != nil {
		fatal(1, err)
	}
	if len(res.Failed) > 0 {
		os.Exit(1)
	}
}

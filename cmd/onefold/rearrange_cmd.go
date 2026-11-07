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

	"github.com/mauriziofonte/onefold/internal/hashcache"
	"github.com/mauriziofonte/onefold/internal/rearrange"
	"github.com/mauriziofonte/onefold/internal/report"
	"github.com/mauriziofonte/onefold/internal/settings"
)

// cmdRearrange reorganizes a tree in place (dry-run unless -execute).
func cmdRearrange(args []string) {
	fs := flag.NewFlagSet("rearrange", flag.ExitOnError)
	namingSpec := fs.String("naming", "ext-year", "naming preset, template or selector map (`onefold naming` lists them); with -name: a free template")
	names := nameFlags{}
	fs.Var(names, "name", "file family and its naming: FAMILY=PRESET[+clean|+raw|+prefix[:N]] (repeatable; families "+
		familyKeys()+", all); the families select the file types")
	settingsFile := fs.String("settings", "", "settings file (JSON, also exported by the GUI); flags given here override it")
	printSettings := fs.Bool("print-settings", false, "print the settings these flags make (JSON for -settings) and exit")
	heartbeat := fs.Duration("heartbeat", report.DefaultHeartbeat, "progress log interval when output is not a terminal")
	execute := fs.Bool("execute", false, "ACTUALLY move/delete (default: dry-run, nothing touched)")
	dupes := fs.String("dupes", "delete", "confirmed duplicates (sparse + full hash): delete | report")
	keepEmpty := fs.Bool("keep-empty-dirs", false, "do not remove directories emptied by the run")
	noCache := fs.Bool("no-cache", false, "do not use/write <root>/.onefold/merge-cache.jsonl")
	workers := fs.Int("move-j", 0, "rename/verify workers (0 = NumCPU)")
	resume := fs.String("resume", "", "complete an interrupted run from its journal")
	undo := fs.String("undo", "", "move every file of a journal back to its original path (deletions are final)")
	list := fs.Bool("list", false, "print every planned move / delete and every excluded file (TSV)")
	reportp := fs.String("report", "", "write the plan/result as JSON")
	cf := addCatalogFlags(fs, "all", runtime.NumCPU(), regimeTemplate)
	perfFlags := addPerfFlags(fs)
	_ = fs.Parse(args)

	if *resume != "" || *undo != "" {
		j, isUndo := *resume, false
		if *undo != "" {
			j, isUndo = *undo, true
		}
		r, err := rearrange.RecoverWith(j, isUndo, rearrange.RecoverOptions{Log: report.Writer, Heartbeat: *heartbeat})
		if err != nil {
			fatal(1, err)
		}
		fmt.Printf("%s: %d files moved, %d duplicates deleted, %d empty dirs removed, %d failed\n",
			map[bool]string{false: "resume", true: "undo"}[isUndo], r.Moved, r.Deleted, r.DirsRemoved, r.Failed)
		for _, e := range r.Errors {
			fmt.Printf("  FAILED %s\n", e)
		}
		if r.Failed > 0 {
			os.Exit(1)
		}
		return
	}
	set := flagsGiven(fs)
	var rs settings.Rearrange
	if *settingsFile != "" {
		if err := settings.Load(*settingsFile, "rearrange", &rs); err != nil {
			fatal(2, err)
		}
	}
	rs.Kind = "rearrange"
	if fs.NArg() == 1 {
		rs.Root = fs.Arg(0)
	}
	if len(names) > 0 {
		rs.Families, rs.Spec, rs.Clean, rs.Layout = names, "", false, ""
	}
	switch {
	case set["naming"] && len(rs.Families) > 0:
		rs.Template = *namingSpec
	case set["naming"] || (*settingsFile == "" && len(rs.Families) == 0):
		rs.Spec, rs.Template = *namingSpec, ""
	}
	if set["dupes"] {
		rs.ReportDupes = *dupes == "report"
	}
	if set["keep-empty-dirs"] {
		rs.KeepEmptyDirs = *keepEmpty
	}
	if set["no-cache"] {
		rs.NoCache = *noCache
	}
	if set["move-j"] {
		rs.Workers = *workers
	}
	var err error
	if rs.Filters, err = cf.filters(rs.Filters); err != nil {
		fatal(2, err)
	}
	if *printSettings {
		b, _ := json.MarshalIndent(rs, "", "  ")
		fmt.Println(string(b))
		return
	}
	if fs.NArg() > 1 || rs.Root == "" || (*dupes != "delete" && *dupes != "report") {
		fmt.Fprintln(os.Stderr, "usage: onefold rearrange [-naming P | -name F=P ...] [-dupes delete|report] [-execute] <root>   (or -settings FILE, -resume/-undo <journal>)")
		os.Exit(2)
	}
	printPerf := perfFlags()
	o, err := rs.Options(*cf.allowMixed)
	if err != nil {
		fatal(2, mixedHint(err))
	}
	cf.hash(&o.Catalog)
	o.Execute, o.Log, o.Heartbeat = *execute, report.Writer, *heartbeat
	o.Catalog.Cache = hashcache.Open()
	res, err := rearrange.Run(o)
	o.Catalog.Cache.Close()
	if err != nil {
		printPerf()
		fatal(1, err)
	}
	mode := "DRY-RUN (nothing touched; add -execute)"
	if *execute {
		mode = "EXECUTED"
	}
	fmt.Printf("\nrearrange %s\n", mode)
	fmt.Printf("root:              %s\n", res.Root)
	fmt.Printf("naming:            %s\n", res.Naming)
	fmt.Printf("files in scope:    %d (skipped by filters/dotfiles/empty: %d)\n", res.Files, res.Skipped)
	if len(res.Excluded) > 0 {
		fmt.Printf("excluded:          %s (they stay where they are)\n", excludedSummary(res.Excluded))
	}
	printPackages(res.Packages)
	fmt.Printf("already in place:  %d\n", res.Stay)
	fmt.Printf("to move/rename:    %d\n", len(res.Moves))
	fmt.Printf("duplicates:        %d groups confirmed on full content (%d split); %d files %s (%s)\n",
		res.DupGroups, res.Verify.Splits, len(res.Deletes), map[bool]string{true: "to delete", false: "kept"}[*dupes == "delete"], report.HumanBytes(res.DeleteBytes))
	if *execute {
		fmt.Printf("moved:             %d   deleted: %d   empty dirs removed: %d\n", res.Moved, res.Deleted, res.DirsRemoved)
		fmt.Printf("journal:           %s   (undo moves: onefold rearrange -undo <journal>)\n", res.Journal)
		for _, f := range res.Failed {
			fmt.Printf("  FAILED %s\n", f)
		}
	}
	if *list {
		for _, d := range res.Deletes {
			fmt.Printf("DELETE\t%s\t(dup of %s)\n", d.Path, d.Keeper)
		}
		for _, m := range res.Moves {
			fmt.Printf("MOVE\t%s\t-> %s\n", m.Src, m.Dst)
		}
		for _, x := range res.Excluded {
			fmt.Printf("EXCL\t%s\t%s\n", x.Rule, x.Path)
		}
		listPackages(res.Packages)
	}
	if *reportp != "" {
		b, _ := json.MarshalIndent(res, "", "  ")
		if err := os.WriteFile(*reportp, b, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "report:", err)
		}
	}
	printPerf()
	if len(res.Failed) > 0 {
		os.Exit(1)
	}
}

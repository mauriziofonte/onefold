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
	"sort"
	"strings"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/config"
	"github.com/mauriziofonte/onefold/internal/kinds"
	"github.com/mauriziofonte/onefold/internal/merge"
	"github.com/mauriziofonte/onefold/internal/meta"
	"github.com/mauriziofonte/onefold/internal/naming"
	"github.com/mauriziofonte/onefold/internal/perf"
	"github.com/mauriziofonte/onefold/internal/plan"
	"github.com/mauriziofonte/onefold/internal/report"
	"github.com/mauriziofonte/onefold/internal/settings"
	"github.com/mauriziofonte/onefold/internal/tools"
)

// cmdMerge folds N input trees into an output tree (dry-run unless -execute).
func cmdMerge(args []string) {
	fs := flag.NewFlagSet("merge", flag.ExitOnError)
	var inputs multiFlag
	names := nameFlags{}
	fs.Var(&inputs, "input", "input directory (repeatable; may equal -output)")
	output := fs.String("output", "", "output directory (receives every distinct content not already there)")
	execute := fs.Bool("execute", false, "ACTUALLY copy (default: dry-run, nothing written)")
	settingsFile := fs.String("settings", "", "settings file (JSON, also exported by the GUI); flags given here override it")
	printSettings := fs.Bool("print-settings", false, "print the settings these flags make (JSON for -settings) and exit")
	fs.Var(names, "name", "file family and its naming: FAMILY=PRESET[+clean|+raw|+prefix[:N]] (repeatable; families "+
		familyKeys()+", all); the families select the file types")
	layout := fs.String("layout", "ext-year", "alias of -naming: any preset (the golden tree aliases dedupe | timeline | document | music | device need -allow-mixed)")
	namingSpec := fs.String("naming", "", "naming preset, template or selector map (default ext-year; `onefold naming` lists them); with -name: a free template")
	trust := fs.Bool("trust-sparse", false, "skip the full-content confirmation of duplicates (faster, NOT recommended)")
	noVerify := fs.Bool("no-verify-copy", false, "do not re-read each copy to verify it")
	noFsync := fs.Bool("no-fsync", false, "do not fsync each copy before publishing it")
	verifyUncached := fs.Bool("verify-uncached", false, "re-read each copy from the disk, not from the OS cache (slower: proves the media, not only the write path)")
	noCache := fs.Bool("no-cache", false, "do not use/write <output>/.onefold/merge-cache.jsonl")
	keepDotfiles := fs.Bool("keep-dotfiles", false, "also merge files whose name starts with '.' and 0-byte files (one empty file per name)")
	workers := fs.Int("copy-j", 0, "copy/verify workers (0 = NumCPU)")
	list := fs.Bool("list", false, "print every planned copy and every excluded file (TSV)")
	reportp := fs.String("report", "", "write the merge plan/result as JSON (purge-sources -from reads it)")
	heartbeat := fs.Duration("heartbeat", report.DefaultHeartbeat, "progress log interval when output is not a terminal")
	cf := addCatalogFlags(fs, "all", runtime.NumCPU(), regimeTemplate)
	perfFlags := addPerfFlags(fs)
	_ = fs.Parse(args)
	set := flagsGiven(fs)
	var m settings.Merge
	if *settingsFile != "" {
		if err := settings.Load(*settingsFile, "merge", &m); err != nil {
			fatal(2, err)
		}
	}
	m.Kind = "merge"
	if len(inputs) > 0 {
		m.Inputs = inputs
	}
	if set["output"] {
		m.Output = *output
	}
	if len(names) > 0 {
		m.Families, m.Spec, m.Clean, m.Layout = names, "", false, ""
	}
	switch {
	case set["naming"] && len(m.Families) > 0:
		m.Template = *namingSpec
	case set["naming"]:
		m.Spec, m.Layout, m.Template = *namingSpec, "", ""
	case set["layout"]:
		m.Spec, m.Layout, m.Template, m.Families = "", *layout, "", nil
	}
	if set["trust-sparse"] {
		m.TrustSparse = *trust
	}
	if set["no-verify-copy"] {
		m.NoVerifyCopy = *noVerify
	}
	if set["no-fsync"] {
		m.NoFsync = *noFsync
	}
	if set["keep-dotfiles"] {
		m.Filters.KeepDotfiles = *keepDotfiles
	}
	if set["verify-uncached"] {
		m.VerifyUncached = *verifyUncached
	}
	if set["no-cache"] {
		m.NoCache = *noCache
	}
	if set["copy-j"] {
		m.Workers = *workers
	}
	var err error
	if m.Filters, err = cf.filters(m.Filters); err != nil {
		fatal(2, err)
	}
	if *printSettings {
		b, _ := json.MarshalIndent(m, "", "  ")
		fmt.Println(string(b))
		return
	}
	if len(m.Inputs) == 0 || m.Output == "" || fs.NArg() > 0 {
		fmt.Fprintln(os.Stderr, "usage: onefold merge -input A [-input B ...] -output O [-execute] [flags]   (or -settings FILE; onefold -h)")
		os.Exit(2)
	}
	printPerf := perfFlags()
	o, err := m.Options(*cf.allowMixed)
	if err != nil {
		fatal(2, mixedHint(err))
	}
	cf.hash(&o.Catalog)
	o.Execute, o.Heartbeat, o.Log = *execute, *heartbeat, report.Writer
	perf.Meta("catalogWorkers", o.Catalog.Workers)
	perf.Meta("copyWorkers", o.Workers)
	perf.Meta("tools", toolsSummary())
	res, err := merge.Run(o)
	if err != nil {
		printPerf()
		fatal(1, err)
	}
	mode := "DRY-RUN (nothing written; add -execute)"
	if *execute {
		mode = "EXECUTED"
	}
	fmt.Printf("\nmerge %s\n", mode)
	fmt.Printf("output:            %s\n", res.Output)
	fmt.Printf("rooms:             %s\n", strings.Join(res.Rooms, " | "))
	fmt.Printf("naming:            %s\n", o.Naming)
	fmt.Printf("files:             output %d, inputs %d (inputs skipped by filters/dotfiles: %d, empty 0-byte: %d)\n",
		res.OutputFiles, res.InputFiles, res.SkippedByRules, res.EmptyInputs)
	if len(res.Excluded) > 0 {
		fmt.Printf("excluded:          %s\n", excludedSummary(res.Excluded))
	}
	printPackages(res.Packages)
	fmt.Printf("distinct contents: %d\n", res.Distinct)
	fmt.Printf("already in output: %d input files (not copied)\n", res.AlreadyPresent)
	fmt.Printf("input duplicates:  %d (same content as another input file, copied once)\n", res.InputDupes)
	fmt.Printf("to copy:           %d files, %s (%d renamed _NN for name collisions)\n", len(res.Copies), report.HumanBytes(res.CopyBytes), res.Renamed)
	fmt.Printf("output duplicates: %d groups, %d extra files (reported only, never deleted)\n", res.OutputDupGroups, res.OutputDupFiles)
	if !o.TrustSparse {
		fmt.Printf("verify-full:       %d duplicate groups confirmed on full content, %d split, %d unreadable kept apart\n",
			res.Verify.Groups, res.Verify.Splits, res.Verify.Unreadable)
	}
	if len(res.Blocked) > 0 {
		fmt.Printf("BLOCKED:           %d (a parent directory exists as a FILE in the output)\n", len(res.Blocked))
		for _, b := range res.Blocked {
			fmt.Printf("  %s\n", b)
		}
	}
	if *execute {
		fmt.Printf("copied:            %d files, %s   stale temps removed: %d\n", res.Copied, report.HumanBytes(res.CopiedBytes), res.StaleTemps)
		for _, f := range res.Failed {
			fmt.Printf("  FAILED %s\n", f)
		}
		if res.CachedVerify > 0 {
			fmt.Printf("verified through the OS cache: %d (the output's file system refused uncached reads)\n", res.CachedVerify)
		}
		fmt.Printf("missing after run: %d distinct contents\n", res.Missing())
		hint := "write -report FILE, then onefold purge-sources -from FILE deletes them"
		if *reportp != "" {
			hint = "onefold purge-sources -from " + *reportp + " deletes them"
		}
		fmt.Printf("settled:           %d input files are now in the output (%s)\n", len(res.Settled), hint)
	}
	if *list {
		for _, c := range res.Copies {
			fmt.Printf("COPY\t%s\t-> %s\t%d\n", c.SrcPath, c.Rel, c.Size)
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
	if res.Missing() > 0 {
		os.Exit(1)
	}
}

// printPackages flags the folders that look like driver or installer
// packages with the flags that leave them out (the first few).
func printPackages(ps []catalog.Package) {
	if len(ps) == 0 {
		return
	}
	n, files := map[string]int{}, 0
	for _, p := range ps {
		n[p.Kind]++
		files += p.Files
	}
	fmt.Printf("packages:          %d folders look like driver (%d) or installer (%d) packages, %d files in the plan; to leave them out:\n",
		len(ps), n["driver"], n["installer"], files)
	for i, p := range ps {
		if i == 5 {
			fmt.Printf("  ... (-list prints them all)\n")
			break
		}
		fmt.Printf("  -exclude-path '%s'\n", strings.ReplaceAll(p.Dir, "'", "''"))
	}
}

// listPackages prints PKG lines for -list.
func listPackages(ps []catalog.Package) {
	for _, p := range ps {
		fmt.Printf("PKG\t%s\t%s\t%d\n", p.Kind, p.Dir, p.Files)
	}
}

// excludedSummary counts the excluded files and directories by reason.
func excludedSummary(ex []catalog.Skip) string {
	n := map[string]int{}
	for _, x := range ex {
		k := x.Rule
		if x.Dir {
			k += " (dirs)"
		}
		n[k]++
	}
	keys := make([]string, 0, len(n))
	for k := range n {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s %d", k, n[k])
	}
	return strings.Join(parts, ", ")
}

func toolsSummary() string {
	var have []string
	for _, n := range tools.Names() {
		if tools.Has(n) {
			have = append(have, n)
		}
	}
	if len(have) == 0 {
		return "none (pure-Go only)"
	}
	return strings.Join(have, ",")
}

// cmdDoctor prints platform, workers and external tool availability.
func cmdDoctor() {
	fmt.Printf("%s  NumCPU=%d\n", versionLine(), runtime.NumCPU())
	c, err := config.Load()
	if err != nil {
		fmt.Printf("config: %v\n", err)
	}
	if l, src, err := meta.ResolveZone(c.Timezone); err != nil {
		fmt.Printf("time zone: %v\n", err)
	} else {
		fmt.Printf("time zone: %s (from %s)\n", l, src)
	}
	if tools.Disabled() {
		fmt.Println("external tools: DISABLED (ONEFOLD_NO_EXTERNAL=1)")
	}
	r := tools.Report()
	for _, n := range tools.Names() {
		p := r[n]
		if p == "" {
			p = "-- missing (pure-Go fallback)"
		}
		fmt.Printf("  %-10s %s\n", n, p)
	}
	fmt.Println("pure-Go: jpeg/heic/tiff/raw/png EXIF, mp4/mov/3gp/m4a, pdf, docx/xlsx/pptx, odt/ods/odp, rtf, doc/xls/ppt, epub, eps, zip, tar, 7z, rar, iso, audio tags")
	fmt.Println("needs a tool: photo/video silvering (exiftool, ffmpeg), mkv/avi/wmv dates (ffprobe), pdf/doc text export (pdftotext, antiword)")
}

// cmdNaming lists presets (with an example rendering), placeholders and grammar.
func cmdNaming() {
	fmt.Println(`naming grammar:
  literal           copied as is
  {name}            placeholder ("" if unknown)
  {a|b|"lit"}       first non-empty of a, b, or the literal
  <A|B>             first alternative whose placeholders are all non-empty (else nothing)
  / or \            path separator; empty segments are dropped; segments sanitized for Windows
  sel[,sel]=tpl;...;*=tpl     per-selector templates; a selector is a harmonized extension (.pdf, .jpg),
                              a type-registry group (uppercase) or a kind (lowercase);
                              precedence: extension > group > kind > *
collisions: always onefold _NN (stem_01.ext, stem_02.ext ...)`)
	fmt.Println("kinds: ", strings.Join(kinds.Kinds(), " "))
	fmt.Println("groups:", strings.Join(kinds.Groups(), " "))
	fmt.Printf("\npresets (append %q to any preset to use {stem-clean} instead of {stem-norm}):\n", naming.CleanSuffix)
	for _, n := range naming.PresetNames() {
		fmt.Printf("  %-26s %s\n", n, naming.Presets[n].Doc)
	}
	fmt.Println("\nlayout aliases (-layout):")
	for _, a := range plan.Layouts {
		fmt.Printf("  %-26s %s\n", a, naming.LayoutAliases[a])
	}
	fmt.Println("\nplaceholders:")
	for _, p := range naming.Placeholders {
		fmt.Printf("  {%s}  %s\n", p[0], p[1])
	}
}

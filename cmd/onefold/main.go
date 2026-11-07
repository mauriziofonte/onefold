// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command onefold: catalog -> plan -> apply (+ export-text, push), merge,
// rearrange. Layout and invariants: LLMS.md at the repository root.
package main

//go:generate go run gen_winres.go

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/config"
	"github.com/mauriziofonte/onefold/internal/fsx"
	"github.com/mauriziofonte/onefold/internal/hashcache"
	"github.com/mauriziofonte/onefold/internal/materialize"
	"github.com/mauriziofonte/onefold/internal/meta"
	"github.com/mauriziofonte/onefold/internal/plan"
	"github.com/mauriziofonte/onefold/internal/planreport"
	"github.com/mauriziofonte/onefold/internal/power"
	"github.com/mauriziofonte/onefold/internal/remote"
	"github.com/mauriziofonte/onefold/internal/report"
	"github.com/mauriziofonte/onefold/internal/scan"
	"github.com/mauriziofonte/onefold/internal/sparsehash"
	"github.com/mauriziofonte/onefold/internal/textexport"
)

// version is injected at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	// the diagnostic log (Windows) follows the user's config; ONEFOLD_DIAG=1
	// forces it when the config itself is the problem
	if c, err := config.Load(); (err == nil && c.DiagLog) || os.Getenv("ONEFOLD_DIAG") == "1" {
		openDiag()
	}
	dlog("start version=%s args=%q %s", version, os.Args[1:], launchInfo())
	if len(os.Args) < 2 {
		own, shell, app := ownsConsole(), parentIsExplorer(), startedAsApp()
		if own || shell || app {
			// double-clicked in Explorer (or a shortcut), or onefold.app
			// opened on macOS: nobody reads a terminal, open the GUI.
			// ownsConsole alone misses the case of Windows Terminal hosting
			// the console.
			dlog("launch: GUI (ownsConsole=%t parentExplorer=%t appBundle=%t)", own, shell, app)
			if app {
				fmt.Fprintln(os.Stderr, "onefold: started as onefold.app with no command: opening the GUI (onefold help lists the commands)")
			}
			detachConsole()
			runGUI(guiOptions{})
			return
		}
		dlog("launch: no arguments from a terminal -> usage")
		usage()
		os.Exit(2)
	}
	// merge shorthand: onefold --input A --input B --output A
	if strings.HasPrefix(os.Args[1], "-") && (hasFlag(os.Args[1:], "input") || hasFlag(os.Args[1:], "output")) {
		requireZone()
		cmdMerge(os.Args[1:])
		return
	}
	if timedCommands[os.Args[1]] {
		requireZone()
	}
	if longCommands[os.Args[1]] || strings.HasPrefix(os.Args[1], "-") {
		powerFromConfig()
	}
	switch os.Args[1] {
	case "merge":
		cmdMerge(os.Args[2:])
	case "gui", "--gui":
		cmdGUI(os.Args[2:])
	case "doctor":
		cmdDoctor()
	case "tools":
		cmdTools(os.Args[2:])
	case "naming":
		cmdNaming()
	case "config":
		cmdConfig(os.Args[2:])
	case "rearrange":
		cmdRearrange(os.Args[2:])
	case "purge-sources":
		cmdPurgeSources(os.Args[2:])
	case "hash":
		cmdHash(os.Args[2:])
	case "index":
		cmdIndex(os.Args[2:])
	case "catalog":
		cmdCatalog(os.Args[2:])
	case "plan":
		cmdPlan(os.Args[2:])
	case "golden-index":
		cmdGoldenIndex(os.Args[2:])
	case "match":
		cmdMatch(os.Args[2:])
	case "similar":
		cmdSimilar(os.Args[2:])
	case "reclaim":
		cmdReclaim(os.Args[2:])
	case "backup":
		cmdBackup(os.Args[2:])
	case "seed":
		cmdSeed(os.Args[2:])
	case "apply":
		cmdApply(os.Args[2:])
	case "push":
		cmdPush(os.Args[2:])
	case "export-text":
		cmdExportText(os.Args[2:])
	case "stats":
		cmdStats(os.Args[2:])
	case "dedupe":
		cmdDedupe(os.Args[2:])
	case "schedule":
		cmdSchedule(os.Args[2:])
	case "version", "--version", "-version", "-V":
		fmt.Println(versionLine())
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

// timedCommands read or write canonical times: they need the user's config
// applied and a known zone.
var timedCommands = map[string]bool{"merge": true, "rearrange": true, "catalog": true, "plan": true, "golden-index": true, "match": true, "similar": true, "reclaim": true, "seed": true, "apply": true, "export-text": true}

// longCommands may work for hours: they hold a sleep lock (config
// keep-awake, on by default) and take the configured priority. The GUI does
// the same per job.
var longCommands = map[string]bool{"merge": true, "rearrange": true, "purge-sources": true, "catalog": true, "plan": true,
	"golden-index": true, "match": true, "similar": true, "reclaim": true, "backup": true, "seed": true, "apply": true,
	"export-text": true, "push": true, "stats": true, "dedupe": true}

// powerFromConfig takes the sleep lock and the priority the config asks for;
// both end with the process. Failures only warn: the run goes on.
func powerFromConfig() {
	c, err := config.Load()
	if err != nil {
		return // requireZone, or the command, reports a broken config
	}
	if !c.NoKeepAwake {
		if _, err := power.KeepAwake(power.Reason); err != nil {
			dlog("keep awake: %v", err)
		}
	}
	if c.LowPriority {
		if err := power.SetLowPriority(true); err != nil {
			fmt.Fprintf(os.Stderr, "low priority: %v\n", err)
		}
	}
}

// requireZone applies the user's config (zone, date order, month names) and
// stops when the config is broken or no zone is known.
func requireZone() {
	c, err := config.Load()
	if err != nil {
		fatal(2, fmt.Errorf("config: %w (fix it with `onefold config` or remove the file)", err))
	}
	if err := c.Apply(); err != nil {
		fatal(2, err)
	}
}

// hasFlag reports whether args contain -name / --name (with or without =value).
func hasFlag(args []string, name string) bool {
	for _, a := range args {
		a = strings.TrimLeft(a, "-")
		if a == name || strings.HasPrefix(a, name+"=") {
			return true
		}
	}
	return false
}

func usage() {
	fmt.Fprintf(os.Stderr, "onefold %s - dedup + canonical golden tree for photos, videos and generic files\n", version)
	fmt.Fprint(os.Stderr, `
usage:
  onefold merge -input A [-input B ...] -output O [-execute]   fold inputs into O (O may be an input); dry-run by default
  onefold -input A -input B -output A [-execute]              same (merge shorthand)
  onefold rearrange [flags] <root> | -resume J | -undo J   rename a tree in place, delete confirmed dupes; dry-run by default
  onefold purge-sources -from REPORT [-execute -expect N] [-trash]   delete merged sources whose content is in the output
  onefold naming                            naming presets, layouts and placeholders
  onefold config [list|get K|set K V|unset K|path]   owner, signature, language, timezone, date-order, names-language, diag-log (per user)
  onefold gui [-port N] [-no-window] [-no-open]   local GUI (also: double-click the exe on Windows)
  onefold doctor                            platform + external tool availability
  onefold tools [list | install all|ID...]  download pinned external tools (Windows x64)
  onefold catalog [flags] <room>...        index rooms -> manifest JSONL (read-only)
  onefold plan [flags] <manifest>...        dedup + winners -> plan JSONL (read-only)
  onefold apply [flags] <plan.jsonl>        dry-run (default) or --execute into --target
  onefold golden-index -o INDEX <plan>...   index the golden tree from the plans applied to it
  onefold match -index INDEX -o SUB <m>...  plan new files against the tree: only the new ones (then apply SUB)
  onefold similar [-max-distance N] <plan>  look-alike golden photos for a manual review (drops nothing)
  onefold reclaim -target G -script F <plan> verify the exact-duplicate drops, write a deletion script (deletes nothing)
  onefold backup [-execute] SRC DST         cold-storage copy (same paths, only adds, verified, content manifest)
  onefold backup -check DST                 audit a cold copy: every file read again against its manifests
  onefold backup -check -tree G DST         read the tree too against the hashes the backups recorded
  onefold backup -restore -tree G [-execute -expect N] DST   put back damaged or missing files from the healthy side
  onefold seed -from S -to D -o F <plan>    bulk-copy script (rsync or robocopy) of a materialized tree (runs nothing)
  onefold export-text --target D <plan>     .txt sidecars for document/text assets
  onefold push --host H --user U [--key K] <local> <remote>   SFTP upload (idempotent; ssh-agent without --key)
  onefold stats [flags] <folder|volume>... what a disk holds: families, extensions, years, folders, duplicates (read-only)
  onefold stats -mounts                    list the volumes
  onefold dedupe -action trash|link|clone|script [-execute -expect N] <folder>...   clean up duplicate copies: one stays
  onefold dedupe -resume JOURNAL           finish or undo an interrupted dedupe
  onefold schedule add -name N -every daily|weekly|hourly [-at HH:MM] -- COMMAND...   run a command on a timetable (systemd timer, launchd, Task Scheduler)
  onefold schedule list | log -name N | remove -name N   the scheduled jobs, the output of their runs
  onefold hash <file> | index <dir> | version

catalog: -o FILE  -j N  -blocks N  -preset media|all|common|image|audio|video|document|archive (CSV, default media)
         -ext CSV (replaces presets)  -category CSV  -exclude CSV (relative substrings)  -prune CSV  -prune-from FILE
         skip rules, all ON by default: system-dirs cloud-dirs dev-dirs dot-dirs junk system-ext unknown-ext noext
         transient shortcuts big (50G); -keep RULES turns some off, -skip RULES on (CSV or all); also -skip-x=false,
         -max-size 0, -keep-dot-dirs, -keep-junk; -no-sweep = every rule off except dot-dirs and junk
         -min-size SIZE  -limit N  -host LABEL  -reuse OLD.jsonl  -quick
plan:    -o FILE  -layout dedupe|timeline|document|music|device|<preset>|<template>  -verify-full  -fix-ext  -video-tier  -video-tolerance SEC  -filename-seq  -self-contained=false
         -artist NAME|-  -report FILE(.json|.md)  -extensive
apply:   -with-stats  -execute -target DIR  -only-host LABEL  -j N  -signature TEXT  -lang ISO639-2  -heartbeat D  -allow-unstamped  -link
         -allow-unstamped (start without exiftool/ffmpeg: photos/videos copied without canonical metadata)
merge:   -input (repeat) -output -execute -naming N (or -layout) -list -report FILE
         -name FAMILY=PRESET[+clean|+raw|+prefix[:N]] (repeat; families img video doc music arch rest, all): the
         families select the file types and name each family; +prefix:N puts "[folder N levels up] " before the name
         -settings FILE (JSON, GUI export; flags override it)  -print-settings
         -trust-sparse -no-verify-copy -no-fsync -no-cache -copy-j N + every catalog filter flag (default -preset all)
rearrange: -execute -naming N | -name ... -settings FILE -dupes delete|report -keep-empty-dirs -no-cache -move-j N -list -report FILE
purge-sources: -from MERGE-REPORT -list -execute -expect N (the dry run's count) -trash (Recycle Bin on Windows local fixed drives, Trash on macOS and Linux) -j N
stats:   -dups off|fast|full  -dup-min SIZE  -one-volume=false  -depth N  -top N  -json FILE|-  -j N
dedupe:  -keep oldest|newest|shortest|first  -keep-in DIR  -script FILE  -ps  -min-size SIZE  -list  -json FILE|-  -j N
all:     -perf (telemetry table)  -perf-out FILE.json

Run 'onefold <command> -h' for per-command flag help.
`)
}

func cmdHash(args []string) {
	fs := flag.NewFlagSet("hash", flag.ExitOnError)
	cov := fs.Int("cov", sparsehash.DefaultCoverage, "coverage percent")
	s := fs.Int("s", sparsehash.DefaultSampleSize, "sample block size (bytes)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "hash: exactly one <file> required")
		os.Exit(2)
	}
	start := time.Now()
	res, err := sparsehash.HashFile(fs.Arg(0), *cov, *s)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	mode := "sparse"
	if res.Full {
		mode = "full"
	}
	fmt.Printf("%s  %s  size=%d sampled=%d (%.2f%%)  %s\n",
		res.Hex, mode, res.Size, res.SampledBytes,
		pct(res.SampledBytes, res.Size), time.Since(start).Round(time.Microsecond))
}

func cmdIndex(args []string) {
	fs := flag.NewFlagSet("index", flag.ExitOnError)
	j := fs.Int("j", 8, "parallel workers")
	cov := fs.Int("cov", sparsehash.DefaultCoverage, "coverage percent")
	s := fs.Int("s", sparsehash.DefaultSampleSize, "sample block size (bytes)")
	all := fs.Bool("all", false, "hash every file, not just media extensions")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "index: exactly one <dir> required")
		os.Exit(2)
	}
	root := fs.Arg(0)

	// nil => match every file (-all); otherwise the media whitelist.
	exts := scan.DefaultMediaExts
	if *all {
		exts = nil
	}

	fmt.Fprintf(os.Stderr, "indexing %s with %d workers (%d cores)…\n", root, *j, runtime.NumCPU())
	st, err := scan.Index(root, *j, *cov, *s, exts, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	secs := st.Elapsed.Seconds()
	fmt.Printf("files:        %d (sparse %d, full %d)\n", st.Files, st.Sparse, st.Full)
	fmt.Printf("skipped:      %d   errors: %d\n", st.Skipped, st.Errors)
	fmt.Printf("logical:      %s\n", report.HumanBytes(st.LogicalBytes))
	fmt.Printf("read (I/O):   %s  (%.2f%% of logical — the sparse win)\n",
		report.HumanBytes(st.SampledBytes), pct(st.SampledBytes, st.LogicalBytes))
	fmt.Printf("wall:         %s\n", st.Elapsed.Round(time.Millisecond))
	if secs > 0 {
		fmt.Printf("throughput:   %.0f files/s   %s/s logical   %s/s read\n",
			float64(st.Files)/secs, report.HumanBytes(int64(float64(st.LogicalBytes)/secs)),
			report.HumanBytes(int64(float64(st.SampledBytes)/secs)))
	}
}

func cmdCatalog(args []string) {
	fs := flag.NewFlagSet("catalog", flag.ExitOnError)
	outp := fs.String("o", "manifest.jsonl", "output manifest (JSONL)")
	host := fs.String("host", "", "host label stored in every record (routes apply --only-host; default: this machine's name)")
	reuse := fs.String("reuse", "", "previous manifest: reuse records whose path+size+mtime are unchanged")
	heartbeat := fs.Duration("heartbeat", report.DefaultHeartbeat, "progress log interval when output is not a terminal")
	cf := addCatalogFlags(fs, "media", runtime.NumCPU(), regimeGolden)
	perfFlags := addPerfFlags(fs)
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "catalog: at least one <room> required")
		os.Exit(2)
	}
	defer perfFlags()()
	rooms := fs.Args()
	o, err := cf.resolve()
	if err != nil {
		fatal(2, err)
	}
	o.Out, o.Host = *outp, *host
	j := &o.Workers
	if *reuse != "" {
		if o.Reuse, err = catalog.LoadManifestByPath(*reuse); err != nil {
			fatal(1, fmt.Errorf("reuse manifest: %w", err))
		}
	}

	fmt.Fprintf(os.Stderr, "cataloging %d room(s) with %d workers (READ-ONLY)…\n", len(rooms), *j)
	prog := report.New(report.Options{Label: "catalog", Unit: report.Items, Growing: true, Heartbeat: *heartbeat})
	o.Progress = prog
	o.Cache = hashcache.Open()
	sum, err := catalog.Build(rooms, o)
	o.Cache.Close()
	prog.Stop()
	if err != nil {
		fatal(1, err)
	}

	secs := sum.Elapsed.Seconds()
	fmt.Printf("\nmanifest:     %s\n", *outp)
	fmt.Printf("assets:       %d (photos %d, videos %d)   skipped %d, errors %d\n", sum.Assets, sum.Photos, sum.Videos, sum.Skipped, sum.Errors)
	if sum.OnlineOnly > 0 {
		fmt.Printf("online-only:  %d files, %s in the cloud, not read (they would be downloaded; -keep online-only reads them)\n",
			sum.OnlineOnly, report.HumanBytes(sum.OnlineOnlyBytes))
	}
	if sum.Limited > 0 || sum.Reused > 0 {
		fmt.Printf("limited:      %d   reused: %d\n", sum.Limited, sum.Reused)
	}
	fmt.Printf("by kind:     ")
	for _, k := range sortedKeys(sum.ByKind) {
		fmt.Printf(" %s %d", k, sum.ByKind[k])
	}
	fmt.Println()
	fmt.Printf("time source:  metadata %d | filename %d | takeout %d | filemtime %d | NONE %d\n", sum.FromMetadata, sum.FromFilename, sum.FromTakeout, sum.FromMtime, sum.NoTime)
	fmt.Printf("sub-second:   %d photos   |   dims missing: %d\n", sum.SubSec, sum.DimsMissing)
	if sum.WithPlace > 0 {
		fmt.Printf("places:       %d photos and videos know where they were taken\n", sum.WithPlace)
	}
	fmt.Printf("logical:      %s   wall: %s", report.HumanBytes(sum.LogicalBytes), sum.Elapsed.Round(time.Millisecond))
	if secs > 0 {
		fmt.Printf("   (%.0f assets/s)", float64(sum.Assets)/secs)
	}
	fmt.Println()

	fmt.Printf("\nby device (top 15):\n")
	for i, k := range sum.TopDevices() {
		if i >= 15 {
			fmt.Printf("  … (%d more)\n", len(sum.ByDevice)-15)
			break
		}
		fmt.Printf("  %-24s %d\n", k, sum.ByDevice[k])
	}

	fmt.Printf("\nby year:\n")
	for _, y := range sortedKeys(sum.ByYear) {
		fmt.Printf("  %-8s %d\n", y, sum.ByYear[y])
	}

	fmt.Printf("\nsample assets (canonical name preview):\n")
	for _, r := range sum.Samples {
		name := r.Meta.Prefix + "_" + compact(r.Meta.Time)
		fmt.Printf("  %-28s kind=%-8s dev=%-16s src=%-8s %dx%d\n", name, r.Meta.Kind, dash(r.Meta.Device), r.Meta.TimeSource, r.Meta.Width, r.Meta.Height)
	}
}

// resolveExts builds the extension whitelist: -ext replaces presets; -category
// adds; preset "all" (without -ext) means no whitelist (nil).
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func fatal(code int, err error) {
	dlog("fatal (exit %d): %v", code, err)
	fmt.Fprintln(os.Stderr, "error:", err)
	meta.CloseExifReaders()
	os.Exit(code)
}

func cmdPlan(args []string) {
	fs := flag.NewFlagSet("plan", flag.ExitOnError)
	outp := fs.String("o", "", "write the materialization plan (JSONL of actions) to this file")
	layout := fs.String("layout", "dedupe", "generic-kind naming: "+strings.Join(plan.Layouts, "|")+", or any preset/template (`onefold naming`)")
	verify := fs.Bool("verify-full", false, "full-content hash of every duplicate group before dropping (reads sources)")
	fixExt := fs.Bool("fix-ext", false, "generic kinds: rename to the extension implied by the content when unambiguous")
	videoTier := fs.Bool("video-tier", false, "videos under VIDEOS/<YEAR>/<4K|1080p|720p|480p|SD|unknown>/")
	videoTol := fs.Float64("video-tolerance", 0, "seconds: videos of one start time whose durations differ by at most this much are one asset (0 = the rounded-duration rule)")
	filenameSeq := fs.Bool("filename-seq", false, "photos/videos dated by a file name without a time of day keep its counter (IMG_20200101_100000_WA0007.jpg) instead of _NN")
	selfContained := fs.Bool("self-contained", true, "write each winner's sub-second, duration and dimensions into the plan (=false: the historical action fields only)")
	artist := fs.String("artist", "", "owner stamped as Artist/Copyright on photo/video copies (default: `onefold config` artist, none if unset; \""+config.None+"\" = none)")
	reportp := fs.String("report", "", "write a report (.md = Markdown, else JSON)")
	extensive := fs.Bool("extensive", false, "report: all duplicate groups/mismatches + full golden file list")
	sidecars := fs.Bool("sidecars", false, "keep the files that belong to a photo or video with it: .AAE, .xmp, .THM, Google Takeout .json (catalog -sidecars) and the video of a Live Photo are copied next to its golden file under its name")
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		fmt.Fprintln(os.Stderr, "plan: at least one <manifest.jsonl> required")
		os.Exit(2)
	}
	if !plan.ValidLayout(*layout) {
		fatal(2, fmt.Errorf("unknown layout %q (valid: %s, or a naming preset/template)", *layout, strings.Join(plan.Layouts, ", ")))
	}
	cfg := loadConfig()
	opts := plan.Options{VerifyFull: *verify, Layout: *layout, FixExt: *fixExt, VideoTier: *videoTier, VideoTolerance: *videoTol, FilenameSeq: *filenameSeq, SelfContained: *selfContained,
		Artist: config.Override(*artist, cfg.Artist), Sidecars: *sidecars}
	recs, err := plan.LoadManifests(fs.Args())
	if err != nil {
		fatal(1, err)
	}
	fmt.Fprintf(os.Stderr, "planning %d records from %d manifest(s) (read-only)…\n", len(recs), fs.NArg())
	r := plan.PlanWith(recs, opts)

	// Artifacts first: a closed stdout pipe (| head) must not lose the plan.
	acts := plan.BuildActionsWith(r, opts)
	if *outp != "" {
		f, err := os.Create(*outp)
		if err != nil {
			fatal(1, err)
		}
		if err := plan.Write(f, acts); err != nil {
			fatal(1, fmt.Errorf("write plan %s: %w", *outp, err))
		}
		if err := f.Close(); err != nil {
			fatal(1, fmt.Errorf("write plan %s: %w", *outp, err))
		}
		fmt.Fprintf(os.Stderr, "\nmaterialization plan: %s (%d actions)\n", *outp, len(acts))
	}
	if *reportp != "" {
		rep := planreport.Build(fs.Args(), recs, r, acts, opts, *extensive)
		if err := planreport.Write(rep, *reportp); err != nil {
			fatal(1, err)
		}
		fmt.Fprintf(os.Stderr, "report: %s\n", *reportp)
	}

	fmt.Printf("\ninput files:   %d\n", r.InputFiles)
	fmt.Printf("GOLDEN assets: %d   (%s kept)\n", r.Assets, report.HumanBytes(r.GoldenBytes))
	fmt.Printf("dropped:       exact-dupe %d | lower-res/LQ %d | smaller-variant %d\n", r.ExactDupes, r.LowerRes, r.SmallerVar)
	fmt.Printf("reclaimable:   %s\n", report.HumanBytes(r.ReclaimBytes))
	fmt.Printf("hash-only assets (photos w/o subsec + timeless + generic): %d\n", r.HashOnly)
	if r.SidecarCount > 0 || r.SidecarsAlone > 0 {
		fmt.Printf("sidecars:      %d kept with their photo or video, %d belong to none here (left out)\n", r.SidecarCount, r.SidecarsAlone)
	}
	if r.SidecarsLeftOut > 0 {
		fmt.Printf("sidecars:      %d left out (cataloged with -sidecars): plan -sidecars keeps them with their photos\n", r.SidecarsLeftOut)
	}
	if *verify {
		fmt.Printf("verify-full:   %d groups checked, %d split (content differed), %d unreadable members kept\n",
			r.VerifiedGroups, r.VerifySplits, r.VerifyUnreadable)
	}

	if len(r.MultiExample) > 0 {
		fmt.Printf("\nexample clusters (winner ✓ + dropped):\n")
		for _, a := range r.MultiExample {
			fmt.Printf("  ✓ %-40s dev=%-16s %dx%d %s\n", filepath.Base(a.Winner.Path), dash(a.Winner.Meta.Device), a.Winner.Meta.Width, a.Winner.Meta.Height, report.HumanBytes(a.Winner.Size))
			for _, m := range a.Members[1:] {
				fmt.Printf("    ✗ %-40s %dx%d %-10s [%s]\n", filepath.Base(m.Path), m.Meta.Width, m.Meta.Height, report.HumanBytes(m.Size), a.Reasons[m.Host+"\x00"+m.Path])
			}
		}
	} else {
		fmt.Printf("\n(no multi-member clusters: every asset is already unique in this input)\n")
	}

}

func cmdApply(args []string) {
	fs := flag.NewFlagSet("apply", flag.ExitOnError)
	stats := fs.Bool("with-stats", false, "also print how much data must be copied to the materialized target")
	execute := fs.Bool("execute", false, "ACTUALLY materialize (default is dry-run: nothing is written)")
	target := fs.String("target", "", "golden tree root to materialize into (required with --execute)")
	onlyHost := fs.String("only-host", "", "process only actions whose srcHost equals this label")
	heartbeat := fs.Duration("heartbeat", report.DefaultHeartbeat, "progress log interval when output is not a terminal")
	workers := fs.Int("j", 0, "parallel photo/generic workers (0 = auto = NumCPU); videos are always serial (SMR sequential read)")
	signature := fs.String("signature", "", "comment stamped on photo/video copies (default: `onefold config` signature, else \""+config.DefaultSignature(version)+"\"; \""+config.None+"\" = none)")
	lang := fs.String("lang", "", "ISO 639-2 language stamped on the first video/audio stream of video copies (default: `onefold config` language, none if unset; \""+config.None+"\" = none)")
	allowUnstamped := fs.Bool("allow-unstamped", false, "with --execute: publish photos/videos without canonical metadata when exiftool/ffmpeg are missing (default: refuse to start)")
	clone := fs.Bool("clone", false, "with --execute: start each copy as a clone of its source where the file system allows it (same volume on APFS, Btrfs, XFS...): no extra space for byte-copied files; photos are then stamped on the clone, the source is never touched")
	link := fs.Bool("link", false, "with --execute: publish each winner as a hard link of its source (original bytes, no copy, no canonical metadata, mtime untouched); target and sources on the same volume. Golden and source share their bytes: edit neither")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "apply: exactly one <plan.jsonl> required")
		os.Exit(2)
	}
	if !isFile(fs.Arg(0)) {
		fatal(1, fmt.Errorf("plan %q not found", fs.Arg(0)))
	}
	if *execute && *target == "" {
		fmt.Fprintln(os.Stderr, "apply --execute requires --target <dir>")
		os.Exit(2)
	}

	acts, err := plan.Read(fs.Arg(0))
	if err != nil {
		fatal(1, err)
	}

	if *execute {
		cfg := loadConfig()
		silver := materialize.Silver{Signature: config.Override(*signature, cfg.EffectiveSignature(version)),
			Language: config.Override(*lang, cfg.Language)}
		if err := (config.Config{Language: silver.Language}).Validate(); err != nil {
			fatal(2, err)
		}
		if *clone && *link {
			fatal(2, errors.New("apply: -clone and -link exclude each other"))
		}
		cmdApplyExecute(acts, *target, *onlyHost, *heartbeat, *workers, silver, *allowUnstamped, *link, *clone)
		return
	}

	// DRY-RUN: parsable textual action list (one asset per stanza, prefixed tags).
	var toCopy, reclaim int64
	byHost := map[string]int64{}
	byTargetTop := map[string]int{}
	nDrops := 0
	for _, a := range acts {
		mode := fmt.Sprintf("silver[artist=%q,datetime=%s,mtime=set]", a.Artist, a.Time)
		if *link {
			mode = "link[original bytes,mtime=source]"
		}
		fmt.Printf("GOLDEN\t%s\t%s:%s\t-> %s\ttime=%s\tdev=%s\tedit=%t\thdr=%t\t%s\n",
			a.Kind, a.SrcHost, a.SrcPath, a.CanonRel, a.Time, dash(a.Device), a.IsEdit, a.IsHDR, mode)
		for _, d := range a.Drops {
			fmt.Printf("  DROP\t%s\t%s:%s\t[%s]\t%s\n", a.Kind, d.Host, d.Path, d.Reason, report.HumanBytes(d.Size))
			reclaim += d.Size
			nDrops++
		}
		for _, sc := range a.Sidecars {
			fmt.Printf("  SIDECAR\t%s:%s\t-> %s\t%s\n", a.SrcHost, sc.Path, sc.Rel, report.HumanBytes(sc.Size))
			toCopy += sc.Size
		}
		toCopy += a.SrcSize
		byHost[a.SrcHost] += a.SrcSize
		top := a.CanonRel
		if i := indexByte(top, '/'); i > 0 {
			top = top[:i]
		}
		byTargetTop[top]++
	}

	fmt.Printf("\n# DRY-RUN (nothing written). %d golden actions, %d drops.\n", len(acts), nDrops)
	if *stats {
		fmt.Printf("\nSTATS:\n")
		fmt.Printf("  golden to materialize: %d assets, %s\n", len(acts), report.HumanBytes(toCopy))
		for _, h := range sortedKeys(byHost) {
			fmt.Printf("    from source %-8s %s\n", h, report.HumanBytes(byHost[h]))
		}
		fmt.Printf("  absorbed drops: %d (%s reclaimable)\n", nDrops, report.HumanBytes(reclaim))
		fmt.Printf("  per target tree:\n")
		for _, t := range sortedKeys(byTargetTop) {
			fmt.Printf("    %-10s %d asset\n", t, byTargetTop[t])
		}
	}
}

func cmdApplyExecute(acts []plan.Action, target, onlyHost string, heartbeat time.Duration, workers int, silver materialize.Silver, allowUnstamped, link, clone bool) {
	// Photos are exiftool/CPU-bound and scale with parallelism; videos are
	// ffmpeg + sequential reads where parallelism only causes seek contention:
	// materialize.ApplyAll runs photos in a pool, videos serially, both groups
	// concurrently.
	var sel []plan.Action
	var total int64
	filtered := 0
	for _, a := range acts {
		if onlyHost != "" && a.SrcHost != onlyHost {
			filtered++
			continue
		}
		total += a.SrcSize
		sel = append(sel, a)
	}
	// an unsilvered copy stays unsilvered (a later run skips existing files):
	// without the tools, start only when asked to
	if link {
		// hard links carry the original bytes: no tools needed, but the target
		// must accept links of the sources; checked before anything is written
		if err := materialize.CheckLink(sel, target); err != nil {
			fmt.Fprintf(os.Stderr, "apply -link: %v\n  -link needs the target on the same volume as the sources, on a filesystem with hard links (NTFS, APFS, ext4, btrfs...).\n  Choose a target on that volume, or run apply without -link to write copies.\n", err)
			os.Exit(2)
		}
	} else if missing := materialize.MissingTools(sel, target); len(missing) > 0 && !allowUnstamped {
		fmt.Fprintf(os.Stderr, "apply: %s not found: photos/videos would be written without canonical metadata for good.\n"+
			"  Install the tools (`onefold doctor`; Windows: `onefold tools install all`) or add -allow-unstamped.\n", strings.Join(missing, ", "))
		os.Exit(2)
	}
	rep := report.New(report.Options{Label: "materialize", TotalBytes: total, Heartbeat: heartbeat})
	st := materialize.ApplyAll(sel, target, materialize.RunOptions{Workers: workers, Progress: rep, Silver: silver, Link: link, Clone: clone})
	rep.Finish()
	fmt.Printf("  written %d, already present %d, failed %d\n", st.Written, st.Skipped, st.Failed)
	if st.Sidecars > 0 {
		fmt.Printf("  sidecars written next to their golden file: %d\n", st.Sidecars)
	}
	if clone {
		fmt.Printf("  cloned %d (the rest copied: another volume or a file system without clones)\n", st.Cloned)
	}
	if onlyHost != "" && filtered > 0 {
		fmt.Printf("  (other-host actions skipped: %d)\n", filtered)
	}
	if st.Unsilvered > 0 {
		fmt.Printf("  %d photo/video copies written WITHOUT canonical metadata (exiftool/ffmpeg missing or refusing them);\n"+
			"  they stay as they are: a later run skips existing files. `onefold doctor` lists the tools.\n", st.Unsilvered)
	}
	if st.Failed > 0 {
		fmt.Fprintf(os.Stderr, "apply: %d action(s) failed\n", st.Failed)
		os.Exit(1)
	}
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}

// cmdExportText writes .txt sidecars (header + extracted text) for the
// document/text actions of a plan. Runs where the sources live (--only-host).
func cmdExportText(args []string) {
	fs := flag.NewFlagSet("export-text", flag.ExitOnError)
	target := fs.String("target", "", "root of the text-export tree (required)")
	onlyHost := fs.String("only-host", "", "process only actions whose srcHost equals this label")
	workers := fs.Int("j", 0, "parallel workers (0 = NumCPU)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 || *target == "" {
		fmt.Fprintln(os.Stderr, "usage: onefold export-text --target DIR [--only-host H] [-j N] <plan.jsonl>")
		os.Exit(2)
	}
	acts, err := plan.Read(fs.Arg(0))
	if err != nil {
		fatal(1, err)
	}
	var todo []plan.Action
	for _, a := range acts {
		if textexport.Eligible(a) && (*onlyHost == "" || a.SrcHost == *onlyHost) {
			todo = append(todo, a)
		}
	}
	n := *workers
	if n <= 0 {
		n = runtime.NumCPU()
	}
	var (
		mu                            sync.Mutex
		wrote, skipped, empty, failed int
		wg                            sync.WaitGroup
		dirs                          fsx.DirSet
	)
	jobs := make(chan plan.Action)
	for w := 0; w < n; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for a := range jobs {
				r, err := textexport.Export(a, *target)
				mu.Lock()
				switch {
				case err != nil:
					failed++
					fmt.Fprintf(os.Stderr, "ERR %s: %v\n", a.SrcPath, err)
				case r.Skipped:
					skipped++
				default:
					wrote++
					dirs.Add(r.Dst)
					if r.Empty {
						empty++
					}
				}
				mu.Unlock()
			}
		}()
	}
	for _, a := range todo {
		jobs <- a
	}
	close(jobs)
	wg.Wait()
	if err := dirs.SyncAll(); err != nil {
		failed++
		fmt.Fprintf(os.Stderr, "ERR flushing the target folders to disk: %v\n", err)
	}
	meta.CloseExifReaders() // exiftool readers do not exit with onefold
	fmt.Printf("export-text: %d eligible, %d written (%d without extractable text), %d skipped (exist), %d errors\n",
		len(todo), wrote, empty, skipped, failed)
	if failed > 0 {
		os.Exit(1)
	}
}

// cmdPush syncs a local staging tree to a remote golden tree over pure-Go SFTP.
// It authenticates with a private key (passphrase prompted if the key is
// encrypted) and runs a connection test BEFORE transferring anything.
func cmdPush(args []string) {
	fs := flag.NewFlagSet("push", flag.ExitOnError)
	host := fs.String("host", "", "SFTP host")
	port := fs.Int("port", 22, "SFTP port")
	user := fs.String("user", "", "SSH user")
	key := fs.String("key", "", "private key path (passphrase prompted if encrypted); default: ssh-agent identities")
	noAgent := fs.Bool("no-agent", false, "do not use the running ssh-agent")
	kh := fs.String("known-hosts", "", "known_hosts path (default ~/.ssh/known_hosts)")
	insecure := fs.Bool("insecure-host-key", false, "skip host-key verification")
	retries := fs.Int("retries", 3, "reconnect and resume this many times after a transfer error")
	heartbeat := fs.Duration("heartbeat", report.DefaultHeartbeat, "progress log interval when output is not a terminal")
	fs.Parse(args)
	rest := fs.Args()
	if *host == "" || *user == "" || len(rest) != 2 {
		fmt.Fprintln(os.Stderr, "usage: onefold push --host H --user U [--key KEY | ssh-agent] [--port N] "+
			"[--known-hosts F | --insecure-host-key] [--retries N] <local-dir> <remote-dir>")
		os.Exit(2)
	}
	localDir, remoteDir := rest[0], rest[1]
	if fi, err := os.Stat(localDir); err != nil || !fi.IsDir() {
		fmt.Fprintf(os.Stderr, "local dir %q not accessible\n", localDir)
		os.Exit(1)
	}
	cfg := remote.Config{Host: *host, Port: *port, User: *user, KeyPath: *key, NoAgent: *noAgent, KnownHosts: *kh, Insecure: *insecure}
	connect := func() (*remote.Client, error) {
		report.Printf("connecting to %s@%s:%d…\n", *user, *host, *port)
		cl, err := remote.Dial(cfg)
		if err != nil {
			return nil, err
		}
		// connection test BEFORE any transfer
		if err := cl.TestConnection(remoteDir); err != nil {
			cl.Close()
			return nil, fmt.Errorf("connection test: %w", err)
		}
		return cl, nil
	}
	cl, err := connect()
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect failed: %v\n", err)
		os.Exit(1)
	}
	report.Printf("connection OK → pushing %s -> %s\n", localDir, remoteDir)

	total, _ := remote.TreeBytes(localDir)
	rep := report.New(report.Options{Label: "push", TotalBytes: total, Heartbeat: *heartbeat})
	onFile := func(rel string, n int64, dur time.Duration, skipped bool) {
		if skipped {
			rep.Advance(n)
		} else {
			rep.Item(n, dur)
		}
	}
	var st remote.Stats
	for attempt := 0; ; attempt++ {
		st, err = cl.UploadTree(localDir, remoteDir, onFile)
		cl.Close()
		if err == nil || attempt >= *retries {
			break
		}
		rep.Event(fmt.Sprintf("push interrupted (%v): reconnecting, attempt %d/%d", err, attempt+1, *retries))
		time.Sleep(time.Duration(attempt+1) * 5 * time.Second)
		if cl, err = connect(); err != nil {
			break
		}
		// completed files are skipped (same size): restart the count from them
		rep.Stop()
		rep = report.New(report.Options{Label: "push", TotalBytes: total, Heartbeat: *heartbeat})
	}
	if err != nil {
		rep.Err()
		rep.Event("push failed: " + err.Error())
	}
	rep.Finish()
	if len(st.Conflicts) > 0 {
		fmt.Fprintf(os.Stderr, "push: %d remote file(s) exist with a different size and were NOT overwritten:\n", len(st.Conflicts))
		for _, c := range st.Conflicts {
			fmt.Fprintf(os.Stderr, "  CONFLICT %s\n", c)
		}
	}
	if err != nil || len(st.Conflicts) > 0 {
		os.Exit(1)
	}
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func compact(t string) string {
	// "2006-01-02 15:04:05" -> "20060102_150405"
	if len(t) != 19 {
		return "????????_??????"
	}
	return t[0:4] + t[5:7] + t[8:10] + "_" + t[11:13] + t[14:16] + t[17:19]
}
func dash(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func pct(part, whole int64) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) / float64(whole) * 100
}

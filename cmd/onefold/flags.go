// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/kinds"
	"github.com/mauriziofonte/onefold/internal/naming"
	"github.com/mauriziofonte/onefold/internal/perf"
	"github.com/mauriziofonte/onefold/internal/sparsehash"
)

// Naming regimes of addCatalogFlags: which scope rule -allow-mixed lifts.
const (
	regimeGolden   = "golden"   // catalog: media kinds only
	regimeTemplate = "template" // merge, rearrange: never preset media
)

// catalogFlags are the enumeration/filter/hash flags shared by `catalog`,
// `merge` and `rearrange`.
type catalogFlags struct {
	fs                                                      *flag.FlagSet
	defPreset, regime                                       string
	allowMixed                                              *bool
	j, cov, s, blocks, limit                                *int
	presetCSV, extCSV, catCSV, exclCSV, pruneCSV, pruneFrom *string
	skipCSV, keepCSV, minSize, maxSize, wholeMax            *string
	noSweep, skipSysDirs, skipCloud, skipDev, skipSysExt    *bool
	skipUnknown, skipNoExt, keepDot, keepJunk, quick        *bool
	sidecars                                                *bool
	excludePaths, excludeGlobs                              *multiFlag
}

// addCatalogFlags registers the catalog flags; regime is the scope rule
// -allow-mixed lifts.
func addCatalogFlags(fs *flag.FlagSet, defPreset string, defJ int, regime string) *catalogFlags {
	var paths, globs multiFlag
	fs.Var(&paths, "exclude-path", "leave out this file or folder (all it holds), absolute or relative to the current dir (repeatable)")
	fs.Var(&globs, "exclude-glob", "leave out paths matching this pattern, relative to each folder: * ** ? [abc], / or \\; no separator = any name, e.g. *.data, **/DRIVERS/** (repeatable)")
	return &catalogFlags{fs: fs, defPreset: defPreset, regime: regime, excludePaths: &paths, excludeGlobs: &globs,
		allowMixed:  fs.Bool("allow-mixed", false, "allow settings that mix the golden archive with merge/rearrange (golden scope beyond photos and videos, preset media or golden tree presets in merge/rearrange)"),
		j:           fs.Int("j", defJ, "catalog workers (parallel walk + hash + extract)"),
		cov:         fs.Int("cov", sparsehash.DefaultCoverage, "sparse hash coverage percent"),
		s:           fs.Int("s", sparsehash.DefaultSampleSize, "sample block size (bytes)"),
		blocks:      fs.Int("blocks", sparsehash.MaxBlocks, "max sampled windows (4=head+tail only, cheapest on seek-slow disks)"),
		presetCSV:   fs.String("preset", defPreset, "extension presets, CSV union: "+strings.Join(kinds.PresetNames(), ", ")),
		extCSV:      fs.String("ext", "", "explicit extension whitelist (CSV); replaces -preset"),
		catCSV:      fs.String("category", "", "add every extension of these categories (CSV): "+strings.Join(kinds.Categories(), ", ")),
		exclCSV:     fs.String("exclude", "", "skip paths whose room-relative path contains any of these substrings (CSV)"),
		pruneCSV:    fs.String("prune", "", "prune directories with these names (CSV, case-insensitive, whole component)"),
		pruneFrom:   fs.String("prune-from", "", "file with one directory name per line to prune (# comments allowed)"),
		skipCSV:     fs.String("skip", "", "turn these skip rules on (CSV, or all): "+ruleNames()),
		keepCSV:     fs.String("keep", "", "turn these skip rules off (CSV, or all): keep what they would skip"),
		noSweep:     fs.Bool("no-sweep", false, "disable every default skip rule except dot-dirs and junk (system/cloud/dev dirs, system/unknown/no ext, transient, shortcuts, 50G cap)"),
		skipSysDirs: fs.Bool("skip-system-dirs", true, "rule system-dirs: prune Windows/system dirs (Windows, AppData, Program Files, $Recycle.Bin, Steam...)"),
		skipCloud:   fs.Bool("skip-cloud-dirs", true, "rule cloud-dirs: prune cloud sync dirs (Dropbox, OneDrive, Google Drive, iCloud Drive)"),
		skipDev:     fs.Bool("skip-dev-dirs", true, "rule dev-dirs: prune vendor, node_modules, tmp, tests"),
		skipSysExt:  fs.Bool("skip-system-ext", true, "rule system-ext: skip system extensions (exe, dll, sys, ini...) when no whitelist is active"),
		skipUnknown: fs.Bool("skip-unknown-ext", true, "rule unknown-ext: skip extensions unknown to the registry when no whitelist is active"),
		skipNoExt:   fs.Bool("skip-noext", true, "rule noext: skip files without extension"),
		minSize:     fs.String("min-size", "", "skip files smaller than SIZE (e.g. 2k)"),
		maxSize:     fs.String("max-size", catalog.DefaultMaxSize, "skip files larger than SIZE (0 = no cap)"),
		keepDot:     fs.Bool("keep-dot-dirs", false, "rule dot-dirs off: descend into directories starting with '.'"),
		keepJunk:    fs.Bool("keep-junk", false, "rule junk off: keep __MACOSX, .DS_Store, Thumbs.db, desktop.ini"),
		limit:       fs.Int("limit", 0, "max accepted files per room (0 = unlimited)"),
		wholeMax:    fs.String("whole-max", catalog.DefaultWholeMax, "read files up to SIZE once, sequentially, and hash/extract from memory (0 = seek-based sampling only)"),
		quick:       fs.Bool("quick", false, "generic kinds: no archive listing, no document parsing, no external tools (time from the filename, else the filesystem)"),
		sidecars:    fs.Bool("sidecars", false, "keep the files that belong to a photo or video with it: .AAE, .xmp, .THM, Google Takeout .json and the video of a Live Photo take its new name and are never deduplicated alone"),
	}
}

func ruleNames() string {
	n := make([]string, len(catalog.Rules))
	for i, r := range catalog.Rules {
		n[i] = r.Name
	}
	return strings.Join(n, ", ")
}

// filters returns base with every flag given on the command line applied on
// top (base = a settings file, or the zero value = every default).
func (c *catalogFlags) filters(base catalog.Filters) (catalog.Filters, error) {
	set := map[string]bool{}
	c.fs.Visit(func(fl *flag.Flag) { set[fl.Name] = true })
	f := base
	str := func(name string, dst *string, v string) {
		if set[name] {
			*dst = v
		}
	}
	str("preset", &f.Preset, *c.presetCSV)
	str("ext", &f.Ext, *c.extCSV)
	str("category", &f.Category, *c.catCSV)
	str("exclude", &f.Exclude, *c.exclCSV)
	str("min-size", &f.MinSize, *c.minSize)
	str("max-size", &f.MaxSize, *c.maxSize)
	str("whole-max", &f.WholeMax, *c.wholeMax)
	ptr := func(name string, dst **bool, v bool) {
		if set[name] {
			b := v
			*dst = &b
		}
	}
	ptr("skip-system-dirs", &f.SkipSystemDirs, *c.skipSysDirs)
	ptr("skip-cloud-dirs", &f.SkipCloudDirs, *c.skipCloud)
	ptr("skip-dev-dirs", &f.SkipDevDirs, *c.skipDev)
	ptr("skip-system-ext", &f.SkipSystemExt, *c.skipSysExt)
	ptr("skip-unknown-ext", &f.SkipUnknownExt, *c.skipUnknown)
	ptr("skip-noext", &f.SkipNoExt, *c.skipNoExt)
	if set["no-sweep"] {
		f.NoSweep = *c.noSweep
	}
	if set["keep-dot-dirs"] {
		f.KeepDotDirs = *c.keepDot
	}
	if set["keep-junk"] {
		f.KeepJunk = *c.keepJunk
	}
	if set["quick"] {
		f.Quick = *c.quick
	}
	if set["sidecars"] {
		f.Sidecars = *c.sidecars
	}
	if set["limit"] {
		f.Limit = *c.limit
	}
	// an explicit empty size meant 0 (no cap, no whole-read) before the
	// shared builder, where "" means the default: keep the flag meaning
	if set["max-size"] && *c.maxSize == "" {
		f.MaxSize = "0"
	}
	if set["whole-max"] && *c.wholeMax == "" {
		f.WholeMax = "0"
	}
	if set["prune"] {
		f.Prune = catalog.SplitCSV(*c.pruneCSV)
	}
	if set["exclude-path"] {
		f.ExcludePaths = append([]string(nil), *c.excludePaths...)
	}
	if set["exclude-glob"] {
		f.ExcludeGlobs = append([]string(nil), *c.excludeGlobs...)
	}
	if *c.pruneFrom != "" {
		b, err := os.ReadFile(*c.pruneFrom)
		if err != nil {
			return f, err
		}
		for _, l := range strings.Split(string(b), "\n") {
			if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
				f.Prune = append(f.Prune, l)
			}
		}
	}
	if *c.skipCSV != "" || *c.keepCSV != "" {
		r := map[string]bool{}
		for k, v := range f.Rules {
			r[k] = v
		}
		if err := catalog.ParseRules(*c.keepCSV, false, r); err != nil {
			return f, err
		}
		if err := catalog.ParseRules(*c.skipCSV, true, r); err != nil {
			return f, err
		}
		f.Rules = r
	}
	return f, nil
}

// hash applies the hashing flags to o; -j replaces the workers of a settings
// file only when given.
func (c *catalogFlags) hash(o *catalog.Options) {
	sparsehash.MaxBlocks = *c.blocks
	given := false
	c.fs.Visit(func(fl *flag.Flag) { given = given || fl.Name == "j" })
	if given || o.Workers <= 0 {
		o.Workers = *c.j
	}
	o.Coverage, o.Sample = *c.cov, *c.s
}

// resolve builds the catalog options from the flags alone, with the scope
// rule of the regime unless -allow-mixed (catalog).
func (c *catalogFlags) resolve() (catalog.Options, error) {
	f, err := c.filters(catalog.Filters{})
	if err != nil {
		return catalog.Options{}, err
	}
	if !*c.allowMixed {
		check := f.CheckTemplateScope
		if c.regime == regimeGolden {
			check = f.CheckGoldenScope
		}
		if err := check(); err != nil {
			return catalog.Options{}, mixedHint(err)
		}
	}
	o, err := f.Options(c.defPreset)
	c.hash(&o)
	return o, err
}

// mixedHint adds the CLI override to a regime refusal.
func mixedHint(err error) error {
	if errors.Is(err, catalog.ErrMixed) {
		return fmt.Errorf("%w (add -allow-mixed to force)", err)
	}
	return err
}

// addPerfFlags registers -perf / -perf-out; the returned func enables telemetry
// and returns the deferred printer.
func addPerfFlags(fs *flag.FlagSet) func() func() {
	on := fs.Bool("perf", false, "print performance telemetry (per phase / per stage) at the end")
	out := fs.String("perf-out", "", "also write the telemetry as JSON to this file")
	return func() func() {
		if !*on && *out == "" {
			return func() {}
		}
		perf.Enable()
		t0 := time.Now()
		return func() {
			perf.Meta("wallSec", fmt.Sprintf("%.2f", time.Since(t0).Seconds()))
			if *on {
				perf.Print(os.Stderr)
			}
			if *out != "" {
				if err := perf.WriteJSON(*out); err != nil {
					fmt.Fprintln(os.Stderr, "perf-out:", err)
				}
			}
		}
	}
}

// nameFlags collects -name FAM=PRESET[+clean|+raw|+prefix[:N]] (repeatable;
// FAM "all" sets every family).
type nameFlags map[string]naming.Choice

func (n nameFlags) String() string { return "" }
func (n nameFlags) Set(v string) error {
	fam, spec, ok := strings.Cut(v, "=")
	fam, spec = strings.TrimSpace(fam), strings.TrimSpace(spec)
	if !ok || fam == "" || spec == "" {
		return fmt.Errorf("-name %q: want FAMILY=PRESET[+clean|+raw|+prefix[:N]]", v)
	}
	c := naming.Choice{Preset: spec}
	if p, mode, ok := strings.Cut(spec, "+"); ok {
		c.Preset, c.Stem = p, mode
		if m, lvl, ok := strings.Cut(mode, ":"); ok {
			n, err := strconv.Atoi(lvl)
			if err != nil || m != naming.StemPrefix {
				return fmt.Errorf("-name %q: a level goes with +prefix only (+prefix:2)", v)
			}
			c.Stem, c.Level = m, n
		}
	}
	keys := []string{fam}
	if fam == "all" {
		keys = nil
		for _, f := range naming.Families {
			keys = append(keys, f.Key)
		}
	} else if _, ok := naming.FamilyByKey(fam); !ok {
		return fmt.Errorf("-name %q: unknown family %q (%s, all)", v, fam, familyKeys())
	}
	for _, k := range keys {
		n[k] = c
	}
	return nil
}

func familyKeys() string {
	var k []string
	for _, f := range naming.Families {
		k = append(k, f.Key)
	}
	return strings.Join(k, ", ")
}

// flagsGiven returns the names of the flags given on the command line.
func flagsGiven(fs *flag.FlagSet) map[string]bool {
	set := map[string]bool{}
	fs.Visit(func(fl *flag.Flag) { set[fl.Name] = true })
	return set
}

// multiFlag collects a repeatable string flag (-input A -input B).
type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

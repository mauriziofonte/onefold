// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package catalog

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mauriziofonte/onefold/internal/fsx"
	"github.com/mauriziofonte/onefold/internal/kinds"
)

// Skip rules: the switchable reasons a file or directory is left out of a
// catalog. One table for the CLI (-skip/-keep and the historical boolean
// flags), the GUI checkboxes and the reasons of the excluded-file lists.
const (
	RuleSystemDirs = "system-dirs"
	RuleCloudDirs  = "cloud-dirs"
	RuleDevDirs    = "dev-dirs"
	RuleDotDirs    = "dot-dirs"
	RuleJunk       = "junk"
	RuleSystemExt  = "system-ext"
	RuleUnknownExt = "unknown-ext"
	RuleNoExt      = "noext"
	RuleTransient  = "transient"
	RuleShortcuts  = "shortcuts"
	RuleBig        = "big"
)

// Reasons of exclusions that are not switchable rules.
const (
	ReasonDotfile = "dotfile"  // a file whose name starts with "."
	ReasonType    = "type"     // extension outside the selected presets / whitelist
	ReasonExclude = "exclude"  // path contains an -exclude substring
	ReasonPrune   = "prune"    // directory named by -prune / -prune-from
	ReasonEmpty   = "empty"    // 0-byte file
	ReasonMinSize = "min-size" // smaller than -min-size
	ReasonMaxSize = "max-size" // larger than the size cap
	ReasonLimit   = "limit"    // over -limit
)

// Rule is one switchable skip rule; every rule is on by default.
type Rule struct {
	Name string `json:"name"`
	Doc  string `json:"doc"`
}

// Rules lists the skip rules in display order.
var Rules = []Rule{
	{RuleSystemDirs, "prune Windows/system dirs: " + strings.Join(kinds.SystemDirs, ", ")},
	{RuleCloudDirs, "prune cloud sync dirs: " + strings.Join(kinds.CloudDirs, ", ")},
	{RuleDevDirs, "prune development dirs: " + strings.Join(kinds.DevDirs, ", ")},
	{RuleDotDirs, "prune directories whose name starts with '.'"},
	{RuleJunk, "skip OS droppings: " + strings.Join(kinds.JunkNames, ", ")},
	{RuleSystemExt, "skip system extensions (exe, dll, sys, ini...) when no whitelist is active"},
	{RuleUnknownExt, "skip extensions unknown to the registry when no whitelist is active"},
	{RuleNoExt, "skip files without extension"},
	{RuleTransient, "skip incomplete downloads, temp and lock files (tmp, crdownload, part, Office ~$ owner files...)"},
	{RuleShortcuts, "skip shortcuts (lnk, url, website, webloc, desktop...)"},
	{RuleBig, "skip files larger than " + DefaultMaxSize + " (unless -max-size is given)"},
}

// KnownRule reports whether name is a skip rule.
func KnownRule(name string) bool {
	for _, r := range Rules {
		if r.Name == name {
			return true
		}
	}
	return false
}

// ParseRules parses a CSV of rule names into set (value on), refusing
// unknown names. "all" names every rule.
func ParseRules(csv string, on bool, set map[string]bool) error {
	for _, n := range SplitCSV(csv) {
		n = strings.ToLower(n)
		if n == "all" {
			for _, r := range Rules {
				set[r.Name] = on
			}
			continue
		}
		if !KnownRule(n) {
			names := make([]string, len(Rules))
			for i, r := range Rules {
				names[i] = r.Name
			}
			return fmt.Errorf("unknown skip rule %q (rules: %s, all)", n, strings.Join(names, ", "))
		}
		set[n] = on
	}
	return nil
}

// Skip is one file or directory left out of a catalog, with its reason (a
// rule name or one of the Reason constants).
type Skip struct {
	Path string `json:"path"`
	Dir  bool   `json:"dir,omitempty"`
	Rule string `json:"rule"`
}

// SortSkips orders skips by path.
func SortSkips(s []Skip) { sort.Slice(s, func(i, j int) bool { return s[i].Path < s[j].Path }) }

// pruneRules maps a lowercase directory name to the rule that prunes it.
func (o Options) pruneRules() map[string]string {
	m := map[string]string{}
	add := func(rule string, names []string) {
		for _, d := range names {
			if d = strings.TrimSpace(d); d != "" {
				m[strings.ToLower(d)] = rule
			}
		}
	}
	add(ReasonPrune, o.Prune)
	if o.SkipDevDirs {
		add(RuleDevDirs, kinds.DevDirs)
	}
	if o.SkipCloudDirs {
		add(RuleCloudDirs, kinds.CloudDirs)
	}
	if o.SkipSystemDirs {
		add(RuleSystemDirs, kinds.SystemDirs)
	}
	return m
}

// dirRule returns why a directory (not a room root) is pruned, "" if it is walked.
func (o Options) dirRule(base string, prune map[string]string) string {
	switch {
	case !o.KeepDotDirs && strings.HasPrefix(base, "."):
		return RuleDotDirs
	case !o.KeepJunk && kinds.IsJunk(base):
		return RuleJunk
	}
	return prune[strings.ToLower(base)]
}

// fileRule returns why a file is skipped by its name, "" if it is accepted
// (sizes are checked later, on stat). Onefold' own temp files are skipped
// without a reason ("-").
func (o Options) fileRule(base, ext string) string {
	switch {
	case strings.HasPrefix(base, ".") && !o.KeepDotfiles:
		return ReasonDotfile
	case fsx.IsTemp(base):
		return "-"
	case !o.KeepJunk && kinds.IsJunk(base):
		return RuleJunk
	case o.Exts != nil && !o.Exts[ext]:
		return ReasonType
	case o.SkipNoExt && ext == "":
		return RuleNoExt
	case o.SkipTransient && kinds.IsTransient(base):
		return RuleTransient
	}
	if kinds.IsShortcut(ext) {
		// shortcuts follow their own rule, also those flagged system (lnk)
		if o.SkipShortcuts {
			return RuleShortcuts
		}
		return ""
	}
	switch {
	case o.Exts == nil && o.SkipSystemExt && kinds.IsSystemExt(ext):
		return RuleSystemExt
	case o.Exts == nil && o.SkipUnknownExt && ext != "" && !kinds.Known(ext):
		return RuleUnknownExt
	}
	return ""
}

// Unfiltered returns o with every filter and skip rule off (merge output:
// every file counts as present). Hash, workers and run fields are kept.
func (o Options) Unfiltered() Options {
	o.Exts, o.Exclude, o.Prune, o.ExcludePaths, o.ExcludeGlobs = nil, nil, nil, nil, nil
	o.KeepDotDirs, o.KeepJunk = true, true
	o.SkipSystemDirs, o.SkipCloudDirs, o.SkipDevDirs = false, false, false
	o.SkipSystemExt, o.SkipUnknownExt, o.SkipNoExt, o.SkipTransient, o.SkipShortcuts = false, false, false, false, false
	o.MinSize, o.MaxSize, o.Limit = 0, 0, 0
	o.OnSkip = nil
	return o
}

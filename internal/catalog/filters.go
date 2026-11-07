// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package catalog

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mauriziofonte/onefold/internal/kinds"
	"github.com/mauriziofonte/onefold/internal/sparsehash"
)

// Default sizes of the catalog filters (SIZE strings, see ParseSize).
const (
	DefaultMaxSize  = "50G"
	DefaultWholeMax = "320K"
)

// Filters describes what a catalog enumerates, in the user's terms: the
// `catalog`/`merge` flags and the GUI filter form both build their Options
// through it, so they share one set of defaults. It is also the JSON of the
// filters in a settings file and in a GUI job.
type Filters struct {
	Preset   string `json:"preset,omitempty"`   // CSV of presets ("" = the caller's default preset)
	Ext      string `json:"ext,omitempty"`      // CSV extension whitelist: replaces Preset
	Category string `json:"category,omitempty"` // CSV of categories added to the set
	Exclude  string `json:"exclude,omitempty"`  // CSV of room-relative path substrings
	// ExcludePaths: files or folders left out (absolute; a folder = all it
	// holds); ExcludeGlobs: patterns on the room-relative path.
	ExcludePaths []string `json:"excludePaths,omitempty"`
	ExcludeGlobs []string `json:"excludeGlobs,omitempty"`
	Prune        []string `json:"prune,omitempty"` // directory names pruned (whole component, case-insensitive)

	// Skip rules (catalog.Rules): Rules sets any of them by name and wins over
	// the rest; else the historical fields below (nil = on). NoSweep turns every
	// rule off except dot-dirs and junk, and lifts the default size cap (an
	// explicit MaxSize still applies).
	Rules          map[string]bool `json:"rules,omitempty"`
	NoSweep        bool            `json:"noSweep,omitempty"`
	SkipSystemDirs *bool           `json:"skipSystemDirs,omitempty"`
	SkipCloudDirs  *bool           `json:"skipCloudDirs,omitempty"`
	SkipDevDirs    *bool           `json:"skipDevDirs,omitempty"`
	SkipSystemExt  *bool           `json:"skipSystemExt,omitempty"`
	SkipUnknownExt *bool           `json:"skipUnknownExt,omitempty"`
	SkipNoExt      *bool           `json:"skipNoExt,omitempty"`

	MinSize  string `json:"minSize,omitempty"`  // SIZE, "" = none
	MaxSize  string `json:"maxSize,omitempty"`  // SIZE, "" = DefaultMaxSize (none with NoSweep), "0" = no cap
	WholeMax string `json:"wholeMax,omitempty"` // SIZE, "" = DefaultWholeMax, "0" = seek-based sampling only

	KeepDotDirs bool `json:"keepDotDirs,omitempty"`
	// KeepDotfiles catalogs files whose name starts with "." and 0-byte
	// files (merge -keep-dotfiles); an empty file is identified by its name.
	KeepDotfiles bool `json:"keepDotfiles,omitempty"`
	KeepJunk     bool `json:"keepJunk,omitempty"`
	Quick        bool `json:"quick,omitempty"`
	Limit        int  `json:"limit,omitempty"`
	// Sidecars keeps the files that belong to a photo or video with it
	// (Options.Sidecars): its edits and metadata take its new name.
	Sidecars bool `json:"sidecars,omitempty"`
	// Takeout reads the Google Takeout metadata files (Options.Takeout).
	Takeout bool `json:"takeout,omitempty"`
}

// RuleOn reports whether the skip rule name is on for f.
func (f Filters) RuleOn(name string) bool {
	if v, ok := f.Rules[name]; ok {
		return v
	}
	switch name {
	case RuleDotDirs:
		return !f.KeepDotDirs
	case RuleJunk:
		return !f.KeepJunk
	case RuleOnlineOnly:
		return true // reading downloads: only an explicit -keep online-only does
	}
	if f.NoSweep {
		return false
	}
	var b *bool
	switch name {
	case RuleSystemDirs:
		b = f.SkipSystemDirs
	case RuleCloudDirs:
		b = f.SkipCloudDirs
	case RuleDevDirs:
		b = f.SkipDevDirs
	case RuleSystemExt:
		b = f.SkipSystemExt
	case RuleUnknownExt:
		b = f.SkipUnknownExt
	case RuleNoExt:
		b = f.SkipNoExt
	}
	return b == nil || *b
}

// Options resolves f into catalog Options (hash defaults included); defPreset
// applies when neither Preset nor Ext is given. Workers, Out, Host and the
// other run fields stay for the caller.
func (f Filters) Options(defPreset string) (Options, error) {
	o := Options{Coverage: sparsehash.DefaultCoverage, Sample: sparsehash.DefaultSampleSize,
		KeepDotDirs: !f.RuleOn(RuleDotDirs), KeepJunk: !f.RuleOn(RuleJunk), Quick: f.Quick, Limit: f.Limit,
		KeepDotfiles: f.KeepDotfiles, KeepEmpty: f.KeepDotfiles, Sidecars: f.Sidecars, Takeout: f.Takeout}
	for n := range f.Rules {
		if !KnownRule(n) {
			return o, fmt.Errorf("unknown skip rule %q", n)
		}
	}
	preset := f.Preset
	if preset == "" {
		preset = defPreset
	}
	var err error
	if o.Exts, err = ResolveExts(preset, f.Ext, f.Category); err != nil {
		return o, err
	}
	o.Exclude = SplitCSV(f.Exclude)
	if o.ExcludePaths, err = CleanExcludePaths(f.ExcludePaths); err != nil {
		return o, err
	}
	if o.ExcludeGlobs, err = CompileGlobs(f.ExcludeGlobs); err != nil {
		return o, err
	}
	o.Prune = append(o.Prune, f.Prune...)
	o.SkipSystemDirs, o.SkipCloudDirs, o.SkipDevDirs = f.RuleOn(RuleSystemDirs), f.RuleOn(RuleCloudDirs), f.RuleOn(RuleDevDirs)
	o.SkipSystemExt, o.SkipUnknownExt, o.SkipNoExt = f.RuleOn(RuleSystemExt), f.RuleOn(RuleUnknownExt), f.RuleOn(RuleNoExt)
	o.SkipTransient, o.SkipShortcuts = f.RuleOn(RuleTransient), f.RuleOn(RuleShortcuts)
	o.SkipOnlineOnly = f.RuleOn(RuleOnlineOnly)
	maxSize := f.MaxSize
	if maxSize == "" && f.RuleOn(RuleBig) {
		maxSize = DefaultMaxSize
	}
	wholeMax := f.WholeMax
	if wholeMax == "" {
		wholeMax = DefaultWholeMax
	}
	if o.MinSize, err = ParseSize(f.MinSize); err != nil {
		return o, err
	}
	if o.MaxSize, err = ParseSize(maxSize); err != nil {
		return o, err
	}
	if o.WholeMax, err = ParseSize(wholeMax); err != nil {
		return o, err
	}
	return o, nil
}

// ResolveExts turns the preset / explicit / category selections into the
// extension whitelist; nil = every extension (preset "all" without -ext).
func ResolveExts(presetCSV, extCSV, catCSV string) (map[string]bool, error) {
	var set map[string]bool
	if extCSV != "" {
		set = map[string]bool{}
		for _, e := range SplitCSV(extCSV) {
			set[strings.ToLower(strings.TrimPrefix(e, "."))] = true
		}
	} else {
		p, all, err := kinds.PresetExts(SplitCSV(presetCSV))
		if err != nil {
			return nil, err
		}
		if all {
			return nil, nil
		}
		set = p
	}
	if catCSV != "" {
		c, err := kinds.CategoryExts(SplitCSV(catCSV))
		if err != nil {
			return nil, err
		}
		for e := range c {
			set[e] = true
		}
	}
	return set, nil
}

// SplitCSV splits on commas, trimming blanks and dropping empty items.
func SplitCSV(s string) []string {
	var out []string
	for _, e := range strings.Split(s, ",") {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	return out
}

// ParseSize accepts plain bytes or a K/M/G/T suffix (binary units, optional
// trailing B); "" = 0.
func ParseSize(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0, nil
	}
	n := strings.TrimSuffix(s, "B")
	mult := int64(1)
	if n != "" {
		switch n[len(n)-1] {
		case 'K':
			mult = 1 << 10
		case 'M':
			mult = 1 << 20
		case 'G':
			mult = 1 << 30
		case 'T':
			mult = 1 << 40
		}
	}
	if mult > 1 {
		n = n[:len(n)-1]
	}
	v, err := strconv.ParseInt(n, 10, 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	return v * mult, nil
}

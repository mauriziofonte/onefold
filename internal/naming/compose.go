// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package naming

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Stem modes: what a family does with the file name.
const (
	StemNorm   = "norm"   // {stem-norm}: tool markers removed, "[origin] " kept (default)
	StemClean  = "clean"  // {stem-clean}: the "[origin] " prefix dropped too
	StemRaw    = "raw"    // {stem}: the name untouched
	StemPrefix = "prefix" // "[<folder>] " + {stem-clean}: the folder Level levels up (1 = parent)
)

// StemModes lists the stem modes.
var StemModes = []string{StemNorm, StemClean, StemRaw, StemPrefix}

// Family is a file family of merge and rearrange: the catalog
// preset that selects its files, the selector of its naming rule and the
// presets made for it (besides GenericPresets).
type Family struct {
	Key      string   `json:"key"`
	Preset   string   `json:"preset"` // catalog preset; "" = everything else
	Sel      string   `json:"sel"`
	Specific []string `json:"specific"`
}

// Families in display order; "rest" (everything else) is last.
var Families = []Family{
	{"img", "image", "photo,image", []string{"photo-timestamp", "photo-month", "devices"}},
	{"video", "video", "video", []string{"video-tier"}},
	{"doc", "document", docGroups, []string{"documents"}},
	{"music", "audio", "audio", []string{"music"}},
	{"arch", "archive", "archive", nil},
	{"rest", "", "*", nil},
}

// GenericPresets are the presets with a single "*" rule: valid for every family.
var GenericPresets = []string{"ext-year", "date-tree", "ext-flat", "ext-size", "ext-era-year", "ext-era-year-month",
	"ext-era-year-size-month", "ext-era-year-origin", "ext-era-year-origin-mtime"}

// FamilyByKey returns the family of key.
func FamilyByKey(key string) (Family, bool) {
	for _, f := range Families {
		if f.Key == key {
			return f, true
		}
	}
	return Family{}, false
}

// Choice is the naming of one family: a preset valid for it and a stem mode.
type Choice struct {
	Preset string `json:"preset"`
	Stem   string `json:"stem,omitempty"`  // "" = norm
	Level  int    `json:"level,omitempty"` // prefix: folder level, 0 = 1
}

func (c Choice) norm() Choice {
	if c.Stem == "" {
		c.Stem = StemNorm
	}
	if c.Stem != StemPrefix {
		c.Level = 0
	} else if c.Level == 0 {
		c.Level = 1
	}
	return c
}

func (c Choice) valid(f Family) error {
	ok := false
	for _, p := range append(append([]string{}, f.Specific...), GenericPresets...) {
		ok = ok || p == c.Preset
	}
	if !ok {
		return fmt.Errorf("naming: preset %q is not valid for family %s (%s)", c.Preset, f.Key,
			strings.Join(append(append([]string{}, f.Specific...), GenericPresets...), ", "))
	}
	switch c.Stem {
	case "", StemNorm, StemClean, StemRaw:
	case StemPrefix:
		if c.Level < 0 || c.Level > MaxOriginLevel {
			return fmt.Errorf("naming: prefix level %d of family %s out of 1..%d", c.Level, f.Key, MaxOriginLevel)
		}
	default:
		return fmt.Errorf("naming: stem mode %q of family %s unknown (%s)", c.Stem, f.Key, strings.Join(StemModes, ", "))
	}
	return nil
}

// CatalogPreset is the catalog scope of the selected families: the CSV union
// of their presets, "all" with everything else.
func CatalogPreset(fams map[string]Choice) string {
	if _, ok := fams["rest"]; ok {
		return "all"
	}
	var out []string
	for _, f := range Families {
		if _, ok := fams[f.Key]; ok && f.Preset != "" {
			out = append(out, f.Preset)
		}
	}
	return strings.Join(out, ",")
}

// Compose returns the naming spec of the selected families (one Choice per
// selected family key): a preset name when one preset covers them all
// unchanged, else a selector map whose "*" rule is the choice of everything
// else (ext-year when that family is not selected).
func Compose(fams map[string]Choice) (string, error) {
	var on []Family
	for k := range fams {
		if _, ok := FamilyByKey(k); !ok {
			return "", fmt.Errorf("naming: unknown family %q", k)
		}
	}
	for _, f := range Families {
		if c, ok := fams[f.Key]; ok {
			if err := c.valid(f); err != nil {
				return "", err
			}
			on = append(on, f)
		}
	}
	if len(on) == 0 {
		return "ext-year", nil
	}
	first := fams[on[0].Key].norm()
	same := true
	for _, f := range on[1:] {
		same = same && fams[f.Key].norm() == first
	}
	if same && isGeneric(first.Preset) {
		return whole(first)
	}
	fallback := Choice{Preset: "ext-year"}.norm()
	if c, ok := fams["rest"]; ok {
		fallback = c.norm()
	}
	var parts []Family
	for _, f := range on {
		if f.Key != "rest" && fams[f.Key].norm() != fallback {
			parts = append(parts, f)
		}
	}
	if len(parts) == 0 {
		return whole(fallback)
	}
	if one := fams[parts[0].Key].norm(); len(parts) == 1 && fallback == (Choice{Preset: "ext-year"}).norm() &&
		!isGeneric(one.Preset) && one.Stem == StemNorm {
		return one.Preset, nil // a family preset already files the rest as ext-year
	}
	var rules []string
	for _, f := range parts {
		t, err := ruleOf(fams[f.Key].norm(), f.Sel)
		if err != nil {
			return "", err
		}
		rules = append(rules, f.Sel+"="+t)
	}
	t, err := ruleOf(fallback, "*")
	if err != nil {
		return "", err
	}
	return strings.Join(append(rules, "*="+t), ";"), nil
}

func isGeneric(p string) bool {
	for _, g := range GenericPresets {
		if g == p {
			return true
		}
	}
	return false
}

// whole renders a choice for every file: the preset name (+ "-clean") when
// the stem mode allows it, else its template.
func whole(c Choice) (string, error) {
	switch c.Stem {
	case StemNorm:
		return c.Preset, nil
	case StemClean:
		return c.Preset + CleanSuffix, nil
	}
	return ruleOf(c, "*")
}

// ruleOf returns the template the choice applies to sel (the preset's rule
// for that selector, else its "*" rule), with its stem mode applied.
func ruleOf(c Choice, sel string) (string, error) {
	p, ok := Presets[c.Preset]
	if !ok {
		return "", fmt.Errorf("naming: unknown preset %q", c.Preset)
	}
	t := templateOf(p.Spec, sel)
	return StemTemplate(t, c.Stem, c.Level)
}

// templateOf returns the template of spec for sel, else its "*" rule.
func templateOf(spec, sel string) string {
	def := ""
	for _, part := range strings.Split(spec, ";") {
		s, t, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(s) == sel {
			return t
		}
		if strings.TrimSpace(s) == "*" {
			def = t
		}
	}
	return def
}

var reStemNorm = regexp.MustCompile(`([{|])stem-norm([|}])`)

// StemTemplate applies a stem mode to one template: clean and prefix use
// {stem-clean} for {stem} and {stem-norm}, raw uses {stem} for {stem-norm};
// prefix also puts "[{origin-N}] " in front of the file name (the last path
// segment, also when it is chosen inside a group).
func StemTemplate(tpl, mode string, level int) (string, error) {
	switch mode {
	case "", StemNorm:
		return tpl, nil
	case StemClean:
		return reStem.ReplaceAllString(tpl, "${1}stem-clean${2}"), nil
	case StemRaw:
		return reStemNorm.ReplaceAllString(tpl, "${1}stem${2}"), nil
	case StemPrefix:
		if level == 0 {
			level = 1
		}
		if level < 1 || level > MaxOriginLevel {
			return "", fmt.Errorf("naming: prefix level %d out of 1..%d", level, MaxOriginLevel)
		}
		t, err := Parse(reStem.ReplaceAllString(tpl, "${1}stem-clean${2}"))
		if err != nil {
			return "", err
		}
		pre := group{seq{lit("["), ph{{name: "origin-" + strconv.Itoa(level)}}, lit("] ")}}
		return prefixName(t.root, pre).String(), nil
	}
	return "", fmt.Errorf("naming: stem mode %q unknown (%s)", mode, strings.Join(StemModes, ", "))
}

// prefixName inserts pre at the start of the last path segment of s. When the
// segment is chosen by a group whose alternatives hold separators, pre goes
// into each alternative's own last segment.
func prefixName(s seq, pre node) seq {
	last := -1
	for i, n := range s {
		if _, ok := n.(sep); ok {
			last = i
		}
	}
	for i := len(s) - 1; i > last; i-- {
		if g, ok := s[i].(group); ok && hasSep(g) {
			ng := make(group, len(g))
			for k, a := range g {
				ng[k] = prefixName(a, pre)
			}
			out := append(seq{}, s...)
			out[i] = ng
			return out
		}
	}
	out := append(seq{}, s[:last+1]...)
	out = append(out, pre)
	return append(out, s[last+1:]...)
}

func hasSep(g group) bool {
	for _, a := range g {
		for _, n := range a {
			switch x := n.(type) {
			case sep:
				return true
			case group:
				if hasSep(x) {
					return true
				}
			}
		}
	}
	return false
}

// String renders the sequence back to template source.
func (s seq) String() string {
	var b strings.Builder
	for _, n := range s {
		switch x := n.(type) {
		case lit:
			b.WriteString(string(x))
		case sep:
			b.WriteByte('/')
		case ph:
			alts := make([]string, len(x))
			for i, a := range x {
				if a.name == "" {
					alts[i] = `"` + a.literal + `"`
				} else {
					alts[i] = a.name
				}
			}
			b.WriteString("{" + strings.Join(alts, "|") + "}")
		case group:
			alts := make([]string, len(x))
			for i, a := range x {
				alts[i] = a.String()
			}
			b.WriteString("<" + strings.Join(alts, "|") + ">")
		}
	}
	return b.String()
}

// FamilyKeys returns the keys of fams in display order (for messages).
func FamilyKeys(fams map[string]Choice) []string {
	var out []string
	for k := range fams {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return famIndex(out[i]) < famIndex(out[j]) })
	return out
}

func famIndex(k string) int {
	for i, f := range Families {
		if f.Key == k {
			return i
		}
	}
	return len(Families)
}

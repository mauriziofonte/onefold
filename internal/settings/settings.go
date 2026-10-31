// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package settings is the one schema of the merge and rearrange settings
// : the JSON a GUI job carries, the file `-settings` reads and what the
// CLI flags fill. Both front ends turn it into engine options here, so a
// setting means the same thing everywhere.
package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/merge"
	"github.com/mauriziofonte/onefold/internal/naming"
	"github.com/mauriziofonte/onefold/internal/rearrange"
)

// Naming is the naming of a merge or rearrange. Families select the
// file types and name each family; Template (expert) replaces their naming
// and keeps their scope. Spec + Clean (CLI -naming, jobs of older versions)
// and Layout (CLI -layout aliases) apply when there are no families.
type Naming struct {
	Families map[string]naming.Choice `json:"families,omitempty"`
	Template string                   `json:"template,omitempty"`
	Spec     string                   `json:"naming,omitempty"`
	Clean    bool                     `json:"clean,omitempty"`
	Layout   string                   `json:"layout,omitempty"`
}

// Resolve returns the naming spec the engine runs.
func (n Naming) Resolve() (string, error) {
	switch {
	case strings.TrimSpace(n.Template) != "":
		return strings.TrimSpace(n.Template), nil
	case len(n.Families) > 0:
		return naming.Compose(n.Families)
	}
	spec := strings.TrimSpace(n.Spec)
	if spec == "" {
		spec = naming.LayoutName(n.Layout)
		if spec == "" {
			spec = "ext-year"
		}
	}
	if n.Clean {
		if _, ok := naming.Presets[spec]; ok {
			spec += naming.CleanSuffix
		}
	}
	return spec, nil
}

// Scope applies the families to f: their catalog preset; with everything
// else, unknown extensions and files without extension are files like the
// others unless a rule names them.
func (n Naming) Scope(f catalog.Filters) (catalog.Filters, error) {
	if len(n.Families) == 0 {
		return f, nil
	}
	if f.Preset != "" || f.Ext != "" || f.Category != "" {
		return f, errors.New("the file families select the file types: preset, ext and category are not allowed with them")
	}
	f.Preset = naming.CatalogPreset(n.Families)
	if _, rest := n.Families["rest"]; rest {
		r := map[string]bool{}
		for k, v := range f.Rules {
			r[k] = v
		}
		for _, k := range []string{catalog.RuleUnknownExt, catalog.RuleNoExt} {
			if _, set := r[k]; !set {
				r[k] = false
			}
		}
		f.Rules = r
	}
	return f, nil
}

// check applies the regime of merge and rearrange unless allowMixed,
// and verifies that the naming resolves.
func (n Naming) check(f catalog.Filters, allowMixed bool) (catalog.Filters, string, error) {
	f, err := n.Scope(f)
	if err != nil {
		return f, "", err
	}
	spec, err := n.Resolve()
	if err != nil {
		return f, "", err
	}
	if !allowMixed {
		if err := f.CheckTemplateScope(); err != nil {
			return f, "", err
		}
		if err := naming.CheckTemplate(spec); err != nil {
			return f, "", err
		}
	}
	if _, err := naming.Layout(spec); err != nil {
		return f, "", err
	}
	return f, spec, nil
}

// Merge mirrors `onefold merge`.
type Merge struct {
	Kind           string          `json:"kind,omitempty"` // "merge" in a settings file
	Inputs         []string        `json:"inputs"`
	Output         string          `json:"output"`
	Filters        catalog.Filters `json:"filters"`
	TrustSparse    bool            `json:"trustSparse,omitempty"`
	NoVerifyCopy   bool            `json:"noVerifyCopy,omitempty"`
	NoFsync        bool            `json:"noFsync,omitempty"`
	VerifyUncached bool            `json:"verifyUncached,omitempty"`
	NoCache        bool            `json:"noCache,omitempty"`
	Workers        int             `json:"workers,omitempty"` // catalog and copy workers, 0 = NumCPU
	Naming
}

// Options checks m and builds the engine options; the caller adds the run
// fields (Execute, Log, Gate...). allowMixed lifts the regime checks (CLI).
func (m Merge) Options(allowMixed bool) (merge.Options, error) {
	if len(m.Inputs) == 0 {
		return merge.Options{}, errors.New("inputs: add at least one folder")
	}
	if strings.TrimSpace(m.Output) == "" {
		return merge.Options{}, errors.New("output: a folder is required")
	}
	f, spec, err := m.check(m.Filters, allowMixed)
	if err != nil {
		return merge.Options{}, err
	}
	co, err := catalogOptions(f, m.Workers)
	if err != nil {
		return merge.Options{}, err
	}
	return merge.Options{Inputs: m.Inputs, Output: m.Output, Catalog: co, Naming: spec, TrustSparse: m.TrustSparse,
		NoVerifyCopy: m.NoVerifyCopy, NoFsync: m.NoFsync, VerifyUncached: m.VerifyUncached, NoCache: m.NoCache, Workers: m.Workers}, nil
}

// Rearrange mirrors `onefold rearrange`.
type Rearrange struct {
	Kind          string          `json:"kind,omitempty"` // "rearrange" in a settings file
	Root          string          `json:"root"`
	ReportDupes   bool            `json:"reportDupes,omitempty"` // keep confirmed duplicates (default: delete)
	KeepEmptyDirs bool            `json:"keepEmptyDirs,omitempty"`
	NoCache       bool            `json:"noCache,omitempty"`
	Filters       catalog.Filters `json:"filters"`
	Workers       int             `json:"workers,omitempty"`
	Naming
}

// Options checks r and builds the engine options (see Merge.Options).
func (r Rearrange) Options(allowMixed bool) (rearrange.Options, error) {
	if strings.TrimSpace(r.Root) == "" {
		return rearrange.Options{}, errors.New("root: a folder is required")
	}
	if r.Filters.KeepDotfiles {
		// empty files share an identity by name: rearrange would delete one of
		// two __init__.py as a duplicate
		return rearrange.Options{}, errors.New("filters.keepDotfiles is a merge option: rearrange never takes dotfiles or empty files")
	}
	f, spec, err := r.check(r.Filters, allowMixed)
	if err != nil {
		return rearrange.Options{}, err
	}
	co, err := catalogOptions(f, r.Workers)
	if err != nil {
		return rearrange.Options{}, err
	}
	return rearrange.Options{Root: r.Root, Naming: spec, Catalog: co, DeleteDupes: !r.ReportDupes, KeepEmptyDirs: r.KeepEmptyDirs,
		NoCache: r.NoCache, Workers: r.Workers}, nil
}

func catalogOptions(f catalog.Filters, workers int) (catalog.Options, error) {
	o, err := f.Options("all")
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	o.Workers = workers
	return o, err
}

// Load reads a settings file into v (*Merge or *Rearrange). Unknown keys are
// errors (a typo must not silently fall back to a default); a "kind" other
// than the command's is refused.
func Load(path, kind string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var head struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return fmt.Errorf("settings %s: %w", path, err)
	}
	if head.Kind != "" && head.Kind != kind {
		return fmt.Errorf("settings %s are for %s, not %s", path, head.Kind, kind)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("settings %s: %w", path, err)
	}
	return nil
}

// Args returns the `onefold merge` arguments that reproduce m (the GUI's
// "copy command"; -print-settings goes the other way).
func (m Merge) Args() []string {
	a := []string{"merge"}
	for _, in := range m.Inputs {
		a = append(a, "-input", in)
	}
	a = append(a, "-output", m.Output)
	a = append(a, m.Naming.args()...)
	a = append(a, filterArgs(m.Filters)...)
	flags := []struct {
		on   bool
		name string
	}{{m.TrustSparse, "-trust-sparse"}, {m.NoVerifyCopy, "-no-verify-copy"}, {m.NoFsync, "-no-fsync"}, {m.VerifyUncached, "-verify-uncached"}, {m.NoCache, "-no-cache"},
		{m.Filters.KeepDotfiles, "-keep-dotfiles"}}
	for _, f := range flags {
		if f.on {
			a = append(a, f.name)
		}
	}
	if m.Workers > 0 {
		a = append(a, "-j", strconv.Itoa(m.Workers), "-copy-j", strconv.Itoa(m.Workers))
	}
	return a
}

// Args returns the `onefold rearrange` arguments that reproduce r.
func (r Rearrange) Args() []string {
	a := []string{"rearrange"}
	a = append(a, r.Naming.args()...)
	a = append(a, filterArgs(r.Filters)...)
	if r.ReportDupes {
		a = append(a, "-dupes", "report")
	}
	if r.KeepEmptyDirs {
		a = append(a, "-keep-empty-dirs")
	}
	if r.NoCache {
		a = append(a, "-no-cache")
	}
	if r.Workers > 0 {
		a = append(a, "-j", strconv.Itoa(r.Workers), "-move-j", strconv.Itoa(r.Workers))
	}
	return append(a, r.Root)
}

func (n Naming) args() []string {
	var a []string
	if len(n.Families) > 0 {
		for _, k := range naming.FamilyKeys(n.Families) {
			c := n.Families[k]
			v := k + "=" + c.Preset
			switch {
			case c.Stem == naming.StemPrefix && c.Level > 1:
				v += "+prefix:" + strconv.Itoa(c.Level)
			case c.Stem != "" && c.Stem != naming.StemNorm:
				v += "+" + c.Stem
			}
			a = append(a, "-name", v)
		}
		if t := strings.TrimSpace(n.Template); t != "" {
			a = append(a, "-naming", t)
		}
		return a
	}
	if t := strings.TrimSpace(n.Template); t != "" {
		return []string{"-naming", t}
	}
	spec, err := n.Resolve()
	if err != nil || spec == "ext-year" {
		return nil
	}
	return []string{"-naming", spec}
}

// filterArgs returns the catalog flags of f: the rules that are off (-keep),
// the ones set on by name (-skip), sizes, exclusions and the scope of
// settings without families.
func filterArgs(f catalog.Filters) []string {
	var a []string
	if f.Preset != "" {
		a = append(a, "-preset", f.Preset)
	}
	if f.Ext != "" {
		a = append(a, "-ext", f.Ext)
	}
	if f.Category != "" {
		a = append(a, "-category", f.Category)
	}
	var keep, skip []string
	for _, r := range catalog.Rules {
		switch on := f.RuleOn(r.Name); {
		case !on:
			keep = append(keep, r.Name)
		case f.Rules[r.Name]:
			skip = append(skip, r.Name)
		}
	}
	if len(keep) > 0 {
		a = append(a, "-keep", strings.Join(keep, ","))
	}
	if len(skip) > 0 {
		a = append(a, "-skip", strings.Join(skip, ","))
	}
	if f.MaxSize != "" {
		a = append(a, "-max-size", f.MaxSize)
	}
	if f.MinSize != "" {
		a = append(a, "-min-size", f.MinSize)
	}
	if f.Exclude != "" {
		a = append(a, "-exclude", f.Exclude)
	}
	for _, p := range f.ExcludePaths {
		a = append(a, "-exclude-path", p)
	}
	for _, g := range f.ExcludeGlobs {
		a = append(a, "-exclude-glob", g)
	}
	if len(f.Prune) > 0 {
		a = append(a, "-prune", strings.Join(f.Prune, ","))
	}
	if f.WholeMax != "" {
		a = append(a, "-whole-max", f.WholeMax)
	}
	if f.Quick {
		a = append(a, "-quick")
	}
	if f.Limit > 0 {
		a = append(a, "-limit", strconv.Itoa(f.Limit))
	}
	return a
}

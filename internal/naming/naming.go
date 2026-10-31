// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package naming renders target paths from templates with placeholders.
//
// Grammar (also printed by `onefold naming`):
//
//	literal text          copied as is
//	{name}                placeholder value ("" if unknown)
//	{a|b|"lit"}           first non-empty of placeholders a, b, or the literal
//	<A|B|...>             group: the first alternative whose placeholders are
//	                      ALL non-empty; if none qualifies, nothing. Nestable.
//	/ or \                path separator (both accepted); empty segments dropped
//
// A spec maps selectors to templates: "sel[,sel]=template;...;*=template".
// A selector is a harmonized extension (".pdf"), a registry group (uppercase:
// PDF, DOC, RAW, ...) or a kind (lowercase: photo, audio, ...); precedence
// extension > group > kind > "*".
//
// '<', '>' and '|' are invalid in Windows file names, so they never need to be
// literal. Every rendered segment is sanitized for Linux AND Windows.
package naming

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/mauriziofonte/onefold/internal/kinds"
	"github.com/mauriziofonte/onefold/internal/names"
)

type node interface {
	render(v Vars) (string, bool) // text, complete (all placeholders non-empty)
}

type lit string

func (l lit) render(Vars) (string, bool) { return string(l), true }

type sep struct{}

func (sep) render(Vars) (string, bool) { return "/", true }

type alt struct {
	name    string // placeholder name, or "" for a literal
	literal string
}

type ph []alt

func (p ph) render(v Vars) (string, bool) {
	for _, a := range p {
		if a.name == "" {
			if a.literal != "" {
				return a.literal, true
			}
			continue
		}
		if s := v.Get(a.name); s != "" {
			return s, true
		}
	}
	return "", false
}

type seq []node

func (s seq) render(v Vars) (string, bool) {
	var b strings.Builder
	ok := true
	for _, n := range s {
		t, c := n.render(v)
		b.WriteString(t)
		ok = ok && c
	}
	return b.String(), ok
}

type group []seq

func (g group) render(v Vars) (string, bool) {
	for _, alternative := range g {
		if t, ok := alternative.render(v); ok {
			return t, true
		}
	}
	return "", true // an empty group is not a missing value
}

// Template is a parsed naming template.
type Template struct {
	src  string
	root seq
}

// String returns the template source.
func (t *Template) String() string { return t.src }

// Parse compiles a template.
func Parse(src string) (*Template, error) {
	p := &parser{s: src}
	root, err := p.seq(false)
	if err != nil {
		return nil, err
	}
	if p.i < len(p.s) {
		return nil, fmt.Errorf("naming: unexpected %q at %d", p.s[p.i], p.i)
	}
	if err := validate(root); err != nil {
		return nil, err
	}
	return &Template{src: src, root: root}, nil
}

type parser struct {
	s string
	i int
}

// seq parses until end, or until '|' / '>' when inGroup.
func (p *parser) seq(inGroup bool) (seq, error) {
	var out seq
	var litb strings.Builder
	flush := func() {
		if litb.Len() > 0 {
			out = append(out, lit(litb.String()))
			litb.Reset()
		}
	}
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch {
		case inGroup && (c == '|' || c == '>'):
			flush()
			return out, nil
		case c == '/' || c == '\\':
			flush()
			out = append(out, sep{})
			p.i++
		case c == '{':
			flush()
			n, err := p.placeholder()
			if err != nil {
				return nil, err
			}
			out = append(out, n)
		case c == '<':
			flush()
			p.i++
			var g group
			for {
				alternative, err := p.seq(true)
				if err != nil {
					return nil, err
				}
				g = append(g, alternative)
				if p.i >= len(p.s) {
					return nil, fmt.Errorf("naming: unclosed '<' in %q", p.s)
				}
				if p.s[p.i] == '>' {
					p.i++
					break
				}
				p.i++ // '|'
			}
			out = append(out, g)
		case c == '>' || c == '|' || c == '}':
			return nil, fmt.Errorf("naming: unexpected %q at %d in %q", c, p.i, p.s)
		default:
			litb.WriteByte(c)
			p.i++
		}
	}
	flush()
	return out, nil
}

func (p *parser) placeholder() (node, error) {
	end := strings.IndexByte(p.s[p.i:], '}')
	if end < 0 {
		return nil, fmt.Errorf("naming: unclosed '{' in %q", p.s)
	}
	body := p.s[p.i+1 : p.i+end]
	p.i += end + 1
	var out ph
	for _, part := range strings.Split(body, "|") {
		part = strings.TrimSpace(part)
		if len(part) >= 2 && part[0] == '"' && part[len(part)-1] == '"' {
			out = append(out, alt{literal: part[1 : len(part)-1]})
			continue
		}
		if part == "" {
			return nil, fmt.Errorf("naming: empty placeholder in %q", p.s)
		}
		out = append(out, alt{name: part})
	}
	return out, nil
}

func validate(s seq) error {
	for _, n := range s {
		switch x := n.(type) {
		case ph:
			for _, a := range x {
				if a.name != "" && !Known(a.name) {
					return fmt.Errorf("naming: unknown placeholder {%s} (see `onefold naming`)", a.name)
				}
			}
		case group:
			for _, alternative := range x {
				if err := validate(alternative); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Render produces the slash-separated relative path ("" if nothing renders).
func (t *Template) Render(v Vars) string {
	raw, _ := t.root.render(v)
	var segs []string
	for _, s := range strings.Split(raw, "/") {
		if s = sanitizeSegment(s); s != "" {
			segs = append(segs, s)
		}
	}
	return strings.Join(segs, "/")
}

// Naming maps selectors (group, kind) to templates ("*" = default).
type Naming struct {
	Name   string
	byKind map[string]*Template
}

// Target renders the relative target path of v.
func (n *Naming) Target(v Vars) string {
	var t *Template
	ok := false
	for _, k := range []string{"." + v.Get("ext-norm"), v.Get("group"), v.Get("kind")} {
		if k != "" && k != "." {
			if t, ok = n.byKind[k]; ok {
				break
			}
		}
	}
	if !ok {
		t = n.byKind["*"]
	}
	if t == nil {
		return ""
	}
	return names.NFC(t.Render(v))
}

// Spec renders the naming back as "kind=template;..." (for reports).
func (n *Naming) Spec() string {
	keys := make([]string, 0, len(n.byKind))
	for k := range n.byKind {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, k+"="+n.byKind[k].src)
	}
	return strings.Join(parts, ";")
}

// Resolve accepts a preset name (optionally with CleanSuffix), a single
// template (applies to every file), or "sel[,sel]=template;...;*=template".
func Resolve(spec string) (*Naming, error) {
	spec = strings.TrimSpace(spec)
	p, ok := Presets[spec]
	if base := strings.TrimSuffix(spec, CleanSuffix); !ok && base != spec {
		if bp, found := Presets[base]; found {
			p, ok = Preset{Spec: reStem.ReplaceAllString(bp.Spec, "${1}stem-clean${2}")}, true
		}
	}
	if ok {
		n, err := parseSpec(p.Spec)
		if err != nil {
			return nil, fmt.Errorf("preset %s: %w", spec, err)
		}
		n.Name = spec
		return n, nil
	}
	if !strings.ContainsAny(spec, "{/\\<") && !strings.Contains(spec, "=") {
		return nil, fmt.Errorf("unknown naming preset %q (see `onefold naming`)", spec)
	}
	n, err := parseSpec(spec)
	if err != nil {
		return nil, err
	}
	n.Name = "custom"
	return n, nil
}

func parseSpec(spec string) (*Naming, error) {
	n := &Naming{byKind: map[string]*Template{}}
	if !strings.Contains(spec, "=") {
		t, err := Parse(spec)
		if err != nil {
			return nil, err
		}
		n.byKind["*"] = t
		return n, nil
	}
	for _, part := range strings.Split(spec, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		sels, tpl, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("naming: %q is not selector=template", part)
		}
		t, err := Parse(strings.TrimSpace(tpl))
		if err != nil {
			return nil, err
		}
		for _, k := range strings.Split(sels, ",") {
			k = strings.TrimSpace(k)
			if !validSelector[k] && !(strings.HasPrefix(k, ".") && kinds.Known(k[1:])) {
				return nil, fmt.Errorf("naming: unknown selector %q (.ext; kinds: %s; groups: %s)", k,
					strings.Join(kinds.Kinds(), " "), strings.Join(kinds.Groups(), " "))
			}
			// Target matches the HARMONIZED extension: a ".jpeg" or ".PDF"
			// selector would be accepted and never apply.
			if e := strings.TrimPrefix(k, "."); e != k && kinds.NormalizeExt(e) != e {
				return nil, fmt.Errorf("naming: selector %q never matches: extension selectors use the harmonized lowercase form (%q)",
					k, "."+kinds.NormalizeExt(e))
			}
			n.byKind[k] = t
		}
	}
	if n.byKind["*"] == nil {
		return nil, fmt.Errorf("naming: a default template '*=' is required")
	}
	return n, nil
}

// reStem matches a {stem} / {stem-norm} placeholder alternative.
var reStem = regexp.MustCompile(`([{|])stem(?:-norm)?([|}])`)

var validSelector = func() map[string]bool {
	m := map[string]bool{"*": true}
	for _, k := range kinds.Kinds() {
		m[k] = true
	}
	for _, g := range kinds.Groups() {
		m[g] = true
	}
	return m
}()

// Layout resolves a historical -layout alias, a preset or a spec.
func Layout(name string) (*Naming, error) {
	if p, ok := LayoutAliases[strings.TrimSpace(name)]; ok {
		name = p
	}
	return Resolve(name)
}

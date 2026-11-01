// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package catalog

import (
	"fmt"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/mauriziofonte/onefold/internal/fsx"
)

// Reasons of the user's exclusions after a preview.
const (
	ReasonExcludePath = "exclude-path" // an excluded file, or a file inside an excluded folder
	ReasonExcludeGlob = "exclude-glob" // the room-relative path matches an excluded pattern
)

// Glob is an exclusion pattern on the room-relative path: `*` within a
// segment, `**` any number of segments, `?`, `[abc]`; `/` and `\` alike; a
// pattern without separator matches any file or folder name (`*.data`,
// `DRIVERS`). Case-insensitive on Windows. A matching folder excludes all it
// holds.
type Glob struct {
	src     string
	segs    []string
	anyName bool
}

// String returns the pattern as given.
func (g Glob) String() string { return g.src }

func foldCase(s string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(s)
	}
	return s
}

// CompileGlob parses an exclusion pattern.
func CompileGlob(p string) (Glob, error) {
	src := strings.TrimSpace(p)
	norm := strings.TrimPrefix(strings.ReplaceAll(foldCase(src), `\`, "/"), "./")
	var segs []string
	for _, s := range strings.Split(norm, "/") {
		if s == "" {
			continue
		}
		if _, err := path.Match(s, ""); err != nil {
			return Glob{}, fmt.Errorf("exclusion pattern %q: %w", src, err)
		}
		segs = append(segs, s)
	}
	if len(segs) == 0 {
		return Glob{}, fmt.Errorf("exclusion pattern %q is empty", src)
	}
	return Glob{src: src, segs: segs, anyName: len(segs) == 1 && segs[0] != "**"}, nil
}

// Match reports whether a room-relative path (any separator) matches.
func (g Glob) Match(rel string) bool {
	segs := strings.Split(strings.Trim(strings.ReplaceAll(foldCase(rel), `\`, "/"), "/"), "/")
	if g.anyName {
		ok, _ := path.Match(g.segs[0], segs[len(segs)-1])
		return ok
	}
	return matchSegs(g.segs, segs)
}

func matchSegs(pat, segs []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			pat = pat[1:]
			if len(pat) == 0 {
				return true
			}
			for i := 0; i <= len(segs); i++ {
				if matchSegs(pat, segs[i:]) {
					return true
				}
			}
			return false
		}
		if len(segs) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], segs[0]); !ok {
			return false
		}
		pat, segs = pat[1:], segs[1:]
	}
	return len(segs) == 0
}

// CompileGlobs compiles every pattern (empty entries skipped).
func CompileGlobs(ps []string) ([]Glob, error) {
	var out []Glob
	for _, p := range ps {
		if strings.TrimSpace(p) == "" {
			continue
		}
		g, err := CompileGlob(p)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}

// CleanExcludePaths gives every excluded path its canonical spelling
// (fsx.Canonical), the one the rooms and their records use: an exclusion
// typed through a link or a short name still matches.
func CleanExcludePaths(ps []string) ([]string, error) {
	var out []string
	for _, p := range ps {
		if strings.TrimSpace(p) == "" {
			continue
		}
		a, err := fsx.Canonical(strings.TrimSpace(p))
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// underPath reports whether p is e or inside e (component boundary,
// case-insensitive on Windows).
func underPath(p, e string) bool {
	fp, fe := foldCase(filepath.Clean(p)), foldCase(filepath.Clean(e))
	if fp == fe {
		return true
	}
	return strings.HasPrefix(fp, strings.TrimSuffix(fe, string(filepath.Separator))+string(filepath.Separator))
}

// UserExclusion returns why the user's exclusions leave out path (a file or
// a folder of room): the substring rule, an excluded path or a pattern; ""
// when none applies. Also used to re-plan on cataloged records.
func (o Options) UserExclusion(room, p string) string {
	rel := strings.TrimPrefix(strings.TrimPrefix(p, filepath.Clean(room)), string(filepath.Separator))
	for _, e := range o.Exclude {
		if e != "" && strings.Contains(rel, e) {
			return ReasonExclude
		}
	}
	if len(o.ExcludePaths) > 0 {
		// the exclusions are canonical (CleanExcludePaths); the path is
		// compared as given and as reached from the canonical room, so a
		// room walked through a link or a short name still matches
		cp := filepath.Join(canonicalCached(room), rel)
		for _, e := range o.ExcludePaths {
			if underPath(p, e) || underPath(cp, canonicalCached(e)) {
				return ReasonExcludePath
			}
		}
	}
	if rel != "" {
		for _, g := range o.ExcludeGlobs {
			if g.Match(rel) {
				return ReasonExcludeGlob
			}
		}
	}
	return ""
}

// HasUserExclusions reports whether any user exclusion is set.
func (o Options) HasUserExclusions() bool {
	return len(o.Exclude) > 0 || len(o.ExcludePaths) > 0 || len(o.ExcludeGlobs) > 0
}

// canonCache caches fsx.Canonical of rooms and excluded paths: UserExclusion
// runs once per file and folder.
var canonCache sync.Map

func canonicalCached(p string) string {
	if c, ok := canonCache.Load(p); ok {
		return c.(string)
	}
	c, err := fsx.Canonical(p)
	if err != nil {
		c = filepath.Clean(p)
	}
	canonCache.Store(p, c)
	return c
}

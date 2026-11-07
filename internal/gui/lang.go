// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package gui

import (
	"sort"
	"strconv"
	"strings"
)

// Langs are the languages of the GUI (internal/gui/web/src/lib/locales).
var Langs = []string{"en", "it", "es", "fr", "de", "pt-BR"}

// PreferredLang picks the UI language from an Accept-Language header: the
// most preferred one the GUI has (any Portuguese: the Brazilian translation),
// English otherwise.
func PreferredLang(header string) string {
	type tag struct {
		lang string
		q    float64
		pos  int
	}
	var tags []tag
	for i, part := range strings.Split(header, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		name := strings.ToLower(strings.TrimSpace(fields[0]))
		if name == "" || name == "*" {
			continue
		}
		q := 1.0
		for _, f := range fields[1:] {
			if v, ok := strings.CutPrefix(strings.TrimSpace(f), "q="); ok {
				if x, err := strconv.ParseFloat(v, 64); err == nil {
					q = x
				}
			}
		}
		tags = append(tags, tag{name, q, i})
	}
	if len(tags) == 0 {
		return "en"
	}
	sort.SliceStable(tags, func(a, b int) bool { return tags[a].q > tags[b].q })
	for _, t := range tags {
		if t.q <= 0 {
			break
		}
		primary, _, _ := strings.Cut(t.lang, "-")
		if primary == "pt" {
			return "pt-BR"
		}
		for _, l := range Langs {
			if l == primary {
				return l
			}
		}
	}
	return "en"
}

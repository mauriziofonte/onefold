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

// PreferredLang picks the UI language from an Accept-Language header: Italian
// when the most preferred language is Italian, English otherwise.
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
	if primary, _, _ := strings.Cut(tags[0].lang, "-"); primary == "it" {
		return "it"
	}
	return "en"
}

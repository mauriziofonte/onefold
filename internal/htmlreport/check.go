// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package htmlreport

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	// Inside a tag: the text of a report is escaped, so "<" opens only tags.
	remote = regexp.MustCompile(`(?i)<[^>]+\s(src|href|xlink:href|action|srcset)\s*=\s*["']?\s*(https?:|//|ftp:)|url\(\s*["']?\s*(https?:|//)|@import`)
	script = regexp.MustCompile(`(?i)<script|<[^>]+\son[a-z]+\s*=|javascript:`)
)

// Check reports why s is not a self-contained report: a script, a request
// to anywhere, no Content-Security-Policy, or a chart with no table under it.
// The tests of every report use it.
func Check(s string) error {
	if !strings.Contains(s, `content="default-src 'none'`) {
		return fmt.Errorf("no Content-Security-Policy forbidding requests")
	}
	if m := script.FindString(s); m != "" {
		return fmt.Errorf("script: %q", m)
	}
	if m := remote.FindString(s); m != "" {
		return fmt.Errorf("network request: %q", m)
	}
	if c, t := strings.Count(s, "<svg"), strings.Count(s, "<details>"); c != t {
		return fmt.Errorf("%d charts but %d tables", c, t)
	}
	return nil
}

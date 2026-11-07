// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package gui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Every translation of the GUI has the keys of en.json, no other, and in each
// string the same placeholders and the same HTML tags: a translation can
// change the words, never what the code fills in or the markup. Langs lists
// exactly the files there are. Only the tags the GUI renders count as markup,
// so path notation such as PICTURES/<year>/ can be translated.
func TestLocales(t *testing.T) {
	dir := filepath.Join("web", "src", "lib", "locales")
	load := func(name string) map[string]string {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]string
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return m
	}
	en := load("en.json")
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	var have []string
	for _, f := range files {
		have = append(have, strings.TrimSuffix(filepath.Base(f), filepath.Ext(f)))
	}
	want := append([]string(nil), Langs...)
	sort.Strings(have)
	sort.Strings(want)
	if strings.Join(have, ",") != strings.Join(want, ",") {
		t.Fatalf("locale files %v, Langs %v", have, want)
	}
	ph := regexp.MustCompile(`\{[a-z][a-zA-Z0-9-]*\}`)
	tag := regexp.MustCompile(`</?(?:b|br|code|em|i|kbd|p|span|strong)(?:\s[^>]*)?>`)
	set := func(re *regexp.Regexp, s string) string {
		m := re.FindAllString(s, -1)
		sort.Strings(m)
		return strings.Join(m, " ")
	}
	for _, l := range Langs {
		if l == "en" {
			continue
		}
		tr := load(l + ".json")
		for k, v := range en {
			s, ok := tr[k]
			if !ok {
				t.Errorf("%s: missing %q", l, k)
				continue
			}
			if v != "" && s == "" {
				t.Errorf("%s: %q is empty", l, k)
			}
			if a, b := set(ph, v), set(ph, s); a != b {
				t.Errorf("%s %q: placeholders %q, English has %q", l, k, b, a)
			}
			if a, b := set(tag, v), set(tag, s); a != b {
				t.Errorf("%s %q: markup %q, English has %q", l, k, b, a)
			}
		}
		for k := range tr {
			if _, ok := en[k]; !ok {
				t.Errorf("%s: %q is not in en.json", l, k)
			}
		}
	}
}

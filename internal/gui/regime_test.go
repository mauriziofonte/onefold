// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package gui

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/mauriziofonte/onefold/internal/catalog"
)

// The server refuses settings that mix the golden archive with merge or
// rearrange, whatever the form sent.
func TestValidateRegimes(t *testing.T) {
	d, _ := filepath.Abs(t.TempDir())
	m := filepath.Join(d, "m.jsonl")
	refused := map[string]string{
		"merge":     `{"inputs":["` + jsonPath(d) + `"],"output":"` + jsonPath(d) + `","filters":{"preset":"media"}}`,
		"rearrange": `{"root":"` + jsonPath(d) + `","naming":"library-files"}`,
		"index":     `{"rooms":["` + jsonPath(d) + `"],"preset":"all"}`,
		"plan":      `{"manifests":["` + jsonPath(m) + `"],"layout":"timeline"}`,
	}
	for kind, raw := range refused {
		if err := validateParams(kind, []byte(raw)); !errors.Is(err, catalog.ErrMixed) {
			t.Errorf("%s: %v, want ErrMixed", kind, err)
		}
	}
	if err := validateParams("plan", []byte(`{"manifests":["`+jsonPath(m)+`"],"fixExt":true}`)); !errors.Is(err, catalog.ErrMixed) {
		t.Errorf("plan fixExt: %v", err)
	}
	accepted := map[string]string{
		"merge":     `{"inputs":["` + jsonPath(d) + `"],"output":"` + jsonPath(d) + `","naming":"music","filters":{"preset":"image,video"}}`,
		"rearrange": `{"root":"` + jsonPath(d) + `","naming":"photo,image={EXT}/{stem}<.{ext}>;*={EXT}/{stem}<.{ext}>"}`,
		"index":     `{"rooms":["` + jsonPath(d) + `"],"filters":{"exclude":"Backup"}}`,
		"plan":      `{"manifests":["` + jsonPath(m) + `"],"layout":"dedupe"}`,
	}
	for kind, raw := range accepted {
		if err := validateParams(kind, []byte(raw)); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
}

// jsonPath escapes a Windows path for a JSON literal.
func jsonPath(p string) string {
	out := make([]byte, 0, len(p))
	for i := 0; i < len(p); i++ {
		if p[i] == '\\' {
			out = append(out, '\\')
		}
		out = append(out, p[i])
	}
	return string(out)
}

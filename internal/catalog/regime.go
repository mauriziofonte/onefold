// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package catalog

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/mauriziofonte/onefold/internal/kinds"
)

// Naming regimes. A golden index catalogs the media kinds only: their
// names are the canonical names of the plan. Merge and rearrange name every
// file with a naming template and never take the golden scope. The GUI server
// always refuses a mix; the CLI does unless -allow-mixed.

// ErrMixed marks settings that mix the golden archive with merge/rearrange.
var ErrMixed = errors.New("settings mix the golden archive with merge/rearrange")

// CheckGoldenScope refuses a golden index scope wider than the media kinds
// (photos and videos, preset media).
func (f Filters) CheckGoldenScope() error {
	preset := f.Preset
	if preset == "" {
		preset = "media"
	}
	exts, err := ResolveExts(preset, f.Ext, f.Category)
	if err != nil {
		return err
	}
	if exts == nil {
		return fmt.Errorf("%w: the golden archive holds photos and videos only, not every file", ErrMixed)
	}
	var bad []string
	for e := range exts {
		if !kinds.IsMedia(kinds.KindOf(e)) {
			bad = append(bad, e)
		}
	}
	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad)
	if len(bad) > 8 {
		bad = append(bad[:8], fmt.Sprintf("and %d more", len(bad)-8))
	}
	return fmt.Errorf("%w: the golden archive holds photos and videos only, not %s", ErrMixed, strings.Join(bad, ", "))
}

// CheckTemplateScope refuses the golden scope (preset media) in merge and
// rearrange, where photos and videos are files like the others.
func (f Filters) CheckTemplateScope() error {
	for _, p := range SplitCSV(f.Preset) {
		if strings.EqualFold(p, "media") {
			return fmt.Errorf("%w: preset media is the golden archive scope; here photos and videos are files like the others: use image,video", ErrMixed)
		}
	}
	return nil
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package catalog

import (
	"errors"
	"testing"
)

func TestRegimeScopes(t *testing.T) {
	for _, c := range []struct {
		f  Filters
		ok bool
	}{
		{Filters{}, true}, // default = media
		{Filters{Preset: "media"}, true},
		{Filters{Preset: "video"}, true}, // narrower is fine
		{Filters{Ext: "jpg,heic,cr2"}, true},
		{Filters{Preset: "all"}, false},
		{Filters{Preset: "image"}, false}, // gif, svg, psd...
		{Filters{Preset: "media,document"}, false},
		{Filters{Ext: "jpg,pdf"}, false},
		{Filters{Preset: "media", Category: "page_layout"}, false},
	} {
		err := c.f.CheckGoldenScope()
		if (err == nil) != c.ok || (err != nil && !errors.Is(err, ErrMixed)) {
			t.Errorf("golden %+v: %v", c.f, err)
		}
	}
	for p, ok := range map[string]bool{"": true, "all": true, "image,video": true, "media": false, "image, MEDIA": false} {
		if err := (Filters{Preset: p}).CheckTemplateScope(); (err == nil) != ok || (err != nil && !errors.Is(err, ErrMixed)) {
			t.Errorf("template %q: %v", p, err)
		}
	}
}

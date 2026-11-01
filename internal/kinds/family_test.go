// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package kinds

import "testing"

// Every category of the registry has a family, so a new category cannot
// silently land in "other".
func TestEveryCategoryHasAFamily(t *testing.T) {
	for _, c := range Categories() {
		if _, ok := familyOfCategory[c]; !ok {
			t.Errorf("category %q has no family: add it to categoriesOf", c)
		}
	}
	n := 0
	for _, cats := range categoriesOf {
		n += len(cats)
	}
	if n != len(familyOfCategory) {
		t.Errorf("a category is listed in two families (%d entries, %d categories)", n, len(familyOfCategory))
	}
	for ext, want := range map[string]string{"jpg": FamPhotos, "cr2": FamPhotos, "mp4": FamVideos, "mp3": FamAudio,
		"pdf": FamDocuments, "docx": FamDocuments, "zip": FamArchives, "go": FamCode, "exe": FamApps,
		"svg": FamDesign, "sqlite": FamData, "": FamOther, "zzzunknown": FamOther} {
		if got := Family(ext); got != want {
			t.Errorf("Family(%q) = %q, want %q", ext, got, want)
		}
	}
}

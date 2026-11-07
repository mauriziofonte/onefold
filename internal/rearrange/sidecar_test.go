// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package rearrange

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/mauriziofonte/onefold/internal/kinds"
)

// With -sidecars an .AAE, .xmp or Takeout .json takes its photo's new name,
// follows the copy that stays when its photo is a duplicate, and is never
// deleted as a duplicate of another sidecar.
func TestSidecarsFollowTheirPhoto(t *testing.T) {
	root := filepath.Join(t.TempDir(), "ROOT")
	put(t, root, "DCIM/IMG_1234.jpg", "photo one", y(2021))
	put(t, root, "DCIM/IMG_1234.AAE", "same edit", y(2021))
	put(t, root, "DCIM/IMG_1234.jpg.json", `{"title":"IMG_1234.jpg"}`, y(2021))
	put(t, root, "DCIM/IMG_5678.jpg", "photo two", y(2021))
	put(t, root, "DCIM/IMG_5678.AAE", "same edit", y(2021))     // same bytes as the other edit
	put(t, root, "OLD/IMG_1234 copy.jpg", "photo one", y(2022)) // a duplicate: deleted
	put(t, root, "OLD/IMG_1234 copy.xmp", "rating 5", y(2022))  // follows the photo that stays
	put(t, root, "OLD/notes.xmp", "no photo", y(2022))          // no owner: out of the scope, stays

	o := opts(root, true)
	o.Catalog.Exts, _, _ = kinds.PresetExts([]string{"image"})
	o.Catalog.Sidecars = true
	res, err := Run(o)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for p := range tree(t, root) {
		got = append(got, p)
	}
	sort.Strings(got)
	want := []string{
		"JPG/2021/IMG_1234.AAE",
		"JPG/2021/IMG_1234.jpg",
		"JPG/2021/IMG_1234.jpg.json",
		"JPG/2021/IMG_1234.xmp",
		"JPG/2021/IMG_5678.AAE",
		"JPG/2021/IMG_5678.jpg",
		"OLD/notes.xmp",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tree\n got %v\nwant %v", got, want)
	}
	if res.Sidecars != 4 || res.Deleted != 1 {
		t.Errorf("sidecars %d deleted %d, want 4 and 1", res.Sidecars, res.Deleted)
	}
	// a second run moves nothing
	again, err := Run(o)
	if err != nil || len(again.Moves) != 0 || len(again.Deletes) != 0 {
		t.Fatalf("second run: %d moves %d deletes, %v", len(again.Moves), len(again.Deletes), err)
	}

	// without the option each file follows its own extension
	root2 := filepath.Join(t.TempDir(), "ROOT")
	put(t, root2, "DCIM/IMG_1234.jpg", "photo one", y(2021))
	put(t, root2, "DCIM/IMG_1234.AAE", "an edit", y(2021))
	o2 := opts(root2, true)
	if _, err := Run(o2); err != nil {
		t.Fatal(err)
	}
	if tr := tree(t, root2); len(tr) != 2 || tr["AAE/2021/IMG_1234.aae"] == "" {
		t.Fatalf("without -sidecars: %v", tr)
	}
}

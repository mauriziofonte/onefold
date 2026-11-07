// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package merge

import (
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/mauriziofonte/onefold/internal/kinds"
	"github.com/mauriziofonte/onefold/internal/testfix"
)

// With -sidecars the edits and metadata of a photo land next to the output
// copy of the photo, under its name: also those of an input duplicate, and
// the same edit file of two different photos is copied for each. A second
// run copies nothing.
func TestMergeSidecars(t *testing.T) {
	root := testfix.TempDir(t)
	O, A, B := filepath.Join(root, "OUT"), filepath.Join(root, "A"), filepath.Join(root, "B")
	write(t, filepath.Join(A, "DCIM/IMG_1.jpg"), "photo one", t2020)
	write(t, filepath.Join(A, "DCIM/IMG_1.AAE"), "same edit", t2020)
	write(t, filepath.Join(A, "DCIM/IMG_2.jpg"), "photo two", t2020)
	write(t, filepath.Join(A, "DCIM/IMG_2.AAE"), "same edit", t2020)      // same bytes, other photo
	write(t, filepath.Join(B, "backup/IMG_1.jpg"), "photo one", t2020)    // input duplicate of A's
	write(t, filepath.Join(B, "backup/IMG_1.AAE"), "same edit", t2020)    // its edit: already planned
	write(t, filepath.Join(B, "backup/IMG_1.jpg.json"), `{"t":1}`, t2020) // its Takeout data: follows
	write(t, filepath.Join(B, "backup/notes.xmp"), "nobody's", t2020)     // out of the scope, no owner
	o := opts([]string{A, B}, O, true)
	o.Catalog.Exts, _, _ = kinds.PresetExts([]string{"image"})
	o.Catalog.Sidecars = true
	res := run(t, o)
	var got []string
	for p := range snap(t, O) {
		got = append(got, p)
	}
	sort.Strings(got)
	want := []string{"JPG/2020/IMG_1.AAE", "JPG/2020/IMG_1.jpg", "JPG/2020/IMG_1.jpg.json", "JPG/2020/IMG_2.AAE", "JPG/2020/IMG_2.jpg"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("output\n got %v\nwant %v", got, want)
	}
	if res.Sidecars != 4 {
		t.Errorf("sidecars %d, want 4", res.Sidecars)
	}
	settled := map[string]string{}
	for _, s := range res.Settled {
		r, _ := filepath.Rel(O, s.Keeper)
		settled[s.Src] = filepath.ToSlash(r)
	}
	if settled[filepath.Join(B, "backup/IMG_1.AAE")] != "JPG/2020/IMG_1.AAE" {
		t.Errorf("the duplicate's edit is not settled on the copy: %v", settled)
	}
	if _, ok := settled[filepath.Join(B, "backup/notes.xmp")]; ok {
		t.Error("a file out of the scope was merged")
	}
	again := run(t, o)
	if len(again.Copies) != 0 {
		t.Fatalf("second run copies %d: %+v", len(again.Copies), again.Copies)
	}
}

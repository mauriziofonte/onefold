// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package materialize

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mauriziofonte/onefold/internal/plan"
)

// An action's sidecars land next to its golden file with their own bytes and
// mtime; a second run writes nothing and never replaces a file.
func TestApplySidecars(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	mt := time.Date(2021, 5, 6, 7, 8, 9, 0, time.UTC)
	for name, body := range map[string]string{"DSC_1.NEF": "raw bytes", "DSC_1.xmp": "<x:xmpmeta/>", "DSC_1.NEF.json": `{"t":1}`} {
		p := filepath.Join(src, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		os.Chtimes(p, mt, mt)
	}
	a := plan.Action{ID: "H", Kind: "photo", SrcHost: "h", SrcPath: filepath.Join(src, "DSC_1.NEF"), SrcSize: 9,
		CanonName: "DSC_20210506_070809.nef", CanonRel: "PICTURES/NEF/2021/DSC_20210506_070809.nef", Time: "2021-05-06 07:08:09",
		Sidecars: []plan.Sidecar{
			{Path: filepath.Join(src, "DSC_1.xmp"), Size: 12, Rel: "PICTURES/NEF/2021/DSC_20210506_070809.xmp"},
			{Path: filepath.Join(src, "DSC_1.NEF.json"), Size: 7, Rel: "PICTURES/NEF/2021/DSC_20210506_070809.nef.json"},
		}}
	st := ApplyAll([]plan.Action{a}, dst, RunOptions{Workers: 1})
	if st.Written != 1 || st.Sidecars != 2 || st.Failed != 0 {
		t.Fatalf("first run: %+v", st)
	}
	x := filepath.Join(dst, "PICTURES/NEF/2021/DSC_20210506_070809.xmp")
	if b, _ := os.ReadFile(x); string(b) != "<x:xmpmeta/>" {
		t.Fatalf("xmp: %q", b)
	}
	if fi, _ := os.Stat(x); !fi.ModTime().Equal(mt) {
		t.Errorf("xmp mtime %v, want the source's %v", fi.ModTime(), mt)
	}
	os.WriteFile(x, []byte("edited in the tree"), 0o644)
	st = ApplyAll([]plan.Action{a}, dst, RunOptions{Workers: 1})
	if st.Written != 0 || st.Sidecars != 0 || st.Skipped != 1 || st.Failed != 0 {
		t.Fatalf("second run: %+v", st)
	}
	if b, _ := os.ReadFile(x); string(b) != "edited in the tree" {
		t.Fatal("a sidecar in the tree was replaced")
	}
	// a sidecar gone from the source fails alone; the rest is written
	os.Remove(filepath.Join(src, "DSC_1.xmp"))
	dst2 := t.TempDir()
	st = ApplyAll([]plan.Action{a}, dst2, RunOptions{Workers: 1})
	if st.Written != 1 || st.Sidecars != 1 || st.Failed != 1 {
		t.Fatalf("missing source: %+v", st)
	}
}

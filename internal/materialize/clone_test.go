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
	"github.com/mauriziofonte/onefold/internal/testfix"
)

// -clone: byte-copied files start as clones where the file system allows it
// (counted), with the bytes and time of a copy; the source is untouched.
func TestApplyClone(t *testing.T) {
	d, capable := testfix.CloneDir(t), true
	if d == "" {
		d, capable = testfix.TempDir(t), false
		t.Log("no clone-capable file system: the byte-copy fallback runs (ONEFOLD_CLONE_DIR names one)")
	}
	body := []byte("a document body, byte-copied into the golden tree")
	src := filepath.Join(d, "src", "a.docx")
	old := time.Date(2001, 1, 1, 0, 0, 0, 0, time.Local)
	if err := testfix.File(src, string(body), old); err != nil {
		t.Fatal(err)
	}
	doc := plan.Action{ID: "d", Kind: "document", SrcPath: src, SrcSize: int64(len(body)),
		CanonRel: "FILES/DOCX/2019/a.docx", Time: "2019-05-06 07:08:09"}
	target := filepath.Join(d, "golden")
	st := ApplyAll([]plan.Action{doc}, target, RunOptions{Clone: true})
	if st.Written != 1 || st.Failed != 0 {
		t.Fatalf("written %d failed %d %v", st.Written, st.Failed, st.Errors)
	}
	if capable && st.Cloned != 1 || !capable && st.Cloned != 0 {
		t.Fatalf("cloned %d (clone-capable: %v)", st.Cloned, capable)
	}
	dst := filepath.Join(target, filepath.FromSlash(doc.CanonRel))
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != string(body) {
		t.Fatalf("golden copy: %q %v", got, err)
	}
	fi, _ := os.Stat(dst)
	if want, _ := parseCanon(doc.Time); !fi.ModTime().Equal(want) {
		t.Errorf("mtime %v, want %v", fi.ModTime(), want)
	}
	if sf, _ := os.Stat(src); !sf.ModTime().Equal(old) {
		t.Error("the source changed")
	}
	noTemps(t, target)
}

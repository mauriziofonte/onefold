// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package materialize

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mauriziofonte/onefold/internal/plan"
)

// -link: the golden name is a hard link of the source (same file, original
// bytes), the source keeps its bytes and mtime, a re-run skips, an existing
// golden file is never replaced, and no onefold temp is left behind.
func TestLinkOriginal(t *testing.T) {
	d := t.TempDir()
	body := []byte("\xff\xd8\xff\xe0 pretend jpeg, never stamped")
	old := time.Date(2001, 1, 1, 0, 0, 0, 0, time.Local)
	mk := func(name string) string {
		p := filepath.Join(d, "src", name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, body, 0o644); err != nil {
			t.Fatal(err)
		}
		os.Chtimes(p, old, old)
		return p
	}
	photo := plan.Action{ID: "p", Kind: "photo", SrcPath: mk("IMG_1.jpg"), SrcSize: int64(len(body)),
		CanonRel: "PICTURES/JPG/2019/IMG_20190506_070809.jpg", Time: "2019-05-06 07:08:09"}
	doc := plan.Action{ID: "d", Kind: "document", SrcPath: mk("a.docx"), SrcSize: int64(len(body)),
		CanonRel: "FILES/DOCX/2019/a.docx", Time: "2019-05-06 07:08:09"}
	target := filepath.Join(d, "golden")
	if err := CheckLink([]plan.Action{photo}, target); err != nil {
		t.Fatalf("CheckLink on one volume: %v", err)
	}
	noTemps(t, target)

	st := ApplyAll([]plan.Action{photo, doc}, target, RunOptions{Workers: 2, Link: true})
	if st.Written != 2 || st.Failed != 0 || st.Unsilvered != 0 {
		t.Fatalf("link run: %+v", st)
	}
	for _, a := range []plan.Action{photo, doc} {
		dst := filepath.Join(target, filepath.FromSlash(a.CanonRel))
		si, err := os.Stat(a.SrcPath)
		if err != nil {
			t.Fatal(err)
		}
		di, err := os.Stat(dst)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(si, di) {
			t.Fatalf("%s is not a hard link of %s", dst, a.SrcPath)
		}
		if b, _ := os.ReadFile(a.SrcPath); !bytes.Equal(b, body) {
			t.Fatalf("%s: source bytes changed", a.SrcPath)
		}
		if !si.ModTime().Equal(old) {
			t.Fatalf("%s: source mtime changed to %v", a.SrcPath, si.ModTime())
		}
		noTemps(t, filepath.Dir(dst))
	}

	again := ApplyAll([]plan.Action{photo, doc}, target, RunOptions{Link: true})
	if again.Skipped != 2 || again.Written != 0 || again.Failed != 0 {
		t.Fatalf("second link run must skip everything: %+v", again)
	}

	// a different file already at the golden name is kept as it is
	other := plan.Action{ID: "o", Kind: "document", SrcPath: mk("b.docx"), SrcSize: int64(len(body)),
		CanonRel: "FILES/DOCX/2019/taken.docx", Time: "2019-05-06 07:08:09"}
	taken := filepath.Join(target, "FILES", "DOCX", "2019", "taken.docx")
	if err := os.WriteFile(taken, []byte("already there"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := LinkOriginal(other, target)
	if err != nil || !res.Skipped {
		t.Fatalf("existing golden name: %v %+v", err, res)
	}
	if b, _ := os.ReadFile(taken); string(b) != "already there" {
		t.Fatal("an existing golden file was replaced")
	}
}

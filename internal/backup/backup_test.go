// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package backup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func put(t *testing.T, p, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Dry run writes nothing; a run copies with the same paths and a manifest;
// a second run adds only what is new and never replaces a different file;
// Check finds bit rot, missing and untracked files.
func TestRunAndCheck(t *testing.T) {
	d := t.TempDir()
	src, dst := filepath.Join(d, "golden"), filepath.Join(d, "cold")
	put(t, filepath.Join(src, "PICTURES", "JPG", "2020", "a.jpg"), "photo a")
	put(t, filepath.Join(src, "FILES", "PDF", "0000", "b.pdf"), "doc b")
	put(t, filepath.Join(src, ".onefold", "state.jsonl"), "state of the tree")
	put(t, filepath.Join(src, "x.onefold-tmp"), "in flight")

	r, err := Run(src, dst, Options{})
	if err != nil || r.Copied != 2 || r.Manifest != "" {
		t.Fatalf("dry run: %+v %v", r, err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("dry run wrote the copy")
	}
	if r, err = Run(src, dst, Options{Execute: true}); err != nil || r.Copied != 2 || r.Manifest == "" || len(r.Failed) > 0 {
		t.Fatalf("run: %+v %v", r, err)
	}
	if read(t, filepath.Join(dst, "PICTURES", "JPG", "2020", "a.jpg")) != "photo a" {
		t.Fatal("copy differs")
	}
	if _, err := os.Stat(filepath.Join(dst, ".onefold", "state.jsonl")); !os.IsNotExist(err) {
		t.Fatal("the tree's own state was copied")
	}
	a, err := Check(dst, 2, nil)
	if err != nil || !a.Clean() || a.OK != 2 || len(a.Untracked) != 0 {
		t.Fatalf("check: %+v %v", a, err)
	}

	// new file in the tree, a file of the copy changed under the same path with another size
	put(t, filepath.Join(src, "VIDEOS", "2021", "v.mp4"), "video")
	put(t, filepath.Join(dst, "FILES", "PDF", "0000", "b.pdf"), "someone else's doc")
	if r, err = Run(src, dst, Options{Execute: true}); err != nil || r.Copied != 1 || r.Present != 1 || len(r.Conflicts) != 1 {
		t.Fatalf("second run: %+v %v", r, err)
	}
	if read(t, filepath.Join(dst, "FILES", "PDF", "0000", "b.pdf")) != "someone else's doc" {
		t.Fatal("a file of the copy was replaced")
	}

	// bit rot (same size), a missing file, an untracked one
	put(t, filepath.Join(dst, "PICTURES", "JPG", "2020", "a.jpg"), "photo A")
	os.Remove(filepath.Join(dst, "VIDEOS", "2021", "v.mp4"))
	put(t, filepath.Join(dst, "notes.txt"), "mine")
	a, err = Check(dst, 2, nil)
	if err != nil || a.Clean() || a.Manifests != 2 {
		t.Fatalf("check after damage: %+v %v", a, err)
	}
	got := strings.Join(a.Changed, ",") + "|" + strings.Join(a.Missing, ",") + "|" + strings.Join(a.Untracked, ",")
	// b.pdf: the first run recorded it, the copy now holds another file
	if got != "FILES/PDF/0000/b.pdf,PICTURES/JPG/2020/a.jpg|VIDEOS/2021/v.mp4|notes.txt" {
		t.Fatalf("audit %s", got)
	}
	if _, err := Run(src, filepath.Join(src, "inside"), Options{}); err == nil {
		t.Error("a copy inside the tree accepted")
	}
	if _, err := Check(t.TempDir(), 1, nil); err == nil {
		t.Error("check without manifests accepted")
	}
}

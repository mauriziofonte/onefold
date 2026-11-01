// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package seed

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mauriziofonte/onefold/internal/plan"
)

func put(t *testing.T, p, body string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Only the plan's files present in the staging tree are listed; rsync
// copies them and never replaces a file of the destination; robocopy lines
// group the files per folder with the no-replace switches.
func TestSeedScripts(t *testing.T) {
	d := t.TempDir()
	stage, dest := filepath.Join(d, "stage"), filepath.Join(d, "dest")
	put(t, filepath.Join(stage, "PICTURES/JPG/2020/a.jpg"), "a")
	put(t, filepath.Join(stage, "PICTURES/JPG/2020/it's.jpg"), "quote")
	put(t, filepath.Join(stage, "FILES/PDF/0000/b.pdf"), "b")
	put(t, filepath.Join(stage, "not-in-plan.txt"), "x")
	put(t, filepath.Join(dest, "FILES/PDF/0000/b.pdf"), "already there, different")
	acts := []plan.Action{{CanonRel: "PICTURES/JPG/2020/a.jpg"}, {CanonRel: "PICTURES/JPG/2020/it's.jpg"},
		{CanonRel: "FILES/PDF/0000/b.pdf"}, {CanonRel: "VIDEOS/2021/v.mp4"}}
	rels, res := Collect(acts, stage, "")
	if res.Files != 3 || len(res.Missing) != 1 || res.Missing[0] != "VIDEOS/2021/v.mp4" {
		t.Fatalf("%+v %v", res, rels)
	}
	var rb bytes.Buffer
	if err := Robocopy(&rb, rels, `C:\stage`, `\\nas\golden`); err != nil {
		t.Fatal(err)
	}
	r := rb.String()
	for _, want := range []string{
		`robocopy "C:\stage\PICTURES\JPG\2020" "\\nas\golden\PICTURES\JPG\2020" "a.jpg" "it's.jpg" /XC /XN /XO`,
		`robocopy "C:\stage\FILES\PDF\0000" "\\nas\golden\FILES\PDF\0000" "b.pdf" /XC /XN /XO`,
	} {
		if !strings.Contains(r, want) {
			t.Errorf("robocopy script lacks %s:\n%s", want, r)
		}
	}
	if strings.Contains(r, "not-in-plan") {
		t.Error("a file outside the plan in the script")
	}
	if runtime.GOOS == "windows" {
		// the real robocopy on the real paths: copies, never replaces
		var wb bytes.Buffer
		if err := Robocopy(&wb, rels, stage, dest); err != nil {
			t.Fatal(err)
		}
		cmdPath := filepath.Join(d, "seed.cmd")
		os.WriteFile(cmdPath, wb.Bytes(), 0o644)
		if out, err := exec.Command("cmd", "/c", cmdPath).CombinedOutput(); err != nil {
			t.Fatalf("robocopy script: %v\n%s", err, out)
		}
		checkDest(t, dest)
		return
	}
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("rsync not installed: the sh script is not run")
	}
	listPath, scriptPath := filepath.Join(d, "seed.files"), filepath.Join(d, "seed.sh")
	var sh, list bytes.Buffer
	if err := Rsync(&sh, &list, rels, stage, dest, listPath); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(listPath, list.Bytes(), 0o644)
	os.WriteFile(scriptPath, sh.Bytes(), 0o755)
	if out, err := exec.Command("sh", scriptPath).CombinedOutput(); err != nil {
		t.Fatalf("rsync script: %v\n%s", err, out)
	}
	checkDest(t, dest)
}

func checkDest(t *testing.T, dest string) {
	t.Helper()
	for rel, want := range map[string]string{"PICTURES/JPG/2020/a.jpg": "a", "PICTURES/JPG/2020/it's.jpg": "quote", "FILES/PDF/0000/b.pdf": "already there, different"} {
		b, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(rel)))
		if err != nil || string(b) != want {
			t.Errorf("%s: %q %v", rel, b, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dest, "not-in-plan.txt")); err == nil {
		t.Error("a file outside the plan was copied")
	}
}

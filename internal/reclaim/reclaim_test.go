// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package reclaim

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

func write(t *testing.T, p, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// Exact duplicates verified now go to the script; a "duplicate" whose
// content changed, one whose winner is not in the golden tree, or on another
// host is skipped with the reason; lower-res drops are for review. Check
// itself deletes nothing.
func TestCheckAndScript(t *testing.T) {
	d := t.TempDir()
	src, G := filepath.Join(d, "src"), filepath.Join(d, "golden")
	win := write(t, filepath.Join(src, "a.txt"), "content A")
	dup := write(t, filepath.Join(src, "copy of a's.txt"), "content A")
	changed := write(t, filepath.Join(src, "copy2.txt"), "content X") // same size, other bytes
	low := write(t, filepath.Join(src, "small.jpg"), "lq")
	win2 := write(t, filepath.Join(src, "b.txt"), "content B")
	dup2 := write(t, filepath.Join(src, "b copy.txt"), "content B")
	write(t, filepath.Join(G, "FILES", "a.txt"), "content A")
	acts := []plan.Action{
		{SrcHost: "h", SrcPath: win, CanonRel: "FILES/a.txt", Drops: []plan.Drop{
			{Host: "h", Path: dup, Reason: "exact-dupe", Size: 9}, {Host: "h", Path: changed, Reason: "exact-dupe", Size: 9},
			{Host: "h", Path: low, Reason: "lower-res", Size: 2}, {Host: "other", Path: "/elsewhere/a.txt", Reason: "exact-dupe", Size: 9},
			{Host: "h", Path: "/pkg/__init__.py", Reason: "exact-dupe", Size: 0}}},
		{SrcHost: "h", SrcPath: win2, CanonRel: "FILES/b.txt", Drops: []plan.Drop{{Host: "h", Path: dup2, Reason: "exact-dupe", Size: 9}}},
	}
	r, err := Check(acts, Options{Target: G})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Safe) != 1 || r.Safe[0].Path != dup || r.SafeBytes != 9 {
		t.Fatalf("safe %+v", r.Safe)
	}
	if len(r.Review) != 1 || r.Review[0].Path != low {
		t.Fatalf("review %+v", r.Review)
	}
	why := map[string]string{}
	for _, it := range r.Skipped {
		why[it.Path] = it.Why
	}
	if !strings.Contains(why[changed], "NOT a duplicate") || !strings.Contains(why[dup2], "golden copy") || !strings.Contains(why["/elsewhere/a.txt"], "another host") || !strings.Contains(why["/pkg/__init__.py"], "empty file") {
		t.Fatalf("skipped %v", why)
	}
	for _, p := range []string{win, dup, changed, low, win2, dup2} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("check deleted %s", p)
		}
	}
	if r2, _ := Check(acts, Options{Target: G, OnlyHost: "other"}); len(r2.Safe)+len(r2.Review)+len(r2.Skipped) != 1 {
		t.Fatalf("only-host: %+v", r2)
	}
	// the script deletes exactly the safe drop, whatever its name holds
	var sh bytes.Buffer
	if err := Script(&sh, r.Safe, false); err != nil {
		t.Fatal(err)
	}
	var ps bytes.Buffer
	Script(&ps, r.Safe, true)
	if !strings.Contains(ps.String(), "Remove-Item -LiteralPath '"+strings.ReplaceAll(dup, "'", "''")+"'") {
		t.Fatalf("powershell script:\n%s", ps.String())
	}
	if runtime.GOOS == "windows" {
		return
	}
	p := filepath.Join(d, "reclaim.sh")
	os.WriteFile(p, sh.Bytes(), 0o755)
	if out, err := exec.Command("sh", p).CombinedOutput(); err != nil {
		t.Fatalf("script: %v\n%s", err, out)
	}
	if _, err := os.Stat(dup); !os.IsNotExist(err) {
		t.Fatal("the script did not delete the duplicate")
	}
	for _, p := range []string{win, changed, low, win2, dup2} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("the script deleted %s", p)
		}
	}
}

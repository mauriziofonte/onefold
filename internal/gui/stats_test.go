// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package gui

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/mauriziofonte/onefold/internal/stats"
)

// A stats job measures two folders, finds the copy shared between them,
// serves its result and stays read-only; bad parameters are refused.
func TestStatsJob(t *testing.T) {
	s, c := newTestServer(t)
	root := t.TempDir()
	a, b := filepath.Join(root, "A"), filepath.Join(root, "B")
	writeFile(t, filepath.Join(a, "photo.jpg"), "same picture bytes")
	writeFile(t, filepath.Join(a, "notes.txt"), "only here")
	writeFile(t, filepath.Join(b, "copy.jpg"), "same picture bytes")
	writeFile(t, filepath.Join(b, "song.mp3"), "a song")

	for _, bad := range []map[string]any{
		{"roots": []string{}},
		{"roots": []string{"relative/dir"}},
		{"roots": []string{a}, "dups": "maybe"},
	} {
		var e map[string]string
		if code := postJSON(t, c, base(s)+"/api/jobs", map[string]any{"kind": "stats", "params": bad}, &e); code < 400 || e["error"] == "" {
			t.Fatalf("params %v accepted: %d %v", bad, code, e)
		}
	}

	var j Job
	if code := postJSON(t, c, base(s)+"/api/jobs", map[string]any{"kind": "stats", "params": map[string]any{"roots": []string{a, b}, "dups": "full"}}, &j); code != 200 {
		t.Fatalf("start: %d", code)
	}
	if j.Stage != "execute" || len(j.Phases) != 2 {
		t.Fatalf("job %+v", j)
	}
	done := waitStatus(t, s.mgr, j.ID, stDone)
	if fmt.Sprint(done.Summary["files"]) != "4" {
		t.Fatalf("summary %v", done.Summary)
	}

	var r stats.Result
	if code := getJSON(t, c, base(s)+"/api/jobs/"+j.ID+"/stats", &r); code != 200 {
		t.Fatalf("GET stats: %d", code)
	}
	if r.Files != 4 || len(r.Roots) != 2 || r.Partial || r.Phase != "done" {
		t.Fatalf("result files %d roots %v partial %v phase %q", r.Files, r.Roots, r.Partial, r.Phase)
	}
	if r.Dups == nil || r.Dups.Groups != 1 || r.Dups.Extra != 1 || !r.Dups.Top[0].Verified {
		t.Fatalf("dups %+v", r.Dups)
	}
	if len(r.Dups.Pairs) != 1 || r.Dups.Pairs[0].A != a || r.Dups.Pairs[0].B != b {
		t.Fatalf("pairs %+v", r.Dups.Pairs)
	}
	if countFiles(root) != 4 {
		t.Fatal("a stats job changed the folders")
	}

	// an unknown job has no stats result
	if code := getJSON(t, c, base(s)+"/api/jobs/nope/stats", nil); code != 404 {
		t.Fatalf("unknown job: %d", code)
	}

	var m struct {
		Mounts []stats.Mount `json:"mounts"`
	}
	if code := getJSON(t, c, base(s)+"/api/mounts", &m); code != 200 {
		t.Fatalf("GET mounts: %d", code)
	}
}

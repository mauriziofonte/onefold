// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package plan

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mauriziofonte/onefold/internal/catalog"
)

// baselineSortedPlanMD5 is the md5 of the sorted plan JSONL produced by the
// pre-generic-files binary on the census manifests below, in this order.
const baselineSortedPlanMD5 = "782f78c205a4399a05caadea489ac2fc"

// Historical manifests (no new fields) must plan byte-identically.
func TestMediaPlanRegression(t *testing.T) {
	dir := filepath.Join("..", "..", "census-final")
	names := []string{"hnas-pictures.jsonl", "hnas-videos.jsonl", "win-pictures.jsonl", "win-tmp.jsonl", "win-videos.jsonl"}
	var recs []catalog.Record
	for _, n := range names {
		rs, _, err := catalog.LoadManifestRecords(filepath.Join(dir, n)) // fixture: written before the zone header
		if err != nil {
			if os.IsNotExist(err) {
				t.Skip("census-final manifests not present")
			}
			t.Fatal(err)
		}
		// fixture written before host labels were mandatory: the labels
		// of that run, in memory (the census is never edited)
		host, _, _ := strings.Cut(n, "-")
		if host == "win" {
			host = "windows"
		}
		for i := range rs {
			rs[i].Host = host
		}
		recs = append(recs, rs...)
	}
	// the owner stamped by that run (no default owner since the per-user
	// config): read from the run's own plan, part of the same fixture
	acts := BuildActionsWith(Plan(recs), Options{Artist: censusArtist(t, dir)})
	lines := make([]string, len(acts))
	for i := range acts {
		b, _ := json.Marshal(&acts[i])
		lines[i] = string(b)
	}
	sort.Strings(lines)
	sum := md5.Sum([]byte(strings.Join(lines, "\n") + "\n"))
	if got := hex.EncodeToString(sum[:]); got != baselineSortedPlanMD5 {
		t.Fatalf("media plan changed: sorted md5 %s, want %s", got, baselineSortedPlanMD5)
	}
}

// Generic golden names are frozen too: testdata/generic-synth.jsonl (every
// generic kind, legacy name markers, undated, reserved names, sniffed ext)
// must plan to the paths the pre-registry binary produced, per layout.
func TestGenericPlanRegression(t *testing.T) {
	recs, _, err := catalog.LoadManifestRecords(filepath.Join("testdata", "generic-synth.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"dedupe": "9535723ab27ace80949c30696ada88a8", "dedupe+fix": "fcd4ceaf827ef44ac6c5335d9c3c2de8",
		"timeline": "5394e466264f42854dee2592ad0750de", "timeline+fix": "16b9508d4e12593efb372a52e6745231",
		"document": "62bb6364d9254a355dfd9c22e740f78f", "document+fix": "9aeb08f9c6b566a09395b9a5312c34b5",
		"music": "f2d9acda51add2f5c8edd52b9a709693", "music+fix": "afd5539c3da11d9521672c9b9201fc87",
		"device": "23c5ac2b2d6ff666c107ce6954b08223", "device+fix": "6d3669195501e7221db2944211727d0d",
	}
	for key, sum := range want {
		layout, fix := strings.CutSuffix(key, "+fix")
		o := Options{Layout: layout, FixExt: fix}
		acts := BuildActionsWith(PlanWith(append([]catalog.Record(nil), recs...), o), o)
		lines := make([]string, len(acts))
		for i, a := range acts {
			lines[i] = a.SrcPath + "\t" + a.CanonRel
		}
		sort.Strings(lines)
		got := md5.Sum([]byte(strings.Join(lines, "\n") + "\n"))
		if hex.EncodeToString(got[:]) != sum {
			t.Errorf("%s: generic golden names changed", key)
		}
	}
}

// censusArtist is the artist of the first media action of mater-plan.jsonl,
// the plan the census run applied.
func censusArtist(t *testing.T, dir string) string {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, "mater-plan.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		var a struct {
			Artist string `json:"artist"`
		}
		if json.Unmarshal(sc.Bytes(), &a) == nil && a.Artist != "" {
			return a.Artist
		}
	}
	t.Fatal("mater-plan.jsonl: no media action with an artist")
	return ""
}

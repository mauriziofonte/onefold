// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package gui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mauriziofonte/onefold/internal/config"
	"github.com/mauriziofonte/onefold/internal/plan"
)

func TestConfigAPI(t *testing.T) {
	if !loopbackOK() {
		t.Skip("this host blocks loopback connections for this process (environment, see TRAPS)")
	}
	cfgPath := filepath.Join(t.TempDir(), config.FileName)
	var seen []config.Config
	s, err := New(Options{Port: -1, StateDir: t.TempDir(), Version: "v1.2.3", ConfigPath: cfgPath,
		OnConfig: func(c config.Config) { seen = append(seen, c) },
		Static:   fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}}})
	if err != nil {
		t.Fatal(err)
	}
	go s.Serve()
	t.Cleanup(s.Shutdown)
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	if resp, err := c.Get(s.URL()); err != nil || resp.StatusCode != 200 {
		t.Fatalf("session link: %v %v", err, resp)
	}
	var got struct {
		Config             config.Config `json:"config"`
		DefaultSignature   string        `json:"defaultSignature"`
		EffectiveSignature string        `json:"effectiveSignature"`
		Path               string        `json:"path"`
	}
	if getJSON(t, c, base(s)+"/api/config", &got); got.DefaultSignature != "Onefold v1.2.3" || got.EffectiveSignature != "Onefold v1.2.3" || got.Path != cfgPath {
		t.Fatalf("defaults: %+v", got)
	}
	want := config.Config{Artist: "Jane Doe", Signature: "My archive", Language: "eng", DiagLog: true}
	if code := postJSON(t, c, base(s)+"/api/config", want, &got); code != 200 || got.Config != want || got.EffectiveSignature != "My archive" {
		t.Fatalf("save: %d %+v", code, got)
	}
	if len(seen) != 1 || seen[0] != want {
		t.Fatalf("OnConfig: %+v", seen)
	}
	if code := postJSON(t, c, base(s)+"/api/config", config.Config{Language: "english"}, nil); code == 200 {
		t.Fatal("invalid language saved")
	}
	if onDisk, err := config.LoadFrom(cfgPath); err != nil || onDisk != want {
		t.Fatalf("on disk: %+v %v", onDisk, err)
	}
}

// Plan and apply jobs take the owner, signature and language from the config
// and record them in their params, so execute and resume stamp what the
// preview showed.
func TestJobsUseConfig(t *testing.T) {
	m := newTestManager(t)
	m.cfgPath, m.version = filepath.Join(t.TempDir(), config.FileName), "v1.2.3"
	if err := config.SaveTo(m.cfgPath, config.Config{Artist: "Jane Doe", Language: "eng"}); err != nil {
		t.Fatal(err)
	}
	d := t.TempDir()
	src := filepath.Join(d, "a.txt")
	writeFile(t, src, "x")
	pl := filepath.Join(d, "plan.jsonl")
	f, _ := os.Create(pl)
	plan.Write(f, []plan.Action{{ID: "a", Kind: "text", SrcHost: "h", SrcPath: src, SrcSize: 1, CanonRel: "FILES/TXT/2020/a.txt", Time: "2020-01-02 03:04:05"}})
	f.Close()
	raw, _ := json.Marshal(ApplyParams{Plan: pl, Target: filepath.Join(d, "golden")})
	j, err := m.Start("apply", raw)
	if err != nil {
		t.Fatal(err)
	}
	j = waitStatus(t, m, j.ID, stPreviewed)
	var p ApplyParams
	json.Unmarshal(j.Params, &p)
	if p.Signature != "Onefold v1.2.3" || p.Language != "eng" {
		t.Fatalf("apply params from config: %+v", p)
	}
}

// Without exiftool/ffmpeg the apply preview lists the missing tools and the
// execute refuses to start, unless the params allow unstamped copies.
func TestApplyNeedsTools(t *testing.T) {
	t.Setenv("ONEFOLD_NO_EXTERNAL", "1")
	m := newTestManager(t)
	d := t.TempDir()
	src := filepath.Join(d, "IMG_1.jpg")
	writeFile(t, src, "not really a jpeg")
	pl := filepath.Join(d, "plan.jsonl")
	f, _ := os.Create(pl)
	plan.Write(f, []plan.Action{{ID: "a", Kind: "photo", SrcHost: "h", SrcPath: src, SrcSize: 17, CanonRel: "PICTURES/JPG/2020/IMG_20200102_030405.jpg", Time: "2020-01-02 03:04:05"}})
	f.Close()
	for _, allow := range []bool{false, true} {
		target := filepath.Join(d, fmt.Sprint("golden-", allow))
		raw, _ := json.Marshal(ApplyParams{Plan: pl, Target: target, AllowUnstamped: allow})
		j, err := m.Start("apply", raw)
		if err != nil {
			t.Fatal(err)
		}
		j = waitStatus(t, m, j.ID, stPreviewed)
		if got := fmt.Sprint(j.Summary["missingTools"]); got != "[exiftool]" {
			t.Fatalf("missingTools: %s", got)
		}
		if err := m.Execute(j.ID, 0); err != nil {
			t.Fatal(err)
		}
		if !allow {
			j = waitStatus(t, m, j.ID, stFailed)
			_, statErr := os.Stat(filepath.Join(target, "PICTURES", "JPG", "2020", "IMG_20200102_030405.jpg"))
			if !strings.Contains(j.Error, "exiftool not found") || statErr == nil {
				t.Fatalf("refused execute: %q, wrote: %v", j.Error, statErr == nil)
			}
			continue
		}
		j = waitStatus(t, m, j.ID, stDone)
		if _, err := os.Stat(filepath.Join(target, "PICTURES", "JPG", "2020", "IMG_20200102_030405.jpg")); err != nil || asInt(j.Summary["unsilvered"]) != 1 {
			t.Fatalf("allowed execute: %v, summary %v", err, j.Summary)
		}
	}
}

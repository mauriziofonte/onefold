// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package gui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/mauriziofonte/onefold/internal/naming"
	"github.com/mauriziofonte/onefold/internal/testfix"
	"github.com/mauriziofonte/onefold/internal/tooldl"
)

func TestPreferredLang(t *testing.T) {
	for in, want := range map[string]string{
		"it-IT,it;q=0.9,en-US;q=0.8": "it", "it": "it", "en-US,en;q=0.9,it;q=0.8": "en",
		"de-DE,de;q=0.9": "en", "": "en", "fr;q=0.5,it;q=0.9": "it", "*": "en",
	} {
		if got := PreferredLang(in); got != want {
			t.Errorf("%q -> %s, want %s", in, got, want)
		}
	}
}

// loopbackOK reports whether this process can reach its own 127.0.0.1
// listener. Some Windows hosts block it for programs launched from WSL
// (WSAEACCES): the HTTP layer is then verified on Linux and by hand.
func loopbackOK() bool {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return false
	}
	defer ln.Close()
	go func() {
		if c, err := ln.Accept(); err == nil {
			c.Close()
		}
	}()
	c, err := net.DialTimeout("tcp", ln.Addr().String(), 2*time.Second)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

func newTestServer(t *testing.T) (*Server, *http.Client) {
	t.Helper()
	if !loopbackOK() {
		t.Skip("this host blocks loopback connections for this process (environment, see TRAPS)")
	}
	s, err := New(Options{Port: -1, StateDir: testfix.TempDir(t), Version: "test",
		Static: fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>onefold</title>")}}})
	if err != nil {
		t.Fatal(err)
	}
	go s.Serve()
	t.Cleanup(s.Shutdown)
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	resp, err := c.Get(s.URL())
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("session link: %v %v", err, resp)
	}
	resp.Body.Close()
	return s, c
}

func base(s *Server) string { return "http://" + s.Addr() }

func getJSON(t *testing.T, c *http.Client, url string, v any) int {
	t.Helper()
	resp, err := c.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if v != nil {
		json.NewDecoder(resp.Body).Decode(v)
	}
	return resp.StatusCode
}

func postJSON(t *testing.T, c *http.Client, url string, body any, v any) int {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := c.Post(url, "application/json", strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if v != nil {
		json.NewDecoder(resp.Body).Decode(v)
	}
	return resp.StatusCode
}

func TestGuard(t *testing.T) {
	s, c := newTestServer(t)
	// no cookie
	resp, _ := http.Get(base(s) + "/api/info")
	if resp.StatusCode != 403 {
		t.Fatalf("no cookie: %d", resp.StatusCode)
	}
	resp.Body.Close()
	// wrong token
	resp, _ = http.Get(base(s) + "/?t=nope")
	if resp.StatusCode != 403 {
		t.Fatalf("bad token: %d", resp.StatusCode)
	}
	resp.Body.Close()
	// foreign Host header (DNS rebinding)
	req, _ := http.NewRequest("GET", base(s)+"/api/info", nil)
	req.Host = "evil.example:80"
	resp, _ = c.Do(req)
	if resp.StatusCode != 403 {
		t.Fatalf("bad host: %d", resp.StatusCode)
	}
	resp.Body.Close()
	// cross-origin POST
	req, _ = http.NewRequest("POST", base(s)+"/api/jobs", strings.NewReader("{}"))
	req.Header.Set("Origin", "http://evil.example")
	resp, _ = c.Do(req)
	if resp.StatusCode != 403 {
		t.Fatalf("cross origin: %d", resp.StatusCode)
	}
	resp.Body.Close()
	// with the session cookie
	var info map[string]any
	req, _ = http.NewRequest("GET", base(s)+"/api/info", nil)
	req.Header.Set("Accept-Language", "it-IT,it;q=0.9")
	resp, _ = c.Do(req)
	json.NewDecoder(resp.Body).Decode(&info)
	resp.Body.Close()
	if resp.StatusCode != 200 || info["lang"] != "it" || info["version"] != "test" {
		t.Fatalf("info: %d %v", resp.StatusCode, info)
	}
	// static page
	resp, _ = c.Get(base(s) + "/")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "onefold") {
		t.Fatalf("static: %s", body)
	}
}

func writeFile(t *testing.T, p, body string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	mt := time.Date(2019, 3, 4, 10, 0, 0, 0, time.Local)
	os.Chtimes(p, mt, mt)
}

// mergeTree: an output with one file, an input with new and duplicate files.
func mergeTree(t *testing.T, n int) (out, in string) {
	root := testfix.TempDir(t)
	out, in = filepath.Join(root, "OUT"), filepath.Join(root, "IN")
	writeFile(t, filepath.Join(out, "TXT", "2019", "have.txt"), "already there")
	writeFile(t, filepath.Join(in, "a", "have-copy.txt"), "already there")
	for i := 0; i < n; i++ {
		writeFile(t, filepath.Join(in, "docs", fmt.Sprintf("note%04d.txt", i)), fmt.Sprintf("note body %d", i))
	}
	writeFile(t, filepath.Join(in, "b", "note0000.txt"), "note body 0") // input duplicate
	return
}

func waitStatus(t *testing.T, m *Manager, id string, want ...string) *Job {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		j, _ := m.Get(id)
		for _, w := range want {
			if j.Status == w {
				return j
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	j, _ := m.Get(id)
	t.Fatalf("job %s: status %s (error %q), want %v", id, j.Status, j.Error, want)
	return nil
}

func countFiles(root string) int {
	n := 0
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && !strings.Contains(p, string(filepath.Separator)+".onefold") {
			n++
		}
		return nil
	})
	return n
}

func TestMergeJobPreviewExecute(t *testing.T) {
	s, c := newTestServer(t)
	out, in := mergeTree(t, 20)
	params := MergeParams{Inputs: []string{in}, Output: out}
	var j Job
	if code := postJSON(t, c, base(s)+"/api/jobs", map[string]any{"kind": "merge", "params": params}, &j); code != 200 {
		t.Fatalf("start: %d %+v", code, j)
	}
	pj := waitStatus(t, s.mgr, j.ID, stPreviewed)
	if asInt(pj.Summary["toCopy"]) != 20 || asInt(pj.Summary["alreadyPresent"]) != 1 || asInt(pj.Summary["inputDupes"]) != 1 {
		t.Fatalf("preview summary: %v", pj.Summary)
	}
	if countFiles(out) != 1 {
		t.Fatal("preview wrote into the output")
	}
	if _, err := os.Stat(filepath.Join(out, ".onefold")); !os.IsNotExist(err) {
		t.Fatal("preview created .onefold in the output")
	}
	var items struct {
		Items []Item `json:"items"`
		Total int    `json:"total"`
	}
	getJSON(t, c, base(s)+"/api/jobs/"+j.ID+"/items?list=copies&q=note0001", &items)
	if items.Total != 1 || !strings.Contains(items.Items[0].Dst, "note0001.txt") {
		t.Fatalf("items: %+v", items)
	}
	if code := postJSON(t, c, base(s)+"/api/jobs/"+j.ID+"/execute", map[string]any{}, nil); code != 200 {
		t.Fatalf("execute: %d", code)
	}
	ej := waitStatus(t, s.mgr, j.ID, stDone, stFailed)
	if ej.Status != stDone || asInt(ej.Summary["copied"]) != 20 {
		t.Fatalf("execute: %s %q %v", ej.Status, ej.Error, ej.Summary)
	}
	if countFiles(out) != 21 {
		t.Fatalf("output has %d files", countFiles(out))
	}
	// the job and its result survive a restart
	var list struct {
		Jobs []Job `json:"jobs"`
	}
	m2 := newManager(&store{dir: s.opts.StateDir}, newHub())
	if j2, ok := m2.Get(j.ID); !ok || j2.Status != stDone {
		t.Fatalf("reloaded job: %+v", j2)
	}
	getJSON(t, c, base(s)+"/api/jobs", &list)
	if len(list.Jobs) != 1 {
		t.Fatalf("jobs: %+v", list)
	}
}

// The executed plan must be the confirmed one: a change after the preview
// fails the execute with nothing written.
func newTestManager(t *testing.T) *Manager {
	return newManager(&store{dir: testfix.TempDir(t)}, newHub())
}

func TestExecuteRefusedWhenFoldersChanged(t *testing.T) {
	s := struct{ mgr *Manager }{newTestManager(t)}
	out, in := mergeTree(t, 5)
	b, _ := json.Marshal(MergeParams{Inputs: []string{in}, Output: out})
	j, err := s.mgr.Start("merge", b)
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, s.mgr, j.ID, stPreviewed)
	writeFile(t, filepath.Join(in, "late", "new.txt"), "appeared after the preview")
	if err := s.mgr.Execute(j.ID, 0); err != nil {
		t.Fatal(err)
	}
	fj := waitStatus(t, s.mgr, j.ID, stFailed, stDone)
	if fj.Status != stFailed || !strings.Contains(fj.Error, "changed since the preview") {
		t.Fatalf("want refusal, got %s %q", fj.Status, fj.Error)
	}
	if countFiles(out) != 1 {
		t.Fatal("refused execute wrote files")
	}
}

// Pause holds the run, Resume completes it.
func TestPauseResume(t *testing.T) {
	s := struct{ mgr *Manager }{newTestManager(t)}
	out, in := mergeTree(t, 1500)
	b, _ := json.Marshal(MergeParams{Inputs: []string{in}, Output: out, Workers: 1})
	j, err := s.mgr.Start("merge", b)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.mgr.Pause(j.ID); err != nil {
		t.Fatal(err)
	}
	pj := waitStatus(t, s.mgr, j.ID, stPaused)
	time.Sleep(150 * time.Millisecond)
	if again, _ := s.mgr.Get(j.ID); again.Status != stPaused || pj.Progress == nil {
		t.Fatalf("not held: %+v", again)
	}
	if err := s.mgr.Resume(j.ID); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, s.mgr, j.ID, stPreviewed)
}

// A run cut by a restart is listed as interrupted and Continue completes it.
func TestInterruptedJobResumes(t *testing.T) {
	dir := testfix.TempDir(t)
	out, in := mergeTree(t, 30)
	st := &store{dir: dir}
	m := newManager(st, newHub())
	b, _ := json.Marshal(MergeParams{Inputs: []string{in}, Output: out})
	j, err := m.Start("merge", b)
	if err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, j.ID, stPreviewed)
	// simulate a power loss in the middle of the execute: the persisted job
	// says running, part of the copies exist
	jj, _ := m.Get(j.ID)
	jj.Stage, jj.Status = "execute", stRunning
	st.saveJob(jj)
	writeFile(t, filepath.Join(out, "TXT", "2019", "note0003.txt"), "note body 3")

	m2 := newManager(st, newHub())
	ij, _ := m2.Get(j.ID)
	if ij.Status != stInterrupted {
		t.Fatalf("after restart: %s", ij.Status)
	}
	if err := m2.Continue(j.ID); err != nil {
		t.Fatal(err)
	}
	dj := waitStatus(t, m2, j.ID, stDone, stFailed)
	if dj.Status != stDone || asInt(dj.Summary["copied"]) != 29 {
		t.Fatalf("resume: %s %q %v", dj.Status, dj.Error, dj.Summary)
	}
	if countFiles(out) != 31 {
		t.Fatalf("output has %d files", countFiles(out))
	}
}

func TestRearrangeConfirmAndJournal(t *testing.T) {
	s := struct{ mgr *Manager }{newTestManager(t)}
	root := filepath.Join(testfix.TempDir(t), "FILES")
	writeFile(t, filepath.Join(root, "x", "a.txt"), "alpha")
	writeFile(t, filepath.Join(root, "y", "a copy.txt"), "alpha")
	writeFile(t, filepath.Join(root, "z", "b.txt"), "bravo")
	b, _ := json.Marshal(RearrangeParams{Root: root})
	j, err := s.mgr.Start("rearrange", b)
	if err != nil {
		t.Fatal(err)
	}
	pj := waitStatus(t, s.mgr, j.ID, stPreviewed)
	if asInt(pj.Summary["deletes"]) != 1 {
		t.Fatalf("preview: %v", pj.Summary)
	}
	if err := s.mgr.Execute(j.ID, 0); err == nil {
		t.Fatal("execute without the typed delete count must be refused")
	}
	if err := s.mgr.Execute(j.ID, 1); err != nil {
		t.Fatal(err)
	}
	dj := waitStatus(t, s.mgr, j.ID, stDone, stFailed)
	if dj.Status != stDone || dj.Journal == "" || asInt(dj.Summary["deleted"]) != 1 {
		t.Fatalf("execute: %s %q %v journal %q", dj.Status, dj.Error, dj.Summary, dj.Journal)
	}
	ji, ok := readJournalInfo(dj.Journal)
	if !ok || ji.State != "complete" {
		t.Fatalf("journal: %+v", ji)
	}
	// undo through a recover job
	rb, _ := json.Marshal(RecoverParams{Journal: dj.Journal, Undo: true})
	uj, err := s.mgr.Start("recover", rb)
	if err != nil {
		t.Fatal(err)
	}
	if u := waitStatus(t, s.mgr, uj.ID, stDone, stFailed); u.Status != stDone {
		t.Fatalf("undo: %s %q", u.Status, u.Error)
	}
	if ji, _ = readJournalInfo(dj.Journal); ji.State != "notApplied" {
		t.Fatalf("after undo: %+v", ji)
	}
}

func TestFolderBrowserAndNamingPreview(t *testing.T) {
	s, c := newTestServer(t)
	_, in := mergeTree(t, 3)
	var fsr struct {
		Path    string    `json:"path"`
		Entries []FSEntry `json:"entries"`
		Roots   []FSEntry `json:"roots"`
	}
	getJSON(t, c, base(s)+"/api/fs?path="+in, &fsr)
	if len(fsr.Entries) != 3 || len(fsr.Roots) == 0 {
		t.Fatalf("fs: %+v", fsr)
	}
	if code := getJSON(t, c, base(s)+"/api/fs?path=relative/dir", nil); code != 400 {
		t.Fatalf("relative path must be refused: %d", code)
	}
	var pv struct {
		Samples []Sample `json:"samples"`
		Error   string   `json:"error"`
	}
	postJSON(t, c, base(s)+"/api/naming/preview", map[string]any{"spec": "ext-year", "dirs": []string{in}}, &pv)
	if pv.Error != "" || len(pv.Samples) == 0 || !strings.HasPrefix(pv.Samples[0].Dst, "TXT") {
		t.Fatalf("preview: %+v", pv)
	}
	postJSON(t, c, base(s)+"/api/naming/preview", map[string]any{"spec": ".jpeg=X/{stem};*={stem}", "dirs": []string{in}}, &pv)
	if !strings.Contains(pv.Error, "never matches") {
		t.Fatalf("bad selector: %+v", pv)
	}
}

func TestToolsAPI(t *testing.T) {
	t.Setenv("ONEFOLD_STATE_DIR", testfix.TempDir(t))
	s, c := newTestServer(t)
	var snap struct {
		Packages []struct {
			ID        string `json:"id"`
			Installed string `json:"installed"`
		} `json:"packages"`
		Dir     string `json:"dir"`
		Running bool   `json:"running"`
	}
	if code := getJSON(t, c, base(s)+"/api/tools", &snap); code != 200 || snap.Dir == "" || snap.Running {
		t.Fatalf("GET /api/tools: %d %+v", code, snap)
	}
	if len(snap.Packages) != len(tooldl.Catalog()) {
		t.Fatalf("packages %d, catalog %d", len(snap.Packages), len(tooldl.Catalog()))
	}
	var e map[string]string
	if code := postJSON(t, c, base(s)+"/api/tools/install", map[string]any{"ids": []string{"nope"}}, &e); code != 409 || e["error"] == "" {
		t.Fatalf("unknown package: %d %v", code, e)
	}
}

// Purge job: from an executed merge job, a preview lists the files,
// the execute needs their typed count, deletes them and empties their dirs.
func TestPurgeJob(t *testing.T) {
	m := newTestManager(t)
	out, in := mergeTree(t, 3)
	writeFile(t, filepath.Join(in, "docs", "x.tmp"), "transient: excluded, stays")
	b, _ := json.Marshal(MergeParams{Inputs: []string{in}, Output: out})
	if _, err := m.Start("purge", []byte(`{"mergeJob":"nope"}`)); err != nil {
		t.Fatal(err) // validated when it runs: the merge job must exist and be executed
	}
	waitIdle(t, m)
	mj, err := m.Start("merge", b)
	if err != nil {
		t.Fatal(err)
	}
	pj := waitStatus(t, m, mj.ID, stPreviewed)
	if asInt(pj.Summary["excluded"]) != 1 {
		t.Fatalf("excluded: %v", pj.Summary)
	}
	if err := m.Execute(mj.ID, 0); err != nil {
		t.Fatal(err)
	}
	if ej := waitStatus(t, m, mj.ID, stDone, stFailed); ej.Status != stDone || asInt(ej.Summary["settled"]) != 5 {
		t.Fatalf("merge execute: %s %v", ej.Status, ej.Summary)
	}
	j, err := m.Start("purge", []byte(`{"mergeJob":"`+mj.ID+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	pv := waitStatus(t, m, j.ID, stPreviewed, stFailed)
	if pv.Status != stPreviewed || asInt(pv.Summary["candidates"]) != 5 || countFiles(in) != 6 {
		t.Fatalf("purge preview: %s %q %v", pv.Status, pv.Error, pv.Summary)
	}
	if err := m.Execute(j.ID, 4); err == nil {
		t.Fatal("wrong typed count accepted")
	}
	if err := m.Execute(j.ID, 5); err != nil {
		t.Fatal(err)
	}
	dj := waitStatus(t, m, j.ID, stDone, stFailed)
	if dj.Status != stDone || asInt(dj.Summary["deleted"]) != 5 || dj.Journal == "" {
		t.Fatalf("purge: %s %q %v", dj.Status, dj.Error, dj.Summary)
	}
	if countFiles(in) != 1 || countFiles(out) != 4 {
		t.Fatalf("after purge: input %d files, output %d", countFiles(in), countFiles(out))
	}
	for _, d := range []string{"a", "b"} {
		if _, err := os.Stat(filepath.Join(in, d)); !os.IsNotExist(err) {
			t.Errorf("emptied dir %s kept", d)
		}
	}
}

func waitIdle(t *testing.T, m *Manager) {
	t.Helper()
	for i := 0; i < 1500; i++ {
		m.mu.Lock()
		busy := m.active != nil
		m.mu.Unlock()
		if !busy {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("a job is still running")
}

// The naming preview composes the families; /api/cli returns the command.
func TestFamiliesPreviewAndCLI(t *testing.T) {
	s, c := newTestServer(t)
	_, in := mergeTree(t, 2)
	var pv struct {
		Samples       []Sample `json:"samples"`
		Error         string   `json:"error"`
		Naming        string   `json:"naming"`
		CatalogPreset string   `json:"catalogPreset"`
	}
	postJSON(t, c, base(s)+"/api/naming/preview", map[string]any{"families": map[string]any{
		"doc": map[string]any{"preset": "ext-year", "stem": "prefix"}, "rest": map[string]any{"preset": "ext-year"}},
		"dirs": []string{in}}, &pv)
	if pv.Error != "" || pv.CatalogPreset != "all" || !strings.Contains(pv.Naming, "origin-1") || len(pv.Samples) == 0 {
		t.Fatalf("families preview: %+v", pv)
	}
	found := false
	for _, sm := range pv.Samples {
		found = found || strings.Contains(sm.Dst, "[docs] note")
	}
	if !found {
		t.Fatalf("no prefixed sample: %+v", pv.Samples)
	}
	var cli struct {
		Command string `json:"command"`
	}
	params := MergeParams{Inputs: []string{in}, Output: "/o u t", Filters: FilterParams{Rules: map[string]bool{"shortcuts": false}}}
	params.Families = map[string]naming.Choice{"doc": {Preset: "documents", Stem: "prefix", Level: 2}}
	postJSON(t, c, base(s)+"/api/cli", map[string]any{"kind": "merge", "params": params}, &cli)
	if !strings.Contains(cli.Command, "-name doc=documents+prefix:2") || !strings.Contains(cli.Command, "-keep shortcuts") ||
		!strings.Contains(cli.Command, "-output '/o u t'") {
		t.Fatalf("cli: %s", cli.Command)
	}
}

// Exclusions after the preview: the plan is re-planned on the
// cataloged records, becomes the confirmed one and the execute runs it;
// "this content everywhere" excludes the file and its duplicates; removing an
// exclusion the preview walked with needs a new preview.
func TestExcludeReplan(t *testing.T) {
	m := newTestManager(t)
	out, in := mergeTree(t, 4)
	writeFile(t, filepath.Join(in, "DRIVERS", "Display", "nvdata.data"), "driver data")
	writeFile(t, filepath.Join(in, "DRIVERS", "Display", "license.txt"), "driver license")
	writeFile(t, filepath.Join(in, "DRIVERS", "Display", "nv.inf"), "[Version]") // system-ext: excluded, marks a driver
	writeFile(t, filepath.Join(in, "DRIVERS", "Display", "nv.sys"), "MZ driver") // system-ext too
	p := MergeParams{Inputs: []string{in}, Output: out}
	p.Filters.ExcludeGlobs = []string{"*.bak"}
	b, _ := json.Marshal(p)
	j, err := m.Start("merge", b)
	if err != nil {
		t.Fatal(err)
	}
	pj := waitStatus(t, m, j.ID, stPreviewed)
	if asInt(pj.Summary["toCopy"]) != 6 || asInt(pj.Summary["packages"]) != 1 {
		t.Fatalf("preview: %v", pj.Summary)
	}
	ex, err := m.Exclude(j.ID, ExcludeReq{Paths: []string{filepath.Join(in, "DRIVERS")}, Globs: []string{"*.bak"},
		ContentOf: filepath.Join(in, "b", "note0000.txt")})
	if err != nil {
		t.Fatal(err)
	}
	// DRIVERS (2 files) and note0000 with its input duplicate leave the plan
	// (the two system files were excluded by the preview walk already)
	if asInt(ex.Summary["toCopy"]) != 3 || asInt(ex.Summary["excluded"]) != 6 || asInt(ex.Summary["packages"]) != 0 || ex.Status != stPreviewed {
		t.Fatalf("replan: %s %v", ex.Status, ex.Summary)
	}
	var saved MergeParams
	json.Unmarshal(ex.Params, &saved)
	if len(saved.Filters.ExcludePaths) != 3 || len(saved.Filters.ExcludeGlobs) != 1 {
		t.Fatalf("params: %+v", saved.Filters)
	}
	items, total, _ := m.query(j.ID, ItemQuery{List: "excluded", Limit: 100})
	if total != 6 || items[0].Note != "exclude-path" {
		t.Fatalf("excluded list: %d %+v", total, items)
	}
	// removing the pattern the preview walked with needs a new preview
	if _, err := m.Exclude(j.ID, ExcludeReq{Paths: saved.Filters.ExcludePaths}); !errors.Is(err, errNeedsPreview) {
		t.Fatalf("removing a walked exclusion: %v", err)
	}
	// the execute runs the re-planned plan
	if err := m.Execute(j.ID, 0); err != nil {
		t.Fatal(err)
	}
	if ej := waitStatus(t, m, j.ID, stDone, stFailed); ej.Status != stDone || asInt(ej.Summary["copied"]) != 3 {
		t.Fatalf("execute: %s %q %v", ej.Status, ej.Error, ej.Summary)
	}
	if countFiles(out) != 4 {
		t.Fatalf("output has %d files", countFiles(out))
	}
	if _, err := m.Exclude(j.ID, ExcludeReq{}); !errors.Is(err, errBadStatus) {
		t.Fatalf("exclude after execute: %v", err)
	}
}

// Result lists: sort, extension, folder; the tree groups sources by
// folder under their room.
func TestListQueryAndTree(t *testing.T) {
	m := newTestManager(t)
	out, in := mergeTree(t, 3)
	writeFile(t, filepath.Join(in, "docs", "sub", "big.pdf"), strings.Repeat("x", 5000))
	b, _ := json.Marshal(MergeParams{Inputs: []string{in}, Output: out})
	j, err := m.Start("merge", b)
	if err != nil {
		t.Fatal(err)
	}
	pj := waitStatus(t, m, j.ID, stPreviewed)
	items, total, _ := m.query(j.ID, ItemQuery{List: "copies", Sort: "-size", Limit: 2})
	if total != 4 || len(items) != 2 || filepath.Base(items[0].Src) != "big.pdf" {
		t.Fatalf("sort: %d %+v", total, items)
	}
	if _, total, _ = m.query(j.ID, ItemQuery{List: "copies", Ext: ".PDF", Limit: 10}); total != 1 {
		t.Fatalf("ext filter: %d", total)
	}
	// note0000 is copied from b/ (its shorter duplicate wins)
	if _, total, _ = m.query(j.ID, ItemQuery{List: "copies", Dir: filepath.Join(in, "docs"), Limit: 10}); total != 3 {
		t.Fatalf("dir filter: %d", total)
	}
	rooms := summaryRooms(pj.Summary)
	top, _, _ := m.tree(j.ID, ItemQuery{List: "copies"}, rooms)
	if len(top) != 1 || top[0].Path != in || top[0].Files != 4 || !top[0].Dir {
		t.Fatalf("tree top: %+v (rooms %v)", top, rooms)
	}
	docs, _, _ := m.tree(j.ID, ItemQuery{List: "copies", Dir: filepath.Join(in, "docs")}, rooms)
	if len(docs) != 3 || !docs[0].Dir || docs[0].Name != "sub" || docs[0].Bytes != 5000 || docs[1].Dir {
		t.Fatalf("tree docs: %+v", docs)
	}
	exts, _ := m.exts(j.ID, "copies")
	if exts["txt"] != 3 || exts["pdf"] != 1 {
		t.Fatalf("exts: %v", exts)
	}
}

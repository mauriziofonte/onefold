// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package gui

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/merge"
	"github.com/mauriziofonte/onefold/internal/power"
	"github.com/mauriziofonte/onefold/internal/report"
	"github.com/mauriziofonte/onefold/internal/runctl"
)

// Job statuses.
const (
	stRunning     = "running"
	stPaused      = "paused"
	stPreviewed   = "previewed"
	stDone        = "done"
	stFailed      = "failed"
	stStopped     = "stopped"
	stInterrupted = "interrupted" // the program ended while the job was running or paused
)

// Job is one run of a mode: a preview, then (merge, rearrange, apply) an
// execute, resumable after a stop or a power loss.
type Job struct {
	ID       string          `json:"id"`
	Kind     string          `json:"kind"` // merge rearrange recover index plan apply purge stats dedupe
	Status   string          `json:"status"`
	Stage    string          `json:"stage"` // preview | execute
	Phases   []string        `json:"phases"`
	Phase    string          `json:"phase"`
	Params   json.RawMessage `json:"params"`
	Summary  map[string]any  `json:"summary,omitempty"`
	Error    string          `json:"error,omitempty"`
	Journal  string          `json:"journal,omitempty"`
	Created  time.Time       `json:"created"`
	Updated  time.Time       `json:"updated"`
	Started  time.Time       `json:"started"`
	Finished time.Time       `json:"finished,omitempty"`
	Errors   int             `json:"errors"`
	Warnings int             `json:"warnings"`
	Progress *Progress       `json:"progress,omitempty"`
}

// Progress of the current phase of the active job.
type Progress struct {
	Label  string  `json:"label"`
	Unit   string  `json:"unit"`
	Done   int64   `json:"done"`
	Total  int64   `json:"total"`
	Final  bool    `json:"final"`
	Items  int64   `json:"items"`
	Errors int64   `json:"errors"`
	Rate   float64 `json:"rate"` // units per second (smoothed)
	ETA    float64 `json:"eta"`  // seconds, -1 = unknown
}

// LogEntry is one line of the log drawer. Key/Args are a translatable message
// of the GUI itself; Msg is raw engine text (paths, OS errors).
type LogEntry struct {
	T     time.Time      `json:"t"`
	Level string         `json:"level"` // info warn error
	Key   string         `json:"key,omitempty"`
	Args  map[string]any `json:"args,omitempty"`
	Msg   string         `json:"msg,omitempty"`
	Path  string         `json:"path,omitempty"`
	Job   string         `json:"job,omitempty"`
}

// Item is one row of a result table.
type Item struct {
	Src  string `json:"src"`
	Dst  string `json:"dst,omitempty"`
	Size int64  `json:"size,omitempty"`
	Note string `json:"note,omitempty"`
}

// result is what a job keeps on disk besides job.json.
type result struct {
	Fingerprint []string          `json:"fingerprint,omitempty"` // confirmed plan (preview)
	Lists       map[string][]Item `json:"lists,omitempty"`
	// preview of a merge/rearrange: what its walk excluded, and the user
	// exclusions it walked with (re-plan)
	WalkExcluded []Item            `json:"walkExcluded,omitempty"`
	PreviewExcl  previewExcl       `json:"previewExcl,omitempty"`
	WalkPackages []catalog.Package `json:"walkPackages,omitempty"`
	// executed merge: what a purge job deletes from
	Settled []merge.Settle `json:"settled,omitempty"`
	Output  string         `json:"output,omitempty"`
	Rooms   []string       `json:"rooms,omitempty"`
}

var (
	errBusy      = errors.New("another job is running")
	errNotFound  = errors.New("job not found")
	errBadStatus = errors.New("the job cannot do that in its current state")
)

// phasesFor lists the phase keys the frontend shows for a kind and stage.
func phasesFor(kind, stage string) []string {
	switch kind + "/" + stage {
	case "merge/preview":
		return []string{"scanOutput", "scanInputs", "verify", "plan"}
	case "merge/execute":
		return []string{"scanOutput", "scanInputs", "verify", "plan", "copy"}
	case "rearrange/preview":
		return []string{"scan", "verify", "plan"}
	case "rearrange/execute":
		return []string{"scan", "verify", "plan", "apply"}
	case "index/execute":
		return []string{"scan"}
	case "plan/execute":
		return []string{"load", "verify", "plan"}
	case "apply/preview":
		return []string{"check"}
	case "apply/execute":
		return []string{"copy"}
	case "purge/preview":
		return []string{"plan"}
	case "purge/execute":
		return []string{"plan", "delete"}
	case "stats/execute":
		return []string{"scan", "dups"}
	case "dedupe/preview":
		return []string{"scan", "dups"}
	case "dedupe/execute":
		return []string{"scan", "dups", "dedupe"}
	}
	return []string{"recover"}
}

// labelPhase maps an engine reporter label to the phase key of a kind.
func labelPhase(kind, label string) string {
	switch label {
	case "catalog output":
		return "scanOutput"
	case "catalog inputs":
		return "scanInputs"
	case "catalog":
		return "scan"
	case "verify":
		return "verify"
	case "merge", "materialize":
		return "copy"
	case "rearrange":
		return "apply"
	case "resume", "undo":
		return "recover"
	case "check":
		return "check"
	case "purge":
		return "delete"
	case "stats scan":
		return "scan"
	case "stats duplicates":
		return "dups"
	case "dedupe":
		return "dedupe"
	}
	return ""
}

// Manager owns the jobs and the single active run.
type Manager struct {
	st  *store
	hub *hub
	// user config (owner, signature, language) read at the start of a plan or
	// apply job; "" = config.Path()
	cfgPath string
	version string
	// tails counts finished jobs still writing their last log line (after
	// they stopped being active); Wait waits for them
	tails sync.WaitGroup

	mu     sync.Mutex
	jobs   map[string]*Job
	active *Job
	gate   *runctl.Gate
	ring   []LogEntry
	dirty  bool
	// rate smoothing of the active progress
	lastDone int64
	lastT    time.Time
	ewma     float64
	lastSave time.Time
}

const ringSize = 2000

func newManager(st *store, h *hub) *Manager {
	m := &Manager{st: st, hub: h, jobs: map[string]*Job{}}
	for _, j := range st.loadJobs() {
		if j.Status == stRunning || j.Status == stPaused {
			j.Status = stInterrupted
			j.Progress = nil
			st.saveJob(j)
		}
		m.jobs[j.ID] = j
	}
	m.prune(100)
	return m
}

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

// snapshot copies a job for marshalling outside the lock.
func (j *Job) snapshot() *Job {
	c := *j
	if j.Progress != nil {
		p := *j.Progress
		c.Progress = &p
	}
	// run() fills Summary in place under the lock; the copy is read (and
	// marshalled) after the lock is released, so it must own its own map.
	if j.Summary != nil {
		c.Summary = make(map[string]any, len(j.Summary))
		for k, v := range j.Summary {
			c.Summary[k] = v
		}
	}
	return &c
}

// List returns every job, newest first.
func (m *Manager) List() []*Job {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Job, 0, len(m.jobs))
	for _, j := range m.jobs {
		out = append(out, j.snapshot())
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Created.After(out[b].Created) })
	return out
}

// Get returns one job.
func (m *Manager) Get(id string) (*Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return nil, false
	}
	return j.snapshot(), true
}

// Active returns the id of the running job ("" when idle).
func (m *Manager) Active() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active == nil {
		return ""
	}
	return m.active.ID
}

// Log returns the recent log entries of the session (all jobs).
func (m *Manager) Log() []LogEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]LogEntry(nil), m.ring...)
}

// prune drops the oldest finished jobs beyond keep.
func (m *Manager) prune(keep int) {
	var all []*Job
	for _, j := range m.jobs {
		all = append(all, j)
	}
	sort.Slice(all, func(a, b int) bool { return all[a].Created.After(all[b].Created) })
	for i, j := range all {
		if i >= keep && j != m.active {
			m.st.removeJob(j.ID)
			delete(m.jobs, j.ID)
		}
	}
}

// Delete removes a job that is not running.
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return errNotFound
	}
	if j == m.active {
		return errBusy
	}
	delete(m.jobs, id)
	return m.st.removeJob(id)
}

// Start creates a job of kind and runs its first stage.
func (m *Manager) Start(kind string, params json.RawMessage) (*Job, error) {
	if err := validateParams(kind, params); err != nil {
		return nil, err
	}
	stage := "execute"
	if kind == "merge" || kind == "rearrange" || kind == "apply" || kind == "purge" || kind == "dedupe" {
		stage = "preview"
	}
	now := time.Now()
	j := &Job{ID: newID(), Kind: kind, Params: params, Created: now, Updated: now}
	m.mu.Lock()
	if m.active != nil {
		m.mu.Unlock()
		return nil, errBusy
	}
	m.jobs[j.ID] = j
	m.mu.Unlock()
	if err := m.begin(j, stage, false); err != nil {
		return nil, err
	}
	// the job's goroutine is already updating j: copy it under the lock
	m.mu.Lock()
	defer m.mu.Unlock()
	return j.snapshot(), nil
}

// Execute runs the execute stage of a previewed job. confirm must equal the
// number of deletions of a rearrange (typed by the user).
func (m *Manager) Execute(id string, confirm int) error {
	m.mu.Lock()
	j, ok := m.jobs[id]
	m.mu.Unlock()
	if !ok {
		return errNotFound
	}
	if j.Status != stPreviewed || j.Stage != "preview" {
		return errBadStatus
	}
	if j.Kind == "rearrange" && asInt(j.Summary["deletes"]) != confirm {
		return fmt.Errorf("confirmation mismatch: type the number of files to delete")
	}
	if j.Kind == "purge" && asInt(j.Summary["candidates"]) != confirm {
		return fmt.Errorf("confirmation mismatch: type the number of files to delete")
	}
	if j.Kind == "dedupe" && asInt(j.Summary["items"]) != confirm {
		return fmt.Errorf("confirmation mismatch: type the number of copies")
	}
	return m.begin(j, "execute", false)
}

// Continue resumes an interrupted, stopped or partially failed job.
func (m *Manager) Continue(id string) error {
	m.mu.Lock()
	j, ok := m.jobs[id]
	m.mu.Unlock()
	if !ok {
		return errNotFound
	}
	if j.Status != stInterrupted && j.Status != stStopped && j.Status != stFailed {
		return errBadStatus
	}
	if j.Stage == "preview" {
		return m.begin(j, "preview", false)
	}
	return m.begin(j, "execute", true)
}

// Pause, Resume and Stop act on the active job.
func (m *Manager) Pause(id string) error  { return m.control(id, (*runctl.Gate).Pause) }
func (m *Manager) Resume(id string) error { return m.control(id, (*runctl.Gate).Resume) }
func (m *Manager) Stop(id string) error   { return m.control(id, (*runctl.Gate).Stop) }

func (m *Manager) control(id string, f func(*runctl.Gate)) error {
	m.mu.Lock()
	g := m.gate
	ok := m.active != nil && m.active.ID == id
	m.mu.Unlock()
	if !ok {
		return errNotFound
	}
	f(g)
	return nil
}

// PauseForExit pauses the active job and persists it, so the next start lists
// it as interrupted and resumable. Called on window close and on signals.
func (m *Manager) PauseForExit() {
	m.mu.Lock()
	j, g := m.active, m.gate
	m.mu.Unlock()
	if j == nil {
		return
	}
	g.Pause()
	m.mu.Lock()
	j.Status = stPaused
	j.Updated = time.Now()
	m.st.saveJob(j)
	m.mu.Unlock()
}

// Wait waits until no finished job is still writing to the state dir. A
// running or paused job is not waited for.
func (m *Manager) Wait() { m.tails.Wait() }

// begin marks j active and runs stage in the background.
func (m *Manager) begin(j *Job, stage string, resume bool) error {
	m.mu.Lock()
	if m.active != nil {
		m.mu.Unlock()
		return errBusy
	}
	// the user's zone, date order and month names hold for the whole job
	// (one job at a time: process-wide settings are safe)
	if err := m.config().Apply(); err != nil {
		m.mu.Unlock()
		return err
	}
	gate := runctl.New(func(s runctl.State) { m.onGate(j, s) })
	m.active, m.gate = j, gate
	j.Stage, j.Status, j.Error = stage, stRunning, ""
	j.Phases = phasesFor(j.Kind, stage)
	j.Phase = j.Phases[0]
	j.Started, j.Updated, j.Finished = time.Now(), time.Now(), time.Time{}
	j.Progress = &Progress{ETA: -1}
	m.lastDone, m.lastT, m.ewma = 0, time.Now(), 0
	m.st.saveJob(j)
	m.mu.Unlock()
	key := "log.jobStarted"
	if resume {
		key = "log.jobResumed"
	}
	m.log(j, LogEntry{Level: "info", Key: key, Args: map[string]any{"kind": j.Kind, "stage": stage}})
	m.broadcastJob(j)
	go m.run(j, stage, resume, gate)
	return nil
}

func (m *Manager) onGate(j *Job, s runctl.State) {
	m.mu.Lock()
	switch s {
	case runctl.Paused:
		j.Status = stPaused
	case runctl.Running:
		j.Status = stRunning
	}
	j.Updated = time.Now()
	m.st.saveJob(j)
	m.mu.Unlock()
	m.log(j, LogEntry{Level: "info", Key: "log.gate." + s.String()})
	m.broadcastJob(j)
}

func (m *Manager) run(j *Job, stage string, resume bool, g *runctl.Gate) {
	var (
		sum     map[string]any
		pending int
		err     error
	)
	// the config's sleep lock and priority hold while the job works
	c := m.config()
	if perr := power.SetLowPriority(c.LowPriority); perr != nil && c.LowPriority {
		m.log(j, LogEntry{Level: "warn", Msg: "low priority: " + perr.Error()})
	}
	if !c.NoKeepAwake {
		if release, perr := power.KeepAwake(power.Reason); perr == nil {
			defer release()
		} else {
			Logf("keep awake: %v", perr)
		}
	}
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("internal error: %v", r)
			}
		}()
		switch j.Kind {
		case "merge":
			sum, pending, err = m.runMerge(j, g, stage == "execute", resume)
		case "rearrange":
			sum, pending, err = m.runRearrange(j, g, stage == "execute", resume)
		case "recover":
			sum, pending, err = m.runRecover(j, g)
		case "index":
			sum, err = m.runIndex(j, g, resume)
		case "plan":
			sum, err = m.runPlan(j, g)
		case "apply":
			sum, pending, err = m.runApply(j, g, stage == "execute")
		case "purge":
			sum, pending, err = m.runPurge(j, g, stage == "execute", resume)
		case "stats":
			sum, err = m.runStats(j, g)
		case "dedupe":
			sum, pending, err = m.runDedupe(j, g, stage == "execute", resume)
		default:
			err = fmt.Errorf("unknown kind %q", j.Kind)
		}
	}()
	m.mu.Lock()
	if sum != nil {
		if j.Summary == nil || stage == "preview" {
			j.Summary = map[string]any{}
		}
		for k, v := range sum {
			j.Summary[k] = v
		}
	}
	switch {
	case errors.Is(err, runctl.ErrStopped) || (g.Stopped() && (err == nil || pending > 0)):
		j.Status = stStopped
	case err != nil:
		j.Status, j.Error = stFailed, err.Error()
	case pending > 0:
		j.Status = stStopped
	case stage == "preview":
		j.Status = stPreviewed
	default:
		j.Status = stDone
	}
	if j.Status == stPreviewed || j.Status == stDone {
		j.Phase = ""
	}
	j.Finished, j.Updated = time.Now(), time.Now()
	if j.Progress != nil && (j.Status == stDone || j.Status == stPreviewed) {
		j.Progress.Done, j.Progress.Final, j.Progress.ETA = j.Progress.Total, true, 0
	}
	m.active, m.gate = nil, nil
	m.tails.Add(1)
	defer m.tails.Done()
	m.st.saveJob(j)
	// read under the lock: once it is released the job may start its next
	// stage (Execute after a preview) and change them
	status, msg := j.Status, j.Error
	m.mu.Unlock()
	switch status {
	case stFailed:
		m.log(j, LogEntry{Level: "error", Key: "log.jobFailed", Msg: msg})
	case stStopped:
		m.log(j, LogEntry{Level: "warn", Key: "log.jobStopped"})
	default:
		m.log(j, LogEntry{Level: "info", Key: "log.jobFinished", Args: map[string]any{"status": status}})
	}
	m.broadcastJob(j)
}

// setPhase moves the active job to phase (no-op for unknown keys).
func (m *Manager) setPhase(j *Job, phase string) {
	m.mu.Lock()
	for _, p := range j.Phases {
		if p == phase && j.Phase != phase {
			j.Phase = phase
			j.Progress = &Progress{Label: phase, ETA: -1}
			m.lastDone, m.lastT, m.ewma = 0, time.Now(), 0
			m.dirty = true
		}
	}
	m.mu.Unlock()
}

// onEvent receives every engine event (report sink).
func (m *Manager) onEvent(e report.Event) {
	m.mu.Lock()
	j := m.active
	if j == nil {
		m.mu.Unlock()
		if e.Kind == "error" || e.Kind == "warn" {
			m.log(nil, LogEntry{Level: levelOf(e.Kind), Msg: e.Msg, Path: e.Path})
		}
		return
	}
	switch e.Kind {
	case "start", "progress", "done":
		if e.Kind == "start" {
			if ph := labelPhase(j.Kind, e.Label); ph != "" {
				for _, p := range j.Phases {
					if p == ph {
						j.Phase = ph
					}
				}
			}
			m.lastDone, m.lastT, m.ewma = 0, time.Now(), 0
		}
		p := j.Progress
		if p == nil || e.Kind == "start" {
			p = &Progress{ETA: -1}
			j.Progress = p
		}
		now := time.Now()
		if dt := now.Sub(m.lastT).Seconds(); dt >= 0.5 {
			inst := float64(e.Done-m.lastDone) / dt
			if m.ewma == 0 {
				m.ewma = inst
			} else {
				m.ewma = 0.3*inst + 0.7*m.ewma
			}
			m.lastDone, m.lastT = e.Done, now
		}
		if m.ewma == 0 && e.Elapsed > time.Second {
			m.ewma = float64(e.Done) / e.Elapsed.Seconds()
		}
		p.Label, p.Unit, p.Done, p.Total, p.Final, p.Items, p.Errors = e.Label, e.Unit, e.Done, e.Total, e.Final, e.Items, e.Errors
		p.Rate, p.ETA = m.ewma, -1
		if e.Final && m.ewma > 0 && e.Total >= e.Done {
			p.ETA = float64(e.Total-e.Done) / m.ewma
		}
		if e.Kind == "done" {
			p.Done, p.ETA = p.Total, 0
			// the phase right before "plan" has no reporter of its own
			for i, ph := range j.Phases {
				if ph == j.Phase && i+1 < len(j.Phases) && j.Phases[i+1] == "plan" {
					j.Phase = "plan"
					j.Progress = &Progress{Label: "plan", ETA: -1}
				}
			}
		}
		m.dirty = true
		save := time.Since(m.lastSave) > 3*time.Second
		if save {
			m.lastSave = time.Now()
			m.st.saveJob(j)
		}
		m.mu.Unlock()
	default:
		m.mu.Unlock()
		if strings.TrimSpace(e.Msg) == "" && e.Path == "" {
			return
		}
		m.log(j, LogEntry{Level: levelOf(e.Kind), Msg: e.Msg, Path: e.Path})
	}
}

func levelOf(kind string) string {
	switch kind {
	case "error":
		return "error"
	case "warn":
		return "warn"
	}
	return "info"
}

// log records an entry in the session ring, the job counters and log file,
// and pushes it to the open pages.
func (m *Manager) log(j *Job, e LogEntry) {
	if e.T.IsZero() {
		e.T = time.Now()
	}
	m.mu.Lock()
	persist := false
	if j != nil {
		e.Job = j.ID
		switch e.Level {
		case "error":
			j.Errors++
		case "warn":
			j.Warnings++
		}
		persist = j.Errors+j.Warnings < maxLogEntries || e.Level == "info"
	}
	m.ring = append(m.ring, e)
	if len(m.ring) > ringSize {
		m.ring = append([]LogEntry(nil), m.ring[len(m.ring)-ringSize:]...)
	}
	m.dirty = true
	m.mu.Unlock()
	if j != nil && persist {
		m.st.appendLog(j.ID, e)
	}
	m.hub.send("log", e)
}

func (m *Manager) broadcastJob(j *Job) {
	m.mu.Lock()
	s := j.snapshot()
	m.mu.Unlock()
	m.hub.send("job", s)
}

// tick pushes the active job at most every interval while it changes.
func (m *Manager) tick(stop <-chan struct{}, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			m.mu.Lock()
			j, dirty := m.active, m.dirty
			m.dirty = false
			var s *Job
			if j != nil && dirty {
				s = j.snapshot()
			}
			m.mu.Unlock()
			if s != nil {
				m.hub.send("job", s)
			}
		}
	}
}

// items returns a page of one result list, filtered by q (case-insensitive).
// checkPlan compares the plan about to run with the confirmed one: equal on a
// first execute, a subset when resuming (part of it is already done).
func checkPlan(confirmed, current []string, subset bool) error {
	have := make(map[string]bool, len(confirmed))
	for _, k := range confirmed {
		have[k] = true
	}
	var extra []string
	for _, k := range current {
		if !have[k] {
			extra = append(extra, k)
		}
	}
	missing := 0
	if !subset {
		cur := make(map[string]bool, len(current))
		for _, k := range current {
			cur[k] = true
		}
		for _, k := range confirmed {
			if !cur[k] {
				missing++
			}
		}
	}
	if len(extra) == 0 && missing == 0 {
		return nil
	}
	ex := ""
	if len(extra) > 0 {
		ex = ", e.g. " + strings.ReplaceAll(extra[0], "\x00", " -> ")
	}
	return fmt.Errorf("the folders changed since the preview (%d new or different, %d no longer planned%s): nothing was written, run a new preview",
		len(extra), missing, ex)
}

// asInt reads a summary counter (int in memory, float64 once reloaded from JSON).
func asInt(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	}
	return 0
}

// display turns a slash relative path into the OS form.
func display(rel string) string { return filepath.FromSlash(rel) }

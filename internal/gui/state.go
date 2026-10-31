// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package gui is the local web GUI of onefold: an HTTP server on 127.0.0.1
// (per-run token, same-origin only) serving the embedded Svelte frontend, a
// JSON API over the engine (merge, rearrange, golden archive, journals) and a
// server-sent event stream with job progress and log entries.
//
// Jobs are persisted in a durable state directory (never a temp dir): a job
// paused, stopped or cut by a power loss is listed as interrupted at the next
// start and can be resumed.
package gui

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/mauriziofonte/onefold/internal/fsx"
	"github.com/mauriziofonte/onefold/internal/statedir"
)

// StateDir is where the GUI keeps jobs, results, logs and catalog caches
// (statedir.Dir: never a temp dir, a run must survive a reboot).
func StateDir() (string, error) { return statedir.Dir() }

// store persists jobs under <state>/jobs/<id>/.
type store struct {
	dir string
	mu  sync.Mutex // serializes log appends
}

const (
	jobFile    = "job.json"
	resultFile = "result.json"
	logFile    = "log.jsonl"
	cacheFile  = "cache.jsonl"
	// maxLogEntries caps the persisted log of one job (a catalog of a damaged
	// disk can produce millions of errors); the counters stay exact.
	maxLogEntries = 20000
)

func (s *store) jobDir(id string) string { return filepath.Join(s.dir, "jobs", id) }
func (s *store) goldenDir() string       { return filepath.Join(s.dir, "golden") }
func (s *store) cachePath(id string) string {
	return filepath.Join(s.jobDir(id), cacheFile)
}

// writeJSON writes v atomically (temp + rename: these are our own files).
func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp := path + fsx.TempSuffix
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func (s *store) saveJob(j *Job) error { return writeJSON(filepath.Join(s.jobDir(j.ID), jobFile), j) }

func (s *store) saveResult(id string, v any) error {
	return writeJSON(filepath.Join(s.jobDir(id), resultFile), v)
}

func (s *store) loadResult(id string, v any) error {
	return readJSON(filepath.Join(s.jobDir(id), resultFile), v)
}

// loadJobs returns every persisted job, newest first.
func (s *store) loadJobs() []*Job {
	dirs, _ := os.ReadDir(filepath.Join(s.dir, "jobs"))
	var out []*Job
	for _, d := range dirs {
		var j Job
		if d.IsDir() && readJSON(filepath.Join(s.jobDir(d.Name()), jobFile), &j) == nil && j.ID == d.Name() {
			out = append(out, &j)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Created.After(out[b].Created) })
	return out
}

// removeJob deletes the state of one job (our own files only).
func (s *store) removeJob(id string) error {
	if id == "" || strings.ContainsAny(id, `/\.`) {
		return errors.New("bad job id")
	}
	d := s.jobDir(id)
	for _, name := range []string{jobFile, resultFile, logFile, cacheFile, jobFile + fsx.TempSuffix,
		resultFile + fsx.TempSuffix, cacheFile + fsx.TempSuffix} {
		os.Remove(filepath.Join(d, name))
	}
	return os.Remove(d)
}

func (s *store) appendLog(id string, e LogEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := filepath.Join(s.jobDir(id), logFile)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	b, _ := json.Marshal(e)
	f.Write(append(b, '\n'))
}

// readLog returns the entries of a job's log matching level ("" = all).
func (s *store) readLog(id, level string, offset, limit int) ([]LogEntry, int) {
	f, err := os.Open(filepath.Join(s.jobDir(id), logFile))
	if err != nil {
		return nil, 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 4<<20)
	var out []LogEntry
	total := 0
	for sc.Scan() {
		var e LogEntry
		if json.Unmarshal(sc.Bytes(), &e) != nil || (level != "" && e.Level != level) {
			continue
		}
		if total >= offset && (limit <= 0 || len(out) < limit) {
			out = append(out, e)
		}
		total++
	}
	return out, total
}

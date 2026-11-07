// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package gui

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/mauriziofonte/onefold/internal/hashcache"
	"github.com/mauriziofonte/onefold/internal/report"
	"github.com/mauriziofonte/onefold/internal/runctl"
	"github.com/mauriziofonte/onefold/internal/stats"
)

// StatsParams mirror `onefold stats`.
type StatsParams struct {
	Roots     []string `json:"roots"`
	Dups      string   `json:"dups"`                 // off fast full ("" = fast)
	DupMin    int64    `json:"dupMin,omitempty"`     // bytes
	AllVolume bool     `json:"allVolumes,omitempty"` // enter the volumes mounted below a root
}

// liveStats holds the latest snapshot of the running stats job.
var liveStats struct {
	sync.Mutex
	id  string
	res *stats.Result
}

func setLiveStats(id string, r *stats.Result) {
	liveStats.Lock()
	liveStats.id, liveStats.res = id, r
	liveStats.Unlock()
}

func (m *Manager) runStats(j *Job, g *runctl.Gate) (map[string]any, error) {
	var p StatsParams
	if err := json.Unmarshal(j.Params, &p); err != nil {
		return nil, err
	}
	cache := hashcache.OpenIn(m.st.dir)
	defer cache.Close()
	o := stats.Options{Roots: p.Roots, OneVolume: !p.AllVolume, Dups: p.Dups, DupMin: p.DupMin, Gate: g, Cache: cache,
		OnSnapshot: func(r *stats.Result) { setLiveStats(j.ID, r) }}
	o.ProgressFor = func(phase string) (stats.Progress, func()) {
		label := "stats scan"
		if phase == "dups" {
			label = "stats duplicates"
		}
		r := report.New(report.Options{Label: label, Unit: report.Items, Growing: phase == "scan"})
		if phase == "dups" {
			m.setPhase(j, "dups")
		}
		return r, r.Stop
	}
	res, err := stats.Run(o)
	setLiveStats("", nil)
	if res == nil {
		return nil, err
	}
	m.st.saveResult(j.ID, res)
	sum := map[string]any{"roots": res.Roots, "files": res.Files, "dirs": res.Dirs, "bytes": res.Bytes, "errors": res.Errors}
	if res.Dups != nil {
		sum["dupGroups"], sum["wasted"], sum["extra"] = res.Dups.Groups, res.Dups.Wasted, res.Dups.Extra
	}
	return sum, err
}

// handleStatsResult serves the result of a stats job: the live snapshot
// while it runs (null before the first one), else the saved result.
func (s *Server) handleStatsResult(w http.ResponseWriter, id string) {
	if j, ok := s.mgr.Get(id); !ok || j.Kind != "stats" {
		writeErr(w, errNotFound)
		return
	}
	liveStats.Lock()
	live := liveStats.res
	if liveStats.id != id {
		live = nil
	}
	liveStats.Unlock()
	if live != nil {
		writeJSONResp(w, 200, live)
		return
	}
	var raw json.RawMessage
	if err := s.mgr.st.loadResult(id, &raw); err != nil {
		if s.mgr.Active() == id {
			writeJSONResp(w, 200, nil)
			return
		}
		writeErr(w, errNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(raw)
}

// handleMounts lists the volumes a stats job can measure.
func (s *Server) handleMounts(w http.ResponseWriter, r *http.Request) {
	writeJSONResp(w, 200, map[string]any{"mounts": stats.Mounts()})
}

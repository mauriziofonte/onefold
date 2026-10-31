// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package gui

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mauriziofonte/onefold/internal/tooldl"
	"github.com/mauriziofonte/onefold/internal/tools"
)

// toolsDL runs the downloads of external tool packages (one batch at a time)
// and publishes their progress as the "tools" event.
type toolsDL struct {
	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	prog    map[string]tooldl.Progress
	sent    time.Time
	seq     int64 // snapshot number: the page drops snapshots older than one it has
}

func (s *Server) toolsSnapshot() map[string]any {
	s.tdl.mu.Lock()
	defer s.tdl.mu.Unlock()
	return s.toolsSnapshotLocked()
}

func (s *Server) toolsSnapshotLocked() map[string]any {
	dir := tools.ManagedDir()
	inst := tooldl.ReadInstalled(dir)
	type pkg struct {
		tooldl.Package
		Installed string           `json:"installed"`
		Progress  *tooldl.Progress `json:"progress,omitempty"`
	}
	pkgs := []pkg{}
	for _, p := range tooldl.Catalog() {
		x := pkg{Package: p, Installed: inst[p.ID].Version}
		if pr, ok := s.tdl.prog[p.ID]; ok {
			pr := pr
			x.Progress = &pr
		}
		pkgs = append(pkgs, x)
	}
	s.tdl.seq++
	return map[string]any{"packages": pkgs, "dir": dir, "running": s.tdl.running, "seq": s.tdl.seq}
}

func (s *Server) handleTools(w http.ResponseWriter, r *http.Request) {
	switch strings.TrimPrefix(r.URL.Path, "/api/tools") {
	case "":
		writeJSONResp(w, 200, s.toolsSnapshot())
	case "/install":
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			IDs []string `json:"ids"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, err)
			return
		}
		if err := s.startToolsInstall(req.IDs); err != nil {
			writeJSONResp(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, 200, s.toolsSnapshot())
	case "/cancel":
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		s.tdl.mu.Lock()
		if s.tdl.cancel != nil {
			s.tdl.cancel()
		}
		s.tdl.mu.Unlock()
		writeJSONResp(w, 200, s.toolsSnapshot())
	default:
		http.NotFound(w, r)
	}
}

// startToolsInstall installs the packages in the background. Refused while a
// job runs: the job may be using a tool that the install replaces.
func (s *Server) startToolsInstall(ids []string) error {
	var pkgs []tooldl.Package
	for _, id := range ids {
		p, ok := tooldl.Find(id)
		if !ok {
			return errors.New("unknown tool package: " + id)
		}
		pkgs = append(pkgs, p)
	}
	if len(pkgs) == 0 {
		return errors.New("no tool package to install on this platform")
	}
	if s.mgr.Active() != "" {
		return errors.New("a run is active: pause or finish it before installing tools")
	}
	dir := tools.ManagedDir()
	if dir == "" {
		return errors.New("cannot resolve the state directory")
	}
	s.tdl.mu.Lock()
	if s.tdl.running {
		s.tdl.mu.Unlock()
		return errors.New("a tool download is already running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.tdl.running, s.tdl.cancel = true, cancel
	s.tdl.prog = map[string]tooldl.Progress{}
	for _, p := range pkgs {
		s.tdl.prog[p.ID] = tooldl.Progress{ID: p.ID, Phase: tooldl.PhaseQueued, Total: p.Size}
	}
	s.tdl.mu.Unlock()
	s.publishTools(true)
	go func() {
		defer cancel()
		in := &tooldl.Installer{Dir: dir, OnProgress: func(p tooldl.Progress) {
			s.tdl.mu.Lock()
			s.tdl.prog[p.ID] = p
			s.tdl.mu.Unlock()
			s.publishTools(p.Phase != tooldl.PhaseDownload)
		}}
		for _, p := range pkgs {
			if ctx.Err() != nil {
				s.tdl.mu.Lock()
				s.tdl.prog[p.ID] = tooldl.Progress{ID: p.ID, Phase: tooldl.PhaseError, Total: p.Size, Err: tooldl.Cancelled}
				s.tdl.mu.Unlock()
				continue
			}
			Logf("tools: installing %s %s", p.ID, p.Version)
			if err := in.Install(ctx, p); err != nil {
				Logf("tools: %s: %v", p.ID, err)
			}
		}
		tools.Reset()
		s.tdl.mu.Lock()
		s.tdl.running, s.tdl.cancel = false, nil
		s.tdl.mu.Unlock()
		s.publishTools(true)
	}()
	return nil
}

// publishTools sends the "tools" event: always on phase changes, at most
// every 250 ms while bytes flow.
func (s *Server) publishTools(force bool) {
	s.tdl.mu.Lock()
	if !force && time.Since(s.tdl.sent) < 250*time.Millisecond {
		s.tdl.mu.Unlock()
		return
	}
	s.tdl.sent = time.Now()
	snap := s.toolsSnapshotLocked()
	s.tdl.mu.Unlock()
	s.hub.send("tools", snap)
}

// toolsBusy reports whether a tool download runs (a job start waits for it).
func (s *Server) toolsBusy() bool {
	s.tdl.mu.Lock()
	defer s.tdl.mu.Unlock()
	return s.tdl.running
}

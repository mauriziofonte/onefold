// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package gui

import (
	"encoding/json"
	"net/http"
	"runtime"

	"github.com/mauriziofonte/onefold/internal/config"
	"github.com/mauriziofonte/onefold/internal/meta"
)

// config reads the user's config; an unreadable file counts as empty (logged).
func (m *Manager) config() config.Config {
	p := m.cfgPath
	if p == "" {
		var err error
		if p, err = config.Path(); err != nil {
			Logf("config: %v", err)
			return config.Config{}
		}
	}
	c, err := config.LoadFrom(p)
	if err != nil {
		Logf("config: %v", err)
		return config.Config{}
	}
	return c
}

// applyConfig makes the user's zone, date order and month names current for
// what runs outside a job (wizard samples, listings); a zone that cannot be
// resolved is logged here and refused when a job starts.
func (m *Manager) applyConfig() {
	if err := m.config().Apply(); err != nil {
		Logf("config: %v", err)
	}
}

// handleConfig serves GET (the config, its defaults and where it lives) and
// POST (replace and save it) on /api/config.
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		var c config.Config
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			writeErr(w, err)
			return
		}
		p, err := s.configPath()
		if err == nil {
			err = config.SaveTo(p, c)
		}
		if err != nil {
			writeErr(w, err)
			return
		}
		Logf("config: saved (diag log %t)", c.DiagLog)
		if s.mgr.Active() == "" { // a running job keeps its settings; the next one reads them again
			s.mgr.applyConfig()
		}
		if s.opts.OnConfig != nil {
			s.opts.OnConfig(c)
		}
	default:
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	p, _ := s.configPath()
	c := s.mgr.config()
	writeJSONResp(w, 200, map[string]any{
		"config": c, "path": p, "none": config.None,
		"defaultSignature": config.DefaultSignature(s.opts.Version), "effectiveSignature": c.EffectiveSignature(s.opts.Version),
		"diagSupported": runtime.GOOS == "windows",
		"zone":          zoneInfo(c),
	})
}

func (s *Server) configPath() (string, error) {
	if s.mgr.cfgPath != "" {
		return s.mgr.cfgPath, nil
	}
	return config.Path()
}

// zoneInfo is the canonical zone a job would use with c, where it comes from
// (config, TZ, host) or why there is none.
func zoneInfo(c config.Config) map[string]string {
	l, src, err := meta.ResolveZone(c.Timezone)
	if err != nil {
		return map[string]string{"error": err.Error()}
	}
	return map[string]string{"name": l.String(), "source": src}
}

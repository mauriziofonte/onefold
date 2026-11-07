// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package gui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mauriziofonte/onefold/internal/dedupe"
	"github.com/mauriziofonte/onefold/internal/hashcache"
	"github.com/mauriziofonte/onefold/internal/purge"
	"github.com/mauriziofonte/onefold/internal/report"
	"github.com/mauriziofonte/onefold/internal/runctl"
	"github.com/mauriziofonte/onefold/internal/stats"
)

// DedupeParams mirror `onefold dedupe`: the duplicate cleanup of the
// Statistics view.
type DedupeParams struct {
	Roots   []string `json:"roots"`
	Keep    string   `json:"keep,omitempty"` // oldest newest shortest first ("" = oldest)
	KeepIn  string   `json:"keepIn,omitempty"`
	Action  string   `json:"action"` // trash link clone script
	MinSize int64    `json:"minSize,omitempty"`
	// Script (action script): the file the execute writes; never an
	// existing one. PS: PowerShell, else POSIX sh.
	Script string `json:"script,omitempty"`
	PS     bool   `json:"ps,omitempty"`
}

func (p DedupeParams) validate() error {
	if len(p.Roots) == 0 {
		return errors.New("roots: add at least one folder")
	}
	if err := absAll("roots", p.Roots...); err != nil {
		return err
	}
	switch p.Keep {
	case "", dedupe.KeepOldest, dedupe.KeepNewest, dedupe.KeepShortest, dedupe.KeepFirst:
	default:
		return fmt.Errorf("keep %q: oldest, newest, shortest or first", p.Keep)
	}
	if p.KeepIn != "" {
		if err := absAll("keepIn", p.KeepIn); err != nil {
			return err
		}
	}
	switch p.Action {
	case dedupe.ActTrash:
		if !purge.TrashSupported {
			return purge.ErrTrashUnsupported
		}
	case dedupe.ActLink, dedupe.ActClone:
	case dedupe.ActScript:
		if p.Script == "" {
			return errors.New("script: the file to write")
		}
		return absAll("script", p.Script)
	default:
		return fmt.Errorf("action %q: trash, link, clone or script", p.Action)
	}
	return nil
}

func dedupeFingerprint(r *dedupe.Result) []string {
	out := make([]string, 0, len(r.Items))
	for _, it := range r.Items {
		out = append(out, it.Extra+"\x00"+it.Keep+"\x00"+it.Content)
	}
	return out
}

func (m *Manager) runDedupe(j *Job, g *runctl.Gate, execute, resume bool) (map[string]any, int, error) {
	var p DedupeParams
	if err := json.Unmarshal(j.Params, &p); err != nil {
		return nil, 0, err
	}
	// an interrupted execute: its half-done replacements first
	if execute && resume && j.Journal != "" {
		fin, rest, problems, err := dedupe.Resume(j.Journal)
		if err != nil {
			return nil, 0, err
		}
		m.log(j, LogEntry{Level: "info", Msg: fmt.Sprintf("dedupe resume: %d replacements finished, %d originals put back", fin, rest), Path: j.Journal})
		for _, pr := range problems {
			m.log(j, LogEntry{Level: "error", Msg: pr})
		}
	}
	cache := hashcache.OpenIn(m.st.dir)
	defer cache.Close()
	script := p.Action == dedupe.ActScript
	o := dedupe.Options{Roots: p.Roots, Keep: p.Keep, KeepIn: p.KeepIn, Action: p.Action, MinSize: p.MinSize,
		Execute: execute && !script, Expect: -1, Gate: g, Cache: cache, JournalDir: filepath.Join(m.st.dir, "dedupe")}
	o.ProgressFor = func(phase string) (stats.Progress, func()) {
		label := map[string]string{"scan": "stats scan", "dups": "stats duplicates"}[phase]
		if label == "" {
			label = "dedupe"
		}
		r := report.New(report.Options{Label: label, Unit: report.Items, Growing: phase == "scan"})
		if phase == "dups" {
			m.setPhase(j, "dups")
		}
		return r, r.Stop
	}
	var prev result
	if execute {
		if err := m.st.loadResult(j.ID, &prev); err != nil {
			return nil, 0, fmt.Errorf("preview result missing: run a new preview (%w)", err)
		}
		o.BeforeExecute = func(r *dedupe.Result) error {
			if err := checkPlan(prev.Fingerprint, dedupeFingerprint(r), resume); err != nil {
				return err
			}
			m.setPhase(j, "dedupe")
			m.log(j, LogEntry{Level: "info", Key: "log.planConfirmed", Args: map[string]any{"n": len(r.Items)}})
			return nil
		}
	}
	r, err := dedupe.Run(o)
	if r == nil {
		return nil, 0, err
	}
	if !execute {
		l := map[string][]Item{}
		for _, it := range r.Items {
			l["dedupe"] = append(l["dedupe"], Item{Src: it.Extra, Dst: it.Keep, Size: it.Size})
		}
		for _, it := range r.Skipped {
			l["skipped"] = append(l["skipped"], Item{Src: it.Extra, Size: it.Size, Note: it.Why})
		}
		m.st.saveResult(j.ID, result{Fingerprint: dedupeFingerprint(r), Lists: l})
		return map[string]any{"roots": r.Roots, "groups": r.Groups, "items": len(r.Items), "bytes": r.Bytes,
			"skipped": len(r.Skipped), "action": r.Action, "keep": r.Keep, "keepIn": r.KeepIn}, 0, err
	}
	if err != nil {
		return nil, 0, err
	}
	sum := map[string]any{}
	if script {
		if err := o.BeforeExecute(r); err != nil {
			return nil, 0, err
		}
		if err := writeScript(p.Script, r, p.PS); err != nil {
			return nil, 0, err
		}
		sum["script"] = p.Script
		m.log(j, LogEntry{Level: "info", Msg: fmt.Sprintf("dedupe script: %d copies verified now", r.Done), Path: p.Script})
	}
	if r.Journal != "" {
		m.mu.Lock()
		j.Journal = r.Journal
		m.st.saveJob(j)
		m.mu.Unlock()
		m.log(j, LogEntry{Level: "info", Key: "log.journal", Path: r.Journal})
	}
	for _, f := range r.Failed {
		m.log(j, LogEntry{Level: "error", Msg: f.Extra + ": " + f.Why})
	}
	if len(r.Failed) > 0 {
		if prev.Lists == nil {
			prev.Lists = map[string][]Item{}
		}
		delete(prev.Lists, "failed")
		for _, f := range r.Failed {
			prev.Lists["failed"] = append(prev.Lists["failed"], Item{Src: f.Extra, Dst: f.Keep, Note: f.Why})
		}
		m.st.saveResult(j.ID, prev)
	}
	sum["done"], sum["doneBytes"], sum["failed"] = r.Done, r.DoneBytes, len(r.Failed)
	pending := 0
	if r.Stopped {
		pending = len(r.Items) - r.Done - len(r.Failed)
	}
	if len(r.Failed) > 0 {
		err = fmt.Errorf("%d copies left as they were (see the log)", len(r.Failed))
	}
	return sum, pending, err
}

// writeScript writes the deletion script to p, never over an existing file.
func writeScript(p string, r *dedupe.Result, ps bool) error {
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if err := dedupe.Script(f, r, ps); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

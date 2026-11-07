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
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/materialize"
	"github.com/mauriziofonte/onefold/internal/merge"
	"github.com/mauriziofonte/onefold/internal/plan"
	"github.com/mauriziofonte/onefold/internal/purge"
	"github.com/mauriziofonte/onefold/internal/rearrange"
	"github.com/mauriziofonte/onefold/internal/report"
	"github.com/mauriziofonte/onefold/internal/runctl"
	"github.com/mauriziofonte/onefold/internal/settings"
	"github.com/mauriziofonte/onefold/internal/stats"
)

// MergeParams and RearrangeParams are the shared settings schema: the
// job JSON is a settings file of `onefold merge|rearrange -settings`.
type (
	MergeParams     = settings.Merge
	RearrangeParams = settings.Rearrange
	FilterParams    = catalog.Filters
)

// PurgeParams delete the sources of an executed merge job.
type PurgeParams struct {
	MergeJob string `json:"mergeJob"`
	Trash    bool   `json:"trash,omitempty"`
}

// RecoverParams resume or undo a rearrange journal.
type RecoverParams struct {
	Journal string `json:"journal"`
	Undo    bool   `json:"undo"`
}

// IndexParams mirror `catalog`. The scope is the golden scope (preset
// media): Preset may only narrow it; Filters carry the skip rules and exclusions
// (their Preset is ignored).
type IndexParams struct {
	Rooms   []string     `json:"rooms"`
	Host    string       `json:"host"`
	Preset  string       `json:"preset"` // "" = media
	Filters FilterParams `json:"filters"`
	Out     string       `json:"out"` // "" = <state>/golden/<host>-<time>.jsonl
}

// PlanParams mirror `plan`.
type PlanParams struct {
	Manifests  []string `json:"manifests"`
	VerifyFull bool     `json:"verifyFull"`
	VideoTier  bool     `json:"videoTier"`
	Layout     string   `json:"layout"`
	FixExt     bool     `json:"fixExt"`
	Artist     string   `json:"artist"` // set from the user's config at start ("" = none)
	Out        string   `json:"out"`    // "" = <state>/golden/plan-<time>.jsonl
}

// ApplyParams mirror `apply --execute`.
type ApplyParams struct {
	Plan      string `json:"plan"`
	Target    string `json:"target"`
	OnlyHost  string `json:"onlyHost"`
	Workers   int    `json:"workers"`
	Signature string `json:"signature"` // set from the user's config at preview ("" = none)
	Language  string `json:"language"`  // set from the user's config at preview ("" = none)
	// AllowUnstamped publishes photos/videos without canonical metadata when
	// exiftool/ffmpeg are missing; without it the execute refuses to start.
	AllowUnstamped bool `json:"allowUnstamped,omitempty"`
}

func absAll(field string, ps ...string) error {
	for _, p := range ps {
		if strings.TrimSpace(p) == "" {
			return fmt.Errorf("%s: a folder is required", field)
		}
		if !filepath.IsAbs(p) {
			return fmt.Errorf("%s: %q is not an absolute path", field, p)
		}
	}
	return nil
}

// validateParams rejects malformed requests before a job is created.
func validateParams(kind string, raw json.RawMessage) error {
	switch kind {
	case "merge":
		var p MergeParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if len(p.Inputs) == 0 {
			return errors.New("inputs: add at least one folder")
		}
		if err := absAll("inputs", p.Inputs...); err != nil {
			return err
		}
		if err := absAll("output", p.Output); err != nil {
			return err
		}
		_, err := p.Options(false)
		return err
	case "rearrange":
		var p RearrangeParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if err := absAll("root", p.Root); err != nil {
			return err
		}
		_, err := p.Options(false)
		return err
	case "purge":
		var p PurgeParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if p.MergeJob == "" {
			return errors.New("mergeJob: the executed merge whose sources are deleted")
		}
		if p.Trash && !purge.TrashSupported {
			return purge.ErrTrashUnsupported
		}
		return nil
	case "recover":
		var p RecoverParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		return absAll("journal", p.Journal)
	case "index":
		var p IndexParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if len(p.Rooms) == 0 {
			return errors.New("rooms: add at least one folder")
		}
		if err := (catalog.Filters{Preset: p.Preset}).CheckGoldenScope(); err != nil {
			return err
		}
		return absAll("rooms", p.Rooms...)
	case "plan":
		var p PlanParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if len(p.Manifests) == 0 {
			return errors.New("manifests: add at least one manifest")
		}
		if !plan.ValidLayout(p.Layout) {
			return fmt.Errorf("layout %q unknown", p.Layout)
		}
		// generic records of CLI manifests take the default golden tree
		if (p.Layout != "" && p.Layout != "dedupe") || p.FixExt {
			return fmt.Errorf("%w: the golden archive files other types with the default layout, without extension fixes", catalog.ErrMixed)
		}
		return absAll("manifests", p.Manifests...)
	case "apply":
		var p ApplyParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		return absAll("plan/target", p.Plan, p.Target)
	case "stats":
		var p StatsParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return err
		}
		if len(p.Roots) == 0 {
			return errors.New("roots: add at least one folder or volume")
		}
		switch p.Dups {
		case "", stats.DupsOff, stats.DupsFast, stats.DupsFull:
		default:
			return fmt.Errorf("dups %q: off, fast or full", p.Dups)
		}
		return absAll("roots", p.Roots...)
	}
	return fmt.Errorf("unknown kind %q", kind)
}

// catalogOptions builds the catalog scope like the CLI flags do (same defaults).
func catalogOptions(f FilterParams, workers int, defPreset string) (catalog.Options, error) {
	o, err := f.Options(defPreset)
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	o.Workers = workers
	return o, err
}

func (m *Manager) setParams(j *Job, v any) {
	b, _ := json.Marshal(v)
	m.mu.Lock()
	j.Params = b
	m.st.saveJob(j)
	m.mu.Unlock()
}

// ---------------------------------------------------------------- merge

func mergeFingerprint(r *merge.Result) []string {
	out := make([]string, 0, len(r.Copies))
	for _, c := range r.Copies {
		out = append(out, c.SrcPath+"\x00"+c.Rel+"\x00"+strconv.FormatInt(c.Size, 10))
	}
	return out
}

func mergeSummary(r *merge.Result) map[string]any {
	return map[string]any{
		"output": r.Output, "outputFiles": r.OutputFiles, "inputFiles": r.InputFiles, "distinct": r.Distinct,
		"alreadyPresent": r.AlreadyPresent, "inputDupes": r.InputDupes, "toCopy": len(r.Copies), "copyBytes": r.CopyBytes,
		"renamed": r.Renamed, "blocked": len(r.Blocked), "outputDupGroups": r.OutputDupGroups, "outputDupFiles": r.OutputDupFiles,
		"verifyGroups": r.Verify.Groups, "verifySplits": r.Verify.Splits, "verifyUnreadable": r.Verify.Unreadable,
		"skippedByFilters": r.SkippedByRules, "emptyInputs": r.EmptyInputs, "onlineOnly": r.OnlineOnly, "excluded": len(r.Excluded),
		"excludedBy": excludedBy(excludedItems(r.Excluded)), "rooms": r.Rooms, "packages": len(r.Packages),
		"cloned": r.Cloned,
	}
}

// previewResult is the result a merge/rearrange preview saves: its plan, its
// lists and what a later re-plan needs.
func previewResult(fp []string, lists map[string][]Item, f catalog.Filters, pkgs []catalog.Package) result {
	return result{Fingerprint: fp, Lists: lists, WalkExcluded: lists["excluded"], WalkPackages: pkgs,
		PreviewExcl: previewExcl{Paths: f.ExcludePaths, Globs: f.ExcludeGlobs}}
}

// excludedItems lists what the filters left out: path, reason (a rule name).
func excludedItems(ex []catalog.Skip) []Item {
	out := make([]Item, 0, len(ex))
	for _, x := range ex {
		note := x.Rule
		if x.Dir {
			note += "/dir"
		}
		out = append(out, Item{Src: x.Path, Note: note})
	}
	return out
}

func mergeLists(r *merge.Result) map[string][]Item {
	l := map[string][]Item{}
	for _, c := range r.Copies {
		it := Item{Src: c.SrcPath, Dst: display(c.Rel), Size: c.Size}
		if c.Renamed {
			it.Note = "renamed"
			l["renamed"] = append(l["renamed"], it)
		}
		l["copies"] = append(l["copies"], it)
		for _, d := range c.Dupes {
			l["inputDupes"] = append(l["inputDupes"], Item{Src: d, Dst: c.SrcPath, Size: c.Size})
		}
	}
	for _, b := range r.Blocked {
		src, dst, _ := strings.Cut(b, " -> ")
		l["blocked"] = append(l["blocked"], Item{Src: src, Dst: display(dst)})
	}
	for _, g := range r.OutputDupList {
		for _, p := range g[1:] {
			l["outputDupes"] = append(l["outputDupes"], Item{Src: p, Dst: g[0]})
		}
	}
	if len(r.Excluded) > 0 {
		l["excluded"] = excludedItems(r.Excluded)
	}
	if len(r.Packages) > 0 {
		l["packages"] = packageItems(r.Packages)
	}
	return l
}

// packageItems lists package folders: folder, kind, bytes, files.
func packageItems(ps []catalog.Package) []Item {
	out := make([]Item, 0, len(ps))
	for _, p := range ps {
		out = append(out, Item{Src: p.Dir, Note: p.Kind, Size: p.Bytes, Dst: strconv.Itoa(p.Files)})
	}
	return out
}

func (m *Manager) runMerge(j *Job, g *runctl.Gate, execute, resume bool) (map[string]any, int, error) {
	var p MergeParams
	if err := json.Unmarshal(j.Params, &p); err != nil {
		return nil, 0, err
	}
	o, err := p.Options(false)
	if err != nil {
		return nil, 0, err
	}
	o.Execute, o.Log, o.Gate, o.StateCache = execute, report.Writer, g, m.st.cachePath(j.ID)
	var prev result
	if execute {
		if err := m.st.loadResult(j.ID, &prev); err != nil {
			return nil, 0, fmt.Errorf("preview result missing: run a new preview (%w)", err)
		}
		o.BeforeExecute = func(r *merge.Result) error {
			m.setPhase(j, "plan")
			if err := checkPlan(prev.Fingerprint, mergeFingerprint(r), resume); err != nil {
				return err
			}
			m.log(j, LogEntry{Level: "info", Key: "log.planConfirmed", Args: map[string]any{"n": len(r.Copies)}})
			return nil
		}
	}
	r, err := merge.Run(o)
	if r == nil {
		return nil, 0, err
	}
	if !execute {
		if err == nil {
			m.st.saveResult(j.ID, previewResult(mergeFingerprint(r), mergeLists(r), p.Filters, r.Packages))
		}
		return mergeSummary(r), 0, err
	}
	sum := map[string]any{"copied": r.Copied, "copiedBytes": r.CopiedBytes, "failed": len(r.Failed), "pending": r.Pending,
		"staleTemps": r.StaleTemps, "missing": r.Missing(), "blockedAfter": len(r.Blocked), "settled": len(r.Settled)}
	if prev.Lists == nil {
		prev.Lists = map[string][]Item{}
	}
	delete(prev.Lists, "failed")
	for _, f := range r.Failed {
		src, rest, _ := strings.Cut(f, " -> ")
		prev.Lists["failed"] = append(prev.Lists["failed"], Item{Src: src, Note: rest})
	}
	// the source purge starts from what this run settled
	prev.Settled, prev.Output, prev.Rooms = r.Settled, r.Output, r.Rooms[1:]
	m.st.saveResult(j.ID, prev)
	if err == nil && len(r.Failed) > 0 {
		err = fmt.Errorf("%d copies failed (see the log)", len(r.Failed))
	}
	return sum, r.Pending, err
}

// ---------------------------------------------------------------- purge

func purgeFingerprint(r *purge.Result) []string {
	out := make([]string, 0, len(r.Candidates))
	for _, c := range r.Candidates {
		out = append(out, c.Src+"\x00"+c.Keeper)
	}
	return out
}

func (m *Manager) runPurge(j *Job, g *runctl.Gate, execute, resume bool) (map[string]any, int, error) {
	var p PurgeParams
	if err := json.Unmarshal(j.Params, &p); err != nil {
		return nil, 0, err
	}
	m.mu.Lock()
	mj := m.jobs[p.MergeJob]
	m.mu.Unlock()
	if mj == nil || mj.Kind != "merge" {
		return nil, 0, fmt.Errorf("merge job %s not found", p.MergeJob)
	}
	var mr result
	if err := m.st.loadResult(p.MergeJob, &mr); err != nil || mr.Output == "" {
		return nil, 0, fmt.Errorf("merge job %s has not been executed: nothing to purge", p.MergeJob)
	}
	o := purge.Options{Items: mr.Settled, Output: mr.Output, Rooms: mr.Rooms, Execute: execute, Trash: p.Trash, Expect: -1,
		Gate: g, Log: report.Writer}
	var prev result
	if execute {
		if err := m.st.loadResult(j.ID, &prev); err != nil {
			return nil, 0, fmt.Errorf("preview result missing: run a new preview (%w)", err)
		}
		o.BeforeExecute = func(r *purge.Result) error {
			if err := checkPlan(prev.Fingerprint, purgeFingerprint(r), resume); err != nil {
				return err
			}
			m.setPhase(j, "delete")
			m.log(j, LogEntry{Level: "info", Key: "log.planConfirmed", Args: map[string]any{"n": len(r.Candidates)}})
			return nil
		}
	}
	r, err := purge.Run(o)
	if r == nil {
		return nil, 0, err
	}
	if !execute {
		l := map[string][]Item{}
		for _, c := range r.Candidates {
			l["purge"] = append(l["purge"], Item{Src: c.Src, Dst: c.Keeper, Size: c.Size})
		}
		for _, x := range r.Refused {
			l["refused"] = append(l["refused"], Item{Src: x.Src, Note: x.Reason})
		}
		m.st.saveResult(j.ID, result{Fingerprint: purgeFingerprint(r), Lists: l})
		return map[string]any{"output": r.Output, "rooms": r.Rooms, "candidates": len(r.Candidates), "bytes": r.Bytes,
			"refused": len(r.Refused), "trash": p.Trash, "mergeJob": p.MergeJob}, 0, nil
	}
	if r.Journal != "" {
		m.mu.Lock()
		j.Journal = r.Journal
		m.st.saveJob(j)
		m.mu.Unlock()
		m.log(j, LogEntry{Level: "info", Key: "log.journal", Path: r.Journal})
	}
	for _, f := range r.Failed {
		m.log(j, LogEntry{Level: "error", Msg: f})
	}
	if len(r.Failed) > 0 {
		delete(prev.Lists, "failed")
		for _, f := range r.Failed {
			src, why, _ := strings.Cut(f, ": ")
			prev.Lists["failed"] = append(prev.Lists["failed"], Item{Src: src, Note: why})
		}
		m.st.saveResult(j.ID, prev)
	}
	sum := map[string]any{"deleted": r.Deleted, "deletedBytes": r.DeletedBytes, "gone": r.Gone, "failed": len(r.Failed),
		"dirsRemoved": r.DirsRemoved, "junkRemoved": r.JunkRemoved, "pending": r.Pending}
	if err == nil && len(r.Failed) > 0 {
		err = fmt.Errorf("%d files not deleted (see the log)", len(r.Failed))
	}
	return sum, r.Pending, err
}

// ---------------------------------------------------------------- rearrange

func rearrangeFingerprint(r *rearrange.Result) []string {
	var out []string
	for _, mv := range r.Moves {
		out = append(out, "m\x00"+mv.Src+"\x00"+mv.Dst+"\x00"+mv.Hash)
	}
	for _, d := range r.Deletes {
		out = append(out, "d\x00"+d.Path+"\x00"+d.Keeper)
	}
	return out
}

func rearrangeItems(r *rearrange.Result) map[string][]Item {
	l := map[string][]Item{}
	for _, mv := range r.Moves {
		l["moves"] = append(l["moves"], Item{Src: display(mv.Src), Dst: display(mv.Dst), Size: mv.Size})
	}
	for _, d := range r.Deletes {
		l["deletes"] = append(l["deletes"], Item{Src: display(d.Path), Dst: display(d.Keeper), Size: d.Size})
	}
	if len(r.Excluded) > 0 {
		l["excluded"] = excludedItems(r.Excluded)
	}
	if len(r.Packages) > 0 {
		l["packages"] = packageItems(r.Packages)
	}
	return l
}

func rearrangeSummary(r *rearrange.Result) map[string]any {
	return map[string]any{"root": r.Root, "naming": r.Naming, "files": r.Files, "stay": r.Stay, "moves": len(r.Moves),
		"deletes": len(r.Deletes), "deleteBytes": r.DeleteBytes, "dupGroups": r.DupGroups, "skipped": r.Skipped, "excluded": len(r.Excluded),
		"excludedBy": excludedBy(excludedItems(r.Excluded)), "verifySplits": r.Verify.Splits, "rooms": []string{r.Root},
		"packages": len(r.Packages)}
}

func (m *Manager) runRearrange(j *Job, g *runctl.Gate, execute, resume bool) (map[string]any, int, error) {
	var p RearrangeParams
	if err := json.Unmarshal(j.Params, &p); err != nil {
		return nil, 0, err
	}
	if execute && resume && j.Journal != "" {
		rr, err := rearrange.RecoverWith(j.Journal, false, rearrange.RecoverOptions{Gate: g, Log: report.Writer})
		m.setPhase(j, "apply")
		sum := map[string]any{"moved": rr.Moved, "deleted": rr.Deleted, "failed": rr.Failed, "dirsRemoved": rr.DirsRemoved}
		for _, e := range rr.Errors {
			m.log(j, LogEntry{Level: "error", Msg: e})
		}
		pending := 0
		if rr.Stopped {
			pending = 1
		}
		if err == nil && rr.Failed > 0 {
			err = fmt.Errorf("%d steps failed (see the log)", rr.Failed)
		}
		return sum, pending, err
	}
	o, err := p.Options(false)
	if err != nil {
		return nil, 0, err
	}
	o.Execute, o.Log, o.Gate, o.StateCache = execute, report.Writer, g, m.st.cachePath(j.ID)
	if execute {
		var prev result
		if err := m.st.loadResult(j.ID, &prev); err != nil {
			return nil, 0, fmt.Errorf("preview result missing: run a new preview (%w)", err)
		}
		o.BeforeExecute = func(r *rearrange.Result) error {
			m.setPhase(j, "plan")
			if err := checkPlan(prev.Fingerprint, rearrangeFingerprint(r), false); err != nil {
				return err
			}
			m.log(j, LogEntry{Level: "info", Key: "log.planConfirmed", Args: map[string]any{"n": len(r.Moves) + len(r.Deletes)}})
			return nil
		}
		o.OnJournal = func(path string) {
			m.mu.Lock()
			j.Journal = path
			m.st.saveJob(j)
			m.mu.Unlock()
			m.log(j, LogEntry{Level: "info", Key: "log.journal", Path: path})
		}
	}
	r, err := rearrange.Run(o)
	if r == nil {
		return nil, 0, err
	}
	if !execute {
		if err == nil {
			m.st.saveResult(j.ID, previewResult(rearrangeFingerprint(r), rearrangeItems(r), p.Filters, r.Packages))
		}
		return rearrangeSummary(r), 0, err
	}
	for _, f := range r.Failed {
		m.log(j, LogEntry{Level: "error", Msg: f})
	}
	sum := map[string]any{"moved": r.Moved, "deleted": r.Deleted, "dirsRemoved": r.DirsRemoved, "failed": len(r.Failed)}
	pending := 0
	if r.Stopped {
		pending = 1
	}
	if err == nil && len(r.Failed) > 0 {
		err = fmt.Errorf("%d steps failed (see the log)", len(r.Failed))
	}
	return sum, pending, err
}

func (m *Manager) runRecover(j *Job, g *runctl.Gate) (map[string]any, int, error) {
	var p RecoverParams
	if err := json.Unmarshal(j.Params, &p); err != nil {
		return nil, 0, err
	}
	rr, err := rearrange.RecoverWith(p.Journal, p.Undo, rearrange.RecoverOptions{Gate: g, Log: report.Writer})
	for _, e := range rr.Errors {
		m.log(j, LogEntry{Level: "error", Msg: e})
	}
	sum := map[string]any{"journal": p.Journal, "undo": p.Undo, "root": rr.Root, "moved": rr.Moved, "deleted": rr.Deleted,
		"failed": rr.Failed, "dirsRemoved": rr.DirsRemoved}
	pending := 0
	if rr.Stopped {
		pending = 1
	}
	if err == nil && rr.Failed > 0 {
		err = fmt.Errorf("%d steps failed (see the log)", rr.Failed)
	}
	return sum, pending, err
}

// ---------------------------------------------------------------- golden archive

func (m *Manager) runIndex(j *Job, g *runctl.Gate, resume bool) (map[string]any, error) {
	var p IndexParams
	if err := json.Unmarshal(j.Params, &p); err != nil {
		return nil, err
	}
	if p.Host == "" {
		// every record carries a host: this machine's name
		p.Host = catalog.DefaultHost()
		m.setParams(j, p)
	}
	if p.Out == "" {
		host := p.Host
		p.Out = filepath.Join(m.st.goldenDir(), fmt.Sprintf("%s-%s.jsonl", safeName(host), time.Now().Format("20060102-150405")))
		m.setParams(j, p)
	}
	if err := os.MkdirAll(filepath.Dir(p.Out), 0o755); err != nil {
		return nil, err
	}
	f := p.Filters
	f.Preset = p.Preset
	co, err := catalogOptions(f, 0, "media")
	if err != nil {
		return nil, err
	}
	co.Out, co.Host, co.Gate = p.Out, p.Host, g
	if resume {
		// a partial manifest of the interrupted run: its records are reused
		if prev, err := catalog.LoadManifestByPath(p.Out); err == nil {
			co.Reuse = prev
		}
	}
	rep := report.New(report.Options{Label: "catalog", Unit: report.Items, Growing: true})
	co.Progress = rep
	sum, err := catalog.Build(p.Rooms, co)
	rep.Stop()
	if sum == nil {
		return nil, err
	}
	return map[string]any{"manifest": p.Out, "assets": sum.Assets, "photos": sum.Photos, "videos": sum.Videos,
		"errors": sum.Errors, "skipped": sum.Skipped, "bytes": sum.LogicalBytes, "reused": sum.Reused, "byKind": sum.ByKind}, err
}

func safeName(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == ' ' {
			return '_'
		}
		return r
	}, s)
}

func (m *Manager) runPlan(j *Job, g *runctl.Gate) (map[string]any, error) {
	var p PlanParams
	if err := json.Unmarshal(j.Params, &p); err != nil {
		return nil, err
	}
	if p.Artist == "" {
		// the owner comes from the user's config, recorded in the job params
		if a := m.config().Artist; a != "" {
			p.Artist = a
			m.setParams(j, p)
		}
	}
	recs, err := plan.LoadManifests(p.Manifests)
	if err != nil {
		return nil, err
	}
	opts := plan.Options{Layout: p.Layout, FixExt: p.FixExt, VideoTier: p.VideoTier, Artist: p.Artist, SelfContained: true}
	var vs plan.VerifyStats
	if p.VerifyFull {
		m.setPhase(j, "verify")
		rep := report.New(report.Options{Label: "verify", Unit: report.Bytes, Growing: true})
		recs, vs = plan.VerifyGroupsWith(recs, runtime.NumCPU(), rep, g)
		rep.Stop()
		if g.Stopped() {
			return nil, runctl.ErrStopped
		}
	}
	m.setPhase(j, "plan")
	r := plan.PlanWith(recs, opts)
	r.VerifiedGroups, r.VerifySplits, r.VerifyUnreadable = vs.Groups, vs.Splits, vs.Unreadable
	acts := plan.BuildActionsWith(r, opts)
	if p.Out == "" {
		p.Out = filepath.Join(m.st.goldenDir(), fmt.Sprintf("plan-%s.jsonl", time.Now().Format("20060102-150405")))
		m.setParams(j, p)
	}
	if err := writePlan(p.Out, acts); err != nil {
		return nil, err
	}
	return map[string]any{"plan": p.Out, "inputFiles": r.InputFiles, "golden": r.Assets, "exactDupes": r.ExactDupes,
		"lowerRes": r.LowerRes, "smaller": r.SmallerVar, "goldenBytes": r.GoldenBytes, "reclaimBytes": r.ReclaimBytes,
		"verifyGroups": r.VerifiedGroups, "verifySplits": r.VerifySplits, "hosts": planHosts(acts)}, nil
}

func writePlan(path string, acts []plan.Action) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := plan.Write(f, acts); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// HostStat is the share of a plan stored on one host.
type HostStat struct {
	Host  string `json:"host"`
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
}

func planHosts(acts []plan.Action) []HostStat {
	m := map[string]*HostStat{}
	for _, a := range acts {
		h := m[a.SrcHost]
		if h == nil {
			h = &HostStat{Host: a.SrcHost}
			m[a.SrcHost] = h
		}
		h.Files++
		h.Bytes += a.SrcSize
	}
	var out []HostStat
	for _, h := range m {
		out = append(out, *h)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Host < out[b].Host })
	return out
}

func (m *Manager) runApply(j *Job, g *runctl.Gate, execute bool) (map[string]any, int, error) {
	var p ApplyParams
	if err := json.Unmarshal(j.Params, &p); err != nil {
		return nil, 0, err
	}
	if !execute && p.Signature == "" && p.Language == "" {
		// tags from the user's config, fixed at preview time: execute and
		// resume stamp exactly what the confirmed preview recorded
		c := m.config()
		p.Signature, p.Language = c.EffectiveSignature(m.version), c.Language
		m.setParams(j, p)
	}
	all, err := plan.Read(p.Plan)
	if err != nil {
		return nil, 0, err
	}
	var sel []plan.Action
	var total int64
	for _, a := range all {
		if p.OnlyHost == "" || a.SrcHost == p.OnlyHost {
			sel = append(sel, a)
			total += a.SrcSize
		}
	}
	if !execute {
		rep := report.New(report.Options{Label: "check", Unit: report.Items, Total: int64(len(sel))})
		existing := 0
		l := map[string][]Item{}
		for _, a := range sel {
			if !g.Wait() {
				rep.Stop()
				return nil, 0, runctl.ErrStopped
			}
			if _, err := os.Stat(filepath.Join(p.Target, filepath.FromSlash(a.CanonRel))); err == nil {
				existing++
				rep.Advance(0)
				continue
			}
			l["toWrite"] = append(l["toWrite"], Item{Src: a.SrcHost + ":" + a.SrcPath, Dst: display(a.CanonRel), Size: a.SrcSize})
			rep.Item(0, 0)
		}
		rep.Stop()
		m.st.saveResult(j.ID, result{Lists: l})
		return map[string]any{"plan": p.Plan, "target": p.Target, "onlyHost": p.OnlyHost, "actions": len(sel), "bytes": total,
			"existing": existing, "toWrite": len(sel) - existing, "missingTools": materialize.MissingTools(sel, p.Target),
			"allowUnstamped": p.AllowUnstamped}, 0, nil
	}
	// an unsilvered copy stays unsilvered (a later run skips it): without the
	// tools, start only when the preview allowed it
	if missing := materialize.MissingTools(sel, p.Target); len(missing) > 0 && !p.AllowUnstamped {
		return nil, 0, fmt.Errorf("%s not found: photos and videos would be written without canonical metadata for good; "+
			"install the tools (Doctor) or allow copies without canonical metadata and run a new check", strings.Join(missing, ", "))
	}
	rep := report.New(report.Options{Label: "materialize", TotalBytes: total})
	st := materialize.ApplyAll(sel, p.Target, materialize.RunOptions{Workers: p.Workers, Gate: g, Progress: rep,
		Silver: materialize.Silver{Signature: p.Signature, Language: p.Language}})
	rep.Stop()
	sum := map[string]any{"written": st.Written, "skippedExisting": st.Skipped, "failed": st.Failed, "pending": st.Pending,
		"unsilvered": st.Unsilvered}
	if st.Failed > 0 {
		return sum, st.Pending, fmt.Errorf("%d actions failed (see the log)", st.Failed)
	}
	return sum, st.Pending, nil
}

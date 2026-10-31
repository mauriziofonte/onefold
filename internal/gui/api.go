// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package gui

import (
	"bufio"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/kinds"
	"github.com/mauriziofonte/onefold/internal/merge"
	"github.com/mauriziofonte/onefold/internal/meta"
	"github.com/mauriziofonte/onefold/internal/naming"
	"github.com/mauriziofonte/onefold/internal/plan"
	"github.com/mauriziofonte/onefold/internal/settings"
)

// ---------------------------------------------------------------- folder browser

// FSEntry is one sub-folder in the folder picker.
type FSEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

func (s *Server) handleFS(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimSpace(r.URL.Query().Get("path"))
	resp := map[string]any{"roots": roots(), "sep": string(filepath.Separator)}
	if p == "" {
		resp["path"], resp["entries"] = "", []FSEntry{}
		writeJSONResp(w, 200, resp)
		return
	}
	p = filepath.Clean(p)
	if !filepath.IsAbs(p) {
		writeErr(w, &os.PathError{Op: "list", Path: p, Err: os.ErrInvalid})
		return
	}
	ents, err := os.ReadDir(p)
	if err != nil {
		writeErr(w, err)
		return
	}
	hidden := r.URL.Query().Get("hidden") == "1"
	out := []FSEntry{}
	for _, e := range ents {
		isDir := e.IsDir()
		if !isDir && e.Type()&fs.ModeSymlink != 0 {
			if fi, err := os.Stat(filepath.Join(p, e.Name())); err == nil && fi.IsDir() {
				isDir = true
			}
		}
		if !isDir || (!hidden && (strings.HasPrefix(e.Name(), ".") || e.Name() == "$RECYCLE.BIN" || e.Name() == "System Volume Information")) {
			continue
		}
		out = append(out, FSEntry{Name: e.Name(), Path: filepath.Join(p, e.Name())})
	}
	sort.Slice(out, func(a, b int) bool { return strings.ToLower(out[a].Name) < strings.ToLower(out[b].Name) })
	parent := filepath.Dir(p)
	if parent == p {
		parent = ""
	}
	resp["path"], resp["parent"], resp["entries"] = p, parent, out
	writeJSONResp(w, 200, resp)
}

// ---------------------------------------------------------------- naming

// PresetInfo describes one naming preset.
type PresetInfo struct {
	Name string `json:"name"`
	Doc  string `json:"doc"`
	Spec string `json:"spec"`
}

func (s *Server) handleNaming(w http.ResponseWriter, r *http.Request) {
	var ps []PresetInfo
	for _, n := range naming.PresetNames() {
		ps = append(ps, PresetInfo{Name: n, Doc: naming.Presets[n].Doc, Spec: naming.Presets[n].Spec})
	}
	var cps []PresetInfo
	for _, n := range kinds.PresetNames() {
		cps = append(cps, PresetInfo{Name: n, Doc: kinds.PresetDoc(n)})
	}
	writeJSONResp(w, 200, map[string]any{"presets": ps, "aliases": naming.LayoutAliases, "placeholders": naming.Placeholders,
		"catalogPresets": cps, "cleanSuffix": naming.CleanSuffix, "families": naming.Families, "genericPresets": naming.GenericPresets,
		"stemModes": naming.StemModes, "maxOriginLevel": naming.MaxOriginLevel, "rules": catalog.Rules,
		"trash": runtime.GOOS == "windows"})
}

// Sample is a source file and its rendered target.
type Sample struct {
	Src string `json:"src"`
	Dst string `json:"dst"`
}

// handleNamingPreview renders the naming of a form (families, free template,
// or a spec) on sample files of its folders, and returns the composed spec.
func (s *Server) handleNamingPreview(w http.ResponseWriter, r *http.Request) {
	var req struct {
		settings.Naming
		Legacy string   `json:"spec"` // older forms: preset, template or map (+ clean)
		Dirs   []string `json:"dirs"`
		Max    int      `json:"max"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, err)
		return
	}
	if req.Spec == "" {
		req.Spec = req.Legacy
	}
	fail := func(err error) {
		writeJSONResp(w, 200, map[string]any{"error": err.Error(), "samples": []Sample{}})
	}
	spec, err := req.Naming.Resolve()
	if err != nil {
		fail(err)
		return
	}
	if err := naming.CheckTemplate(spec); err != nil {
		fail(err)
		return
	}
	nm, err := naming.Resolve(spec)
	if err != nil {
		fail(err)
		return
	}
	if req.Max <= 0 || req.Max > 12 {
		req.Max = 6
	}
	var samples []Sample
	for _, p := range sampleFiles(req.Dirs, req.Max) {
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		m, _ := meta.Extract(p) // reads headers only; best-effort
		rec := catalog.Record{Path: p, Size: fi.Size(), MTimeNs: fi.ModTime().UnixNano(), Meta: m}
		samples = append(samples, Sample{Src: p, Dst: display(nm.Target(naming.VarsOf(rec)))})
	}
	if samples == nil {
		samples = []Sample{}
	}
	writeJSONResp(w, 200, map[string]any{"spec": nm.Spec(), "naming": spec, "catalogPreset": naming.CatalogPreset(req.Families),
		"samples": samples})
}

// handleCLI returns the command line equivalent to the settings of a form
// (POST {kind: merge|rearrange, params}): PowerShell quoting, display only.
func (s *Server) handleCLI(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Kind   string          `json:"kind"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, err)
		return
	}
	var args []string
	switch req.Kind {
	case "merge":
		var p MergeParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeErr(w, err)
			return
		}
		args = p.Args()
	case "rearrange":
		var p RearrangeParams
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeErr(w, err)
			return
		}
		args = p.Args()
	default:
		writeJSONResp(w, 400, map[string]any{"error": "kind: merge or rearrange"})
		return
	}
	writeJSONResp(w, 200, map[string]any{"args": args, "command": psCommand(args)})
}

var psSafe = regexp.MustCompile(`^[A-Za-z0-9_./:=+,\\-]+$`)

// psCommand quotes args for PowerShell: single quotes, a quote doubled.
func psCommand(args []string) string {
	out := []string{"onefold"}
	for _, a := range args {
		if !psSafe.MatchString(a) {
			a = "'" + strings.ReplaceAll(a, "'", "''") + "'"
		}
		out = append(out, a)
	}
	return strings.Join(out, " ")
}

// sampleFiles picks up to max files from dirs, preferring distinct
// extensions, without descending into dot or junk folders and with a bounded
// walk (a preview must answer fast on a huge tree).
func sampleFiles(dirs []string, max int) []string {
	var out []string
	seenExt := map[string]bool{}
	var extra []string
	for _, d := range dirs {
		if !filepath.IsAbs(d) {
			continue
		}
		visited := 0
		filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			visited++
			if visited > 3000 || len(out) >= max {
				return filepath.SkipAll
			}
			name := e.Name()
			if e.IsDir() {
				if p != d && (strings.HasPrefix(name, ".") || kinds.IsJunk(name)) {
					return filepath.SkipDir
				}
				return nil
			}
			if !e.Type().IsRegular() || strings.HasPrefix(name, ".") || kinds.IsJunk(name) {
				return nil
			}
			ext := kinds.Ext(name)
			if !seenExt[ext] {
				seenExt[ext] = true
				out = append(out, p)
			} else if len(extra) < max {
				extra = append(extra, p)
			}
			return nil
		})
	}
	for _, p := range extra {
		if len(out) >= max {
			break
		}
		out = append(out, p)
	}
	return out
}

// ---------------------------------------------------------------- journals

// JournalInfo summarizes one rearrange journal from what is on disk now.
type JournalInfo struct {
	Path     string    `json:"path"`
	Root     string    `json:"root"`
	Naming   string    `json:"naming"`
	Time     time.Time `json:"time"`
	Moves    int       `json:"moves"`
	Deletes  int       `json:"deletes"`
	InFlight int       `json:"inFlight"` // files parked under their temp name
	Done     int       `json:"done"`     // moves found at their destination
	State    string    `json:"state"`    // complete | interrupted | notApplied
}

func readJournalInfo(path string) (JournalInfo, bool) {
	ji := JournalInfo{Path: path}
	f, err := os.Open(path)
	if err != nil {
		return ji, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	type ent struct {
		Op     string `json:"op"`
		Root   string `json:"root"`
		Naming string `json:"naming"`
		Time   string `json:"time"`
		Src    string `json:"src"`
		Tmp    string `json:"tmp"`
		Dst    string `json:"dst"`
	}
	for sc.Scan() {
		var e ent
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		switch e.Op {
		case "header":
			ji.Root, ji.Naming = e.Root, e.Naming
			ji.Time, _ = time.Parse(time.RFC3339, e.Time)
		case "move":
			ji.Moves++
			if ji.Root != "" {
				if _, err := os.Lstat(filepath.Join(ji.Root, filepath.FromSlash(e.Tmp))); err == nil {
					ji.InFlight++
				} else if _, err := os.Lstat(filepath.Join(ji.Root, filepath.FromSlash(e.Dst))); err == nil {
					ji.Done++
				}
			}
		case "delete":
			ji.Deletes++
		}
	}
	if ji.Root == "" {
		return ji, false
	}
	switch {
	case ji.InFlight == 0 && ji.Done == ji.Moves:
		ji.State = "complete"
	case ji.InFlight == 0 && ji.Done == 0:
		ji.State = "notApplied"
	default:
		ji.State = "interrupted"
	}
	return ji, true
}

func (s *Server) handleJournals(w http.ResponseWriter, r *http.Request) {
	roots := map[string]bool{}
	if q := strings.TrimSpace(r.URL.Query().Get("root")); q != "" && filepath.IsAbs(q) {
		roots[filepath.Clean(q)] = true
	}
	for _, j := range s.mgr.List() {
		if j.Kind == "rearrange" {
			var p RearrangeParams
			if json.Unmarshal(j.Params, &p) == nil && p.Root != "" {
				roots[filepath.Clean(p.Root)] = true
			}
		}
		if j.Journal != "" {
			roots[filepath.Dir(filepath.Dir(j.Journal))] = true
		}
	}
	out := []JournalInfo{}
	for root := range roots {
		paths, _ := filepath.Glob(filepath.Join(root, merge.CacheDir, "rearrange-*.jsonl"))
		for _, p := range paths {
			if ji, ok := readJournalInfo(p); ok {
				out = append(out, ji)
			}
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Time.After(out[b].Time) })
	writeJSONResp(w, 200, map[string]any{"journals": out})
}

// ---------------------------------------------------------------- golden archive files

// GoldenFile is a manifest or plan in the state directory.
type GoldenFile struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	Plan     bool      `json:"plan"`
	Hosts    any       `json:"hosts,omitempty"`
}

func (s *Server) handleGolden(w http.ResponseWriter, r *http.Request) {
	if p := r.URL.Query().Get("plan"); p != "" {
		acts, err := plan.Read(p)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSONResp(w, 200, map[string]any{"hosts": planHosts(acts), "actions": len(acts)})
		return
	}
	dir := s.mgr.st.goldenDir()
	ents, _ := os.ReadDir(dir)
	out := []GoldenFile{}
	for _, e := range ents {
		fi, err := e.Info()
		if err != nil || e.IsDir() || strings.HasSuffix(e.Name(), "tmp") {
			continue
		}
		out = append(out, GoldenFile{Name: e.Name(), Path: filepath.Join(dir, e.Name()), Size: fi.Size(),
			Modified: fi.ModTime(), Plan: strings.HasPrefix(e.Name(), "plan-")})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Modified.After(out[b].Modified) })
	writeJSONResp(w, 200, map[string]any{"dir": dir, "files": out})
}

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
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/fsx"
	"github.com/mauriziofonte/onefold/internal/kinds"
	"github.com/mauriziofonte/onefold/internal/meta"
	"github.com/mauriziofonte/onefold/internal/names"
	"github.com/mauriziofonte/onefold/internal/naming"
	"github.com/mauriziofonte/onefold/internal/plan"
	"github.com/mauriziofonte/onefold/internal/settings"
	"github.com/mauriziofonte/onefold/internal/sparsehash"
)

// Guided setup wizard: a bounded quick scan of the chosen folders (names
// and sizes only) and a preview of the real engine on a sample of them. The
// wizard never starts a job: it fills a form, the form previews.

const (
	scanBudget     = 6 * time.Second
	scanMaxEntries = 300000
	sampleMax      = 28
	bigFile        = 50 << 30 // catalog.DefaultMaxSize
)

// Exclusion rules of the wizard, as bits of a mask (the frontend uses the
// names and maps them to the catalog rules, lib/wizard.js).
var wizardRules = []string{"system", "cloud", "dev", "junk", "big", "transient", "shortcuts"}

var (
	famOnce        sync.Once
	docExts, arExt map[string]bool
)

// familyOf maps an extension to a wizard family (frontend lib/regime.js).
func familyOf(ext string) string {
	famOnce.Do(func() {
		docExts, _, _ = kinds.PresetExts([]string{"document"})
		arExt, _, _ = kinds.PresetExts([]string{"archive"})
	})
	switch kinds.KindOf(ext) {
	case kinds.Photo, kinds.Image:
		return "img"
	case kinds.Video:
		return "video"
	case kinds.Audio:
		return "music"
	}
	switch {
	case docExts[ext]:
		return "doc"
	case arExt[ext]:
		return "arch"
	}
	return "rest"
}

// goldenClass places an extension against the golden scope: in, gifbmp (raster
// images without a capture date), gfx (graphics and design), "" (not an image).
func goldenClass(ext string) string {
	switch k := kinds.KindOf(ext); {
	case kinds.IsMedia(k):
		return "in"
	case k != kinds.Image:
		return ""
	case kinds.Format(ext) == "gif" || kinds.Format(ext) == "bmp":
		return "gifbmp"
	}
	return "gfx"
}

var ruleDirs = func() [3]map[string]bool {
	var out [3]map[string]bool
	for i, l := range [][]string{kinds.SystemDirs, kinds.CloudDirs, kinds.DevDirs} {
		out[i] = map[string]bool{}
		for _, d := range l {
			out[i][strings.ToLower(d)] = true
		}
	}
	return out
}()

// ruleMask returns the exclusion rules matching a file (rel = path under its root).
func ruleMask(rel string, size int64) int {
	mask := 0
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i, p := range parts {
		lp := strings.ToLower(p)
		if i < len(parts)-1 {
			for r, set := range ruleDirs {
				if set[lp] {
					mask |= 1 << r
				}
			}
		}
		if kinds.IsJunk(p) {
			mask |= 1 << 3
		}
	}
	if size > bigFile {
		mask |= 1 << 4
	}
	base := path.Base(filepath.ToSlash(rel))
	if kinds.IsTransient(base) {
		mask |= 1 << 5
	}
	if kinds.IsShortcut(kinds.Ext(base)) {
		mask |= 1 << 6
	}
	return mask
}

// alwaysSkipped: what no wizard choice ever catalogs (dotfiles, onefold
// temps, empty files, system extensions other than shortcuts). Unknown
// extensions and files without one belong to "everything else".
func alwaysSkipped(base, ext string, size int64) bool {
	return strings.HasPrefix(base, ".") || fsx.IsTemp(base) || size == 0 || (kinds.IsSystemExt(ext) && !kinds.IsShortcut(ext))
}

type wizardSample struct {
	Path string `json:"path"`
	Root string `json:"root"`
	Rel  string `json:"rel"`
}

type wizardScan struct {
	Files     int64               `json:"files"`
	Bytes     int64               `json:"bytes"`
	Secs      float64             `json:"secs"`
	Truncated bool                `json:"truncated"`
	Counts    map[string][2]int64 `json:"counts"` // "fam|mask" -> files, bytes
	Golden    map[string][2]int64 `json:"golden"` // "class|mask" -> files, bytes
	Rules     map[string]int64    `json:"rules"`  // rule -> files it matches
	Cases     map[string]int64    `json:"cases"`  // copies, dated, origin
	Sample    []wizardSample      `json:"sample"`
}

// scanFolders walks dirs within the scan budget and picks the sample: every
// family (distinct extensions first; for the golden goal mostly photos and
// videos, one example per class left out), the hard cases (copy names with
// their original, dates in names, origin prefixes) and files caught by each rule.
func scanFolders(dirs []string, goal string) wizardScan {
	res := wizardScan{Counts: map[string][2]int64{}, Golden: map[string][2]int64{}, Rules: map[string]int64{}, Cases: map[string]int64{}}
	start := time.Now()
	seen := map[string]bool{}
	perKey := map[string]int{}
	perExt := map[string]int{}
	perCase := map[string]int{}
	perRule := map[int]bool{}
	var sample []wizardSample
	add := func(p, root string) {
		if !seen[p] && len(sample) < sampleMax {
			seen[p] = true
			rel, _ := filepath.Rel(root, p)
			sample = append(sample, wizardSample{Path: p, Root: root, Rel: filepath.ToSlash(rel)})
		}
	}
	entries := 0
	for _, root := range dirs {
		root = filepath.Clean(root)
		filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			entries++
			if entries > scanMaxEntries || time.Since(start) > scanBudget {
				res.Truncated = true
				return filepath.SkipAll
			}
			if e.IsDir() {
				if p != root && strings.HasPrefix(e.Name(), ".") {
					return filepath.SkipDir // dot dirs are never cataloged
				}
				return nil
			}
			if !e.Type().IsRegular() {
				return nil
			}
			info, err := e.Info()
			if err != nil {
				return nil
			}
			base, ext, size := e.Name(), kinds.Ext(e.Name()), info.Size()
			if alwaysSkipped(base, ext, size) {
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			mask := ruleMask(rel, size)
			fam := familyOf(ext)
			res.Files++
			res.Bytes += size
			c := res.Counts[fam+"|"+itoa(mask)]
			res.Counts[fam+"|"+itoa(mask)] = [2]int64{c[0] + 1, c[1] + size}
			if gc := goldenClass(ext); gc != "" {
				g := res.Golden[gc+"|"+itoa(mask)]
				res.Golden[gc+"|"+itoa(mask)] = [2]int64{g[0] + 1, g[1] + size}
			}
			for i, r := range wizardRules {
				if mask&(1<<i) != 0 {
					res.Rules[r]++
				}
			}
			stem := strings.TrimSuffix(base, filepath.Ext(base))
			kase := ""
			switch {
			case names.IsCopyName(p):
				kase = "copies"
			case names.ParseLegacy(stem).Origin != "":
				kase = "origin"
			default:
				if _, ok := meta.DeduceFromFilename(base); ok {
					kase = "dated"
				}
			}
			if kase != "" {
				res.Cases[kase]++
			}
			switch {
			case mask != 0 && !perRule[mask] && len(perRule) < 4:
				perRule[mask] = true
				add(p, root)
			case kase != "" && perCase[kase] < 2:
				perCase[kase]++
				add(p, root)
				if kase == "copies" { // its original, when it sits next to it
					if orig := filepath.Join(filepath.Dir(p), names.ParseLegacy(stem).Clean+filepath.Ext(base)); orig != p {
						if fi, err := os.Stat(orig); err == nil && fi.Mode().IsRegular() {
							add(orig, root)
						}
					}
				}
			case mask == 0:
				key, limit, perExtMax := fam, 3, 1 // three families' files, distinct extensions
				if goal == "golden" {
					key, limit, perExtMax = goldenClass(ext), 1, 1
					if key == "in" {
						limit, perExtMax = 10, 4
					}
				}
				if perKey[key] < limit && perExt[key+ext] < perExtMax {
					perKey[key]++
					perExt[key+ext]++
					add(p, root)
				}
			}
			return nil
		})
	}
	res.Secs = time.Since(start).Seconds()
	res.Sample = sample
	if res.Sample == nil {
		res.Sample = []wizardSample{}
	}
	return res
}

func itoa(n int) string { return strconv.Itoa(n) }

func (s *Server) handleWizardScan(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dirs []string `json:"dirs"`
		Goal string   `json:"goal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, err)
		return
	}
	if err := absAll("dirs", req.Dirs...); err != nil || len(req.Dirs) == 0 {
		writeErr(w, errors.New("dirs: add at least one absolute folder"))
		return
	}
	writeJSONResp(w, 200, scanFolders(req.Dirs, req.Goal))
}

// wizardTag is a preview annotation: a message key of the frontend and its argument.
type wizardTag struct {
	K string `json:"k"`
	A string `json:"a,omitempty"`
}

type wizardRow struct {
	Src  string      `json:"src"`  // relative to its root
	Fam  string      `json:"fam"`  // wizard family of the file
	Dst  string      `json:"dst"`  // relative to the target ("" = not written)
	Skip bool        `json:"skip"` // left out by the scope or the rules
	Dup  bool        `json:"dup"`
	Del  bool        `json:"del"` // deleted (rearrange, dupes delete)
	Tags []wizardTag `json:"tags"`
}

type wizardPreviewReq struct {
	Goal      string                   `json:"goal"` // merge | rearrange | golden | dupes
	Sample    []wizardSample           `json:"sample"`
	Fams      []string                 `json:"fams"`
	Families  map[string]naming.Choice `json:"families"` // naming of each selected family
	Naming    string                   `json:"naming"`   // older clients: a spec (+ clean)
	Clean     bool                     `json:"clean"`
	Rules     map[string]bool          `json:"rules"`
	Exclude   string                   `json:"exclude"`
	Dupes     string                   `json:"dupes"`
	VideoTier bool                     `json:"videoTier"`
}

// previewSample runs the engine on the sample: scope and rules, metadata,
// sparse hash, then the golden plan or the naming template with collisions.
func previewSample(q wizardPreviewReq) ([]wizardRow, error) {
	var nm *naming.Naming
	if q.Goal == "merge" || q.Goal == "rearrange" {
		spec, err := settings.Naming{Families: q.Families, Spec: q.Naming, Clean: q.Clean}.Resolve()
		if err != nil {
			return nil, err
		}
		if err := naming.CheckTemplate(spec); err != nil {
			return nil, err
		}
		if nm, err = naming.Resolve(spec); err != nil {
			return nil, err
		}
	}
	fams := map[string]bool{}
	for _, f := range q.Fams {
		fams[f] = true
	}
	excl := catalog.SplitCSV(q.Exclude)
	rows := make([]wizardRow, len(q.Sample))
	var recs []catalog.Record
	idx := map[string]int{}
	for i, sm := range q.Sample {
		rows[i] = wizardRow{Src: sm.Rel, Fam: familyOf(kinds.Ext(sm.Path)), Tags: []wizardTag{}}
		row := &rows[i]
		if !filepath.IsAbs(sm.Path) || !filepath.IsAbs(sm.Root) {
			row.Skip = true
			continue
		}
		fi, err := os.Stat(sm.Path)
		if err != nil || !fi.Mode().IsRegular() {
			row.Skip = true
			row.Tags = append(row.Tags, wizardTag{K: "gone"})
			continue
		}
		ext := kinds.Ext(sm.Path)
		mask := ruleMask(sm.Rel, fi.Size())
		switch {
		case q.Goal == "golden" && goldenClass(ext) != "in":
			row.Skip = true
			row.Tags = append(row.Tags, wizardTag{K: "outGolden", A: strings.ToUpper(ext)})
			continue
		case q.Goal != "golden" && !fams["rest"] && !fams[familyOf(ext)]:
			row.Skip = true
			row.Tags = append(row.Tags, wizardTag{K: "notSelected"})
			continue
		}
		if r := firstRule(mask, q.Rules); r != "" {
			row.Skip = true
			row.Tags = append(row.Tags, wizardTag{K: "excl", A: r})
			continue
		}
		if e := firstSubstring(sm.Rel, excl); e != "" {
			row.Skip = true
			row.Tags = append(row.Tags, wizardTag{K: "exclText", A: e})
			continue
		}
		m, _ := meta.Extract(sm.Path)
		h, err := sparsehash.HashFile(sm.Path, 0, 0)
		if err != nil {
			row.Skip = true
			row.Tags = append(row.Tags, wizardTag{K: "gone"})
			continue
		}
		rec := catalog.Record{Path: sm.Path, Room: sm.Root, Size: fi.Size(), MTimeNs: fi.ModTime().UnixNano(), Hash: h.Hex, Meta: m}
		idx[sm.Path] = i
		recs = append(recs, rec)
		switch m.TimeSource {
		case "filename":
			row.Tags = append(row.Tags, wizardTag{K: "dateName"})
		case "filemtime", "none":
			row.Tags = append(row.Tags, wizardTag{K: "dateMtime"})
		}
	}
	if q.Goal == "golden" {
		previewGolden(q, recs, idx, rows)
		return rows, nil
	}
	previewTemplate(q, nm, recs, idx, rows)
	return rows, nil
}

func previewGolden(q wizardPreviewReq, recs []catalog.Record, idx map[string]int, rows []wizardRow) {
	o := plan.Options{VideoTier: q.VideoTier}
	for _, a := range plan.BuildActionsWith(plan.PlanWith(recs, o), o) {
		i := idx[a.SrcPath]
		rows[i].Dst = display(a.CanonRel)
		if kinds.IsRAW(kinds.Ext(a.SrcPath)) || kinds.Format(kinds.Ext(a.SrcPath)) == "mpeg-ps" {
			rows[i].Tags = append(rows[i].Tags, wizardTag{K: "byteCopy"})
		} else {
			rows[i].Tags = append(rows[i].Tags, wizardTag{K: "canon"})
		}
		for _, d := range a.Drops {
			if j, ok := idx[d.Path]; ok {
				rows[j].Dup = true
				rows[j].Tags = append(rows[j].Tags, wizardTag{K: "sameShot", A: rows[i].Src})
			}
		}
	}
}

func previewTemplate(q wizardPreviewReq, nm *naming.Naming, recs []catalog.Record, idx map[string]int, rows []wizardRow) {
	// keeper of identical contents, as the engine picks it: folder order, then a
	// clean name over a copy name ("x (1)", "Copy of x"), shorter path, lexical
	rootRank := map[string]int{}
	for _, sm := range q.Sample {
		if _, ok := rootRank[sm.Root]; !ok {
			rootRank[sm.Root] = len(rootRank)
		}
	}
	sorted := append([]catalog.Record(nil), recs...)
	sort.SliceStable(sorted, func(a, b int) bool {
		x, y := sorted[a], sorted[b]
		if rootRank[x.Room] != rootRank[y.Room] {
			return rootRank[x.Room] < rootRank[y.Room]
		}
		if cx, cy := names.IsCopyName(x.Path), names.IsCopyName(y.Path); cx != cy {
			return cy
		}
		if len(x.Path) != len(y.Path) {
			return len(x.Path) < len(y.Path)
		}
		return x.Path < y.Path
	})
	first := map[string]string{} // hash -> kept source
	for _, r := range sorted {
		if _, ok := first[r.Hash]; !ok {
			first[r.Hash] = rows[idx[r.Path]].Src
		}
	}
	var items []naming.Item
	var kept []int
	for _, r := range recs {
		i := idx[r.Path]
		if k := first[r.Hash]; k != rows[i].Src {
			rows[i].Dup = true
			switch {
			case q.Goal == "rearrange" && q.Dupes != "report":
				rows[i].Del = true
				rows[i].Tags = append(rows[i].Tags, wizardTag{K: "dupDelete", A: k})
			case q.Goal == "merge":
				rows[i].Tags = append(rows[i].Tags, wizardTag{K: "dupSkip", A: k})
			default:
				rows[i].Tags = append(rows[i].Tags, wizardTag{K: "dupOf", A: k})
			}
			continue
		}
		if q.Goal == "dupes" {
			continue
		}
		cur := ""
		if q.Goal == "rearrange" {
			cur = rows[i].Src
		}
		items = append(items, naming.Item{Current: cur, Target: nm.Target(naming.VarsOf(r)), Tie: r.Hash + r.Path})
		kept = append(kept, i)
		stem := strings.TrimSuffix(filepath.Base(r.Path), filepath.Ext(r.Path))
		if names.HasMarkers(stem) {
			rows[i].Tags = append(rows[i].Tags, wizardTag{K: "markers"})
		}
	}
	got := naming.Assign(items, map[string]bool{})
	for n, i := range kept {
		rows[i].Dst = display(got[n])
		if !strings.EqualFold(got[n], items[n].Target) {
			rows[i].Tags = append(rows[i].Tags, wizardTag{K: "collision"})
		}
	}
}

func firstRule(mask int, on map[string]bool) string {
	for i, r := range wizardRules {
		if mask&(1<<i) != 0 && on[r] {
			return r
		}
	}
	return ""
}

func firstSubstring(rel string, subs []string) string {
	for _, e := range subs {
		if strings.Contains(rel, e) {
			return e
		}
	}
	return ""
}

func (s *Server) handleWizardPreview(w http.ResponseWriter, r *http.Request) {
	var q wizardPreviewReq
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeErr(w, err)
		return
	}
	rows, err := previewSample(q)
	if err != nil {
		writeJSONResp(w, 200, map[string]any{"error": err.Error(), "rows": []wizardRow{}})
		return
	}
	writeJSONResp(w, 200, map[string]any{"rows": rows})
}

// ---------------------------------------------------------------- recipes

// Recipe is a named set of wizard answers, kept in the state dir.
type Recipe struct {
	ID      string          `json:"id"`
	Name    string          `json:"name"`
	Goal    string          `json:"goal"`
	Created time.Time       `json:"created"`
	Answers json.RawMessage `json:"answers"`
}

var recipesMu sync.Mutex

func (s *Server) recipesPath() string { return filepath.Join(s.mgr.st.dir, "recipes.json") }

func (s *Server) loadRecipes() []Recipe {
	var rs []Recipe
	if err := readJSON(s.recipesPath(), &rs); err != nil && !errors.Is(err, os.ErrNotExist) {
		Logf("recipes: %v", err)
	}
	if rs == nil {
		rs = []Recipe{}
	}
	sort.Slice(rs, func(a, b int) bool { return rs[a].Created.After(rs[b].Created) })
	return rs
}

// handleRecipes: GET lists, POST adds {name, goal, answers}, DELETE /api/recipes/<id>.
func (s *Server) handleRecipes(w http.ResponseWriter, r *http.Request) {
	recipesMu.Lock()
	defer recipesMu.Unlock()
	rs := s.loadRecipes()
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		var in Recipe
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeErr(w, err)
			return
		}
		if in.Name = strings.TrimSpace(in.Name); in.Name == "" || len(in.Answers) == 0 {
			writeErr(w, errors.New("recipe: a name and the answers are required"))
			return
		}
		b := make([]byte, 6)
		rand.Read(b)
		in.ID, in.Created = hex.EncodeToString(b), time.Now()
		rs = append([]Recipe{in}, rs...)
	case http.MethodDelete:
		id := strings.TrimPrefix(r.URL.Path, "/api/recipes/")
		out := rs[:0]
		for _, x := range rs {
			if x.ID != id {
				out = append(out, x)
			}
		}
		rs = out
	default:
		http.Error(w, "method", http.StatusMethodNotAllowed)
		return
	}
	if r.Method != http.MethodGet {
		if err := writeJSON(s.recipesPath(), rs); err != nil {
			writeErr(w, err)
			return
		}
	}
	writeJSONResp(w, 200, map[string]any{"recipes": rs})
}

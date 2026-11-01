// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package plan is the planner: given the catalog manifest(s), it applies the
// dedup and elects the golden winner per unique logical asset. It is PURE
// computation over the manifest (only --verify-full reads source bytes).
//
// Media kinds (photo/video):
//  0. sparse-hash  -> collapse byte-identical copies (exact dupes)
//  1. tag-identity -> cluster VARIANTS of the same capture (HQ vs LQ vs copy).
//     The cluster key is what is INVARIANT across variants: canonical time +
//     duration bucket (video) or sub-second (photo). Device is NOT in the key,
//     because an LQ re-encode LOSES its vendor metadata and must still cluster
//     with its HQ original; a device-partition keeps genuinely-distinct captures
//     (same second, DIFFERENT non-empty device) apart.
//     Winner: resolution, HDR, byte size. Ties keep the incumbent.
//
// Generic kinds (everything else): exact hash only; deterministic winner by
// room order, clean name, oldest mtime, shorter path, lexical path.
package plan

import (
	"fmt"
	"math"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/kinds"
	"github.com/mauriziofonte/onefold/internal/meta"
	"github.com/mauriziofonte/onefold/internal/names"
	"github.com/mauriziofonte/onefold/internal/naming"
	"github.com/mauriziofonte/onefold/internal/perf"
	"github.com/mauriziofonte/onefold/internal/runctl"
	"github.com/mauriziofonte/onefold/internal/sparsehash"
)

// Options tune planning. The zero value reproduces the historical media plan
// minus the owner stamp (Artist), which is configured per user.
type Options struct {
	VerifyFull bool   // full-content hash on every multi-member hash group
	Layout     string // generic-kind naming: layout alias, preset or template ("" = dedupe)
	FixExt     bool   // generic kinds: rename to the magic-byte extension when unambiguous
	VideoTier  bool   // videos: VIDEOS/<YEAR>/<tier>/...
	Artist     string // owner stamped on media copies ("" = none); generic kinds are never stamped
	// SelfContained writes the winner's sub-second, duration and dimensions
	// into each action, so the plan alone identifies a shot; off = the
	// historical action bytes.
	SelfContained bool
	// VideoTolerance (seconds, 0 = off: the historical rounded-duration
	// bucket) clusters the videos of one start time whose durations differ by
	// at most this much: a re-encoded copy whose length shifted across a
	// rounding edge is then the lower-res variant of its original.
	VideoTolerance float64
	// FilenameSeq: a photo or video dated by a file name without a time of
	// day (default 10:00:00 / 12:00:00) keeps the counter of that name in
	// its canonical name (IMG-20200101-WA0007.jpg ->
	// IMG_20200101_100000_WA0007.jpg) instead of a _NN given by hash order.
	FilenameSeq bool
}

// Layouts lists the historical --layout aliases (any naming preset or
// template is accepted too).
var Layouts = []string{"dedupe", "timeline", "document", "music", "device"}

// ValidLayout reports whether l resolves to a naming ("" = dedupe).
func ValidLayout(l string) bool {
	_, err := layoutOf(l)
	return err == nil
}

// Drop is a non-winner file absorbed by a golden asset (recorded, not deleted).
type Drop struct {
	Host   string `json:"host"`
	Path   string `json:"path"`
	Reason string `json:"reason"`
	Size   int64  `json:"size"`
}

// Action is one line of the materialization plan: which source becomes which
// canonical golden file, with the silvering intent and the drops it absorbs.
// The plan JSONL is device-agnostic (each record carries its source host), so
// runs from several machines glue into ONE file applied by `onefold apply`.
type Action struct {
	ID          string `json:"id"`   // winner sparse-hash
	Kind        string `json:"kind"` // photo | video | image | audio | document | text | archive | other
	SrcHost     string `json:"srcHost"`
	SrcPath     string `json:"srcPath"`
	SrcSize     int64  `json:"srcSize"`
	CanonName   string `json:"canonName"`
	CanonRel    string `json:"canonRel"` // canonical relative path in the golden tree
	Time        string `json:"time"`
	Device      string `json:"device"`      // RAW make/model
	CanonDevice string `json:"canonDevice"` // canonical device name, stamped into the silvered copy
	Category    string `json:"category"`
	IsEdit      bool   `json:"isEdit"`
	IsHDR       bool   `json:"isHDR"`
	Artist      string `json:"artist"` // ownership to embed on the silvered copy (media only)
	Drops       []Drop `json:"drops"`
	// The winner's own metadata (Options.SelfContained; omitted when zero or
	// not requested): with them a plan matches new files without its manifests.
	SubSecNs    int64   `json:"subSecNs,omitempty"`
	DurationSec float64 `json:"durationSec,omitempty"`
	Width       int     `json:"width,omitempty"`
	Height      int     `json:"height,omitempty"`
}

// IsMediaAction reports whether the action goes through the media silvering path.
func (a Action) IsMediaAction() bool { return kinds.IsMedia(a.Kind) || a.Kind == "" }

// LoadManifests reads the manifests of a plan (catalog.LoadManifest: header,
// same zone) and refuses records without a host label: the host routes
// `apply --only-host` and tells apart equal paths of different machines.
func LoadManifests(paths []string) ([]catalog.Record, error) {
	var recs []catalog.Record
	for _, p := range paths {
		rs, err := catalog.LoadManifest(p)
		if err != nil {
			return nil, err
		}
		for i := range rs {
			if rs[i].Host == "" {
				return nil, fmt.Errorf("%s: %s has no host label: catalog it again (the host defaults to this machine's name, -host sets it)", p, rs[i].Path)
			}
		}
		recs = append(recs, rs...)
	}
	return recs, nil
}

func compactT(t string) string {
	if len(t) != 19 {
		return "00000000_000000"
	}
	return t[0:4] + t[5:7] + t[8:10] + "_" + t[11:13] + t[14:16] + t[17:19]
}

func yearOf(t string) string {
	if len(t) >= 4 {
		return t[:4]
	}
	return "0000"
}

// canonNameRel computes the canonical filename and relative golden path.
func canonNameRel(a Asset, o Options) (name, rel string) {
	w := a.Winner
	m := w.Meta
	if !kinds.IsMedia(m.Kind) && m.Kind != "" {
		return genericNameRel(w, o)
	}
	ext := strings.ToLower(filepath.Ext(w.Path))
	prefix := m.Prefix
	if prefix == "" {
		prefix = "IMG"
	}
	name = prefix + "_" + compactT(m.Time) + ext
	if o.FilenameSeq && m.TimeSource == "filename" {
		if c := meta.FilenameCounter(filepath.Base(w.Path)); c != "" {
			name = prefix + "_" + compactT(m.Time) + "_" + names.SanitizeName(c) + ext
		}
	}
	if m.Kind == "photo" {
		fmtdir := strings.ToUpper(kinds.NormalizeExt(strings.TrimPrefix(ext, ".")))
		return name, path.Join("PICTURES", fmtdir, yearOf(m.Time), name)
	}
	if o.VideoTier {
		return name, path.Join("VIDEOS", yearOf(m.Time), orUnknown(names.VideoTier(m.Width, m.Height)), name)
	}
	return name, path.Join("VIDEOS", yearOf(m.Time), name)
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// layoutCache holds the compiled -layout namings (resolved once per process).
var layoutCache sync.Map

func layoutOf(l string) (*naming.Naming, error) {
	if l == "" {
		l = "dedupe"
	}
	if n, ok := layoutCache.Load(l); ok {
		return n.(*naming.Naming), nil
	}
	n, err := naming.Layout(l)
	if err != nil {
		return nil, err
	}
	layoutCache.Store(l, n)
	return n, nil
}

// genericNameRel renders a non-media winner through the naming engine.
func genericNameRel(w catalog.Record, o Options) (name, rel string) {
	nm, err := layoutOf(o.Layout)
	if err != nil { // rejected upfront by ValidLayout
		nm, _ = layoutOf("")
	}
	v := naming.VarsOf(w)
	if o.FixExt && w.Meta.SniffExt != "" {
		v.SetExt(w.Meta.SniffExt)
	}
	rel = nm.Target(v)
	return path.Base(rel), rel
}

// BuildActions is BuildActionsWith with default options.
func BuildActions(r *Result) []Action { return BuildActionsWith(r, Options{}) }

// BuildActionsWith turns the golden assets into the materialization plan,
// resolving canonical-path collisions (case-insensitive, so the tree is valid
// on Windows too) with a deterministic _NN suffix ordered by winner hash.
func BuildActionsWith(r *Result, o Options) []Action {
	acts := make([]Action, 0, len(r.Golden))
	for _, a := range r.Golden {
		w := a.Winner
		name, rel := canonNameRel(a, o)
		act := Action{
			ID: w.Hash, Kind: w.Meta.Kind,
			SrcHost: w.Host, SrcPath: w.Path, SrcSize: w.Size,
			CanonName: name, CanonRel: rel,
			Time: w.Meta.Time, Device: w.Meta.Device, CanonDevice: w.Meta.CanonDevice, Category: w.Meta.Category,
			IsEdit: isEdit(w), IsHDR: isHDR(w),
		}
		if act.IsMediaAction() {
			act.Artist = o.Artist
		}
		if o.SelfContained {
			act.SubSecNs, act.DurationSec, act.Width, act.Height = w.Meta.SubSecNs, w.Meta.DurationSec, w.Meta.Width, w.Meta.Height
		}
		for _, m := range a.Members {
			if m.Path == w.Path && m.Host == w.Host {
				continue
			}
			act.Drops = append(act.Drops, Drop{Host: m.Host, Path: m.Path, Reason: a.Reasons[memberKey(m)], Size: m.Size})
		}
		acts = append(acts, act)
	}
	byRel := map[string][]int{}
	for i := range acts {
		k := strings.ToLower(acts[i].CanonRel)
		byRel[k] = append(byRel[k], i)
	}
	for _, idxs := range byRel {
		if len(idxs) < 2 {
			continue
		}
		sort.Slice(idxs, func(x, y int) bool {
			if acts[idxs[x]].ID != acts[idxs[y]].ID {
				return acts[idxs[x]].ID < acts[idxs[y]].ID
			}
			return acts[idxs[x]].SrcPath < acts[idxs[y]].SrcPath
		})
		for n, i := range idxs {
			if n == 0 {
				continue // first keeps the bare name
			}
			acts[i].CanonRel = names.WithNN(acts[i].CanonRel, n)
			acts[i].CanonName = path.Base(acts[i].CanonRel)
		}
	}
	return acts
}

// editCats are photo categories that are PARALLEL artifacts (Policy A): a
// Snapseed/Photoshop/Lightroom edit or a Gigapixel enlargement is a distinct
// creative output to KEEP, never dropped as an inferior variant.
var editCats = map[string]bool{"snapseed": true, "editing": true, "photoshop": true, "enlargement": true}

// IsEdit reports whether a record is an edit (its own asset, Policy A).
func IsEdit(r catalog.Record) bool { return isEdit(r) }

func isEdit(r catalog.Record) bool {
	return (kinds.IsMedia(r.Meta.Kind) || r.Meta.Kind == "") && editCats[r.Meta.Category]
}

// isHDR flags the HDR output of a capture (Policy C: keep HDR by default).
func isHDR(r catalog.Record) bool {
	return strings.Contains(strings.ToUpper(filepath.Base(r.Path)), "_HDR")
}

// memberKey identifies a member across hosts (same path may exist on two machines).
func memberKey(r catalog.Record) string { return r.Host + "\x00" + r.Path }

// Asset is one unique logical capture: the elected winner plus every member file
// (variants + exact copies) with the reason each non-winner was dropped.
type Asset struct {
	Winner  catalog.Record
	Members []catalog.Record  // Members[0] is the winner
	Reasons map[string]string // memberKey -> reason (winner absent)
}

// Result is the planner outcome (read-only, for inspection before any apply).
type Result struct {
	InputFiles   int
	Assets       int   // unique logical assets = golden count
	ExactDupes   int   // byte-identical copies dropped
	LowerRes     int   // lower-resolution variants (LQ) dropped
	SmallerVar   int   // same-res but smaller variants dropped
	HashOnly     int   // assets deduped by exact hash only (photos w/o subsec + timeless + generic)
	GoldenBytes  int64 // sum of winner sizes
	ReclaimBytes int64 // sum of dropped members' sizes
	MultiExample []Asset
	Golden       []Asset // every elected asset (for the materialization plan)

	// --verify-full
	VerifiedGroups   int // multi-member hash groups whose members were full-hashed
	VerifySplits     int // groups that turned out to contain different content
	VerifyUnreadable int // members that could not be read (kept, never dropped)
}

// ShotKey is the capture identity of the time clusters: a dated video by its
// time and rounded duration, a dated photo with a sub-second by time,
// sub-second and canonical format (one winner per format). "" = the file is
// identified by its bytes only.
func ShotKey(kind, time string, subSecNs int64, durationSec float64, path string) string {
	switch {
	case kind == "video" && time != "":
		return time + "|d" + durBucket(durationSec)
	case kind == "photo" && time != "" && subSecNs != 0:
		return time + "|s" + strconv.FormatInt(subSecNs, 10) + "|" + kinds.NormalizeExt(kinds.Ext(path))
	}
	return ""
}

// mergeVideoDurations joins the video clusters of one start time whose
// durations chain within tol seconds of the shortest member of the run:
// sorted by duration, a member starts a new cluster when it is more than tol
// longer than the first member of the current one. Photo clusters are kept.
func mergeVideoDurations(dated map[string][]catalog.Record, tol float64) map[string][]catalog.Record {
	byTime := map[string][]catalog.Record{}
	out := map[string][]catalog.Record{}
	for k, rs := range dated {
		if rs[0].Meta.Kind != "video" {
			out[k] = rs
			continue
		}
		byTime[rs[0].Meta.Time] = append(byTime[rs[0].Meta.Time], rs...)
	}
	for t, rs := range byTime {
		sort.SliceStable(rs, func(i, j int) bool {
			if rs[i].Meta.DurationSec != rs[j].Meta.DurationSec {
				return rs[i].Meta.DurationSec < rs[j].Meta.DurationSec
			}
			return memberKey(rs[i]) < memberKey(rs[j])
		})
		anchor := 0
		for i := range rs {
			if rs[i].Meta.DurationSec-rs[anchor].Meta.DurationSec > tol {
				anchor = i
			}
			k := t + "|d" + strconv.FormatFloat(rs[anchor].Meta.DurationSec, 'f', -1, 64) + "|" + memberKey(rs[anchor])
			out[k] = append(out[k], rs[i])
		}
	}
	return out
}

func durBucket(sec float64) string { return strconv.Itoa(int(math.Round(sec))) }

func res(r catalog.Record) int64 { return int64(r.Meta.Width) * int64(r.Meta.Height) }

// Plan is PlanWith with default options (historical media behavior).
func Plan(recs []catalog.Record) *Result { return PlanWith(recs, Options{}) }

// PlanWith applies the dedup+winner logic over all records.
func PlanWith(recs []catalog.Record, o Options) *Result {
	res_ := &Result{InputFiles: len(recs)}
	if o.VerifyFull {
		recs = verifyFull(recs, res_)
	}

	roomRank := map[string]int{}
	for _, r := range recs {
		if _, ok := roomRank[r.Room]; !ok {
			roomRank[r.Room] = len(roomRank)
		}
	}

	dated := map[string][]catalog.Record{}    // time|duration -> members (VIDEO only)
	hashOnly := map[string][]catalog.Record{} // hash          -> members
	for _, r := range recs {
		// VIDEO clusters by time+duration. PHOTO clusters by time+SUB-SECOND
		// (the sub-second VALUE disambiguates burst frames - same second, same
		// device - so we catch non-byte-identical same-captures, e.g. a phone
		// original vs a jpegoptim'd room copy, WITHOUT merging distinct bursts).
		// A photo with NO sub-second falls back to exact-hash (safe), as does any
		// timeless record and every generic kind. The canonical photo format is
		// part of the capture identity: the RAW and the JPG (or the HEIC and its
		// exported JPG) of one shot are two assets, one winner per format.
		switch key := ShotKey(r.Meta.Kind, r.Meta.Time, r.Meta.SubSecNs, r.Meta.DurationSec, r.Path); {
		case key != "":
			dated[key] = append(dated[key], r)
		case r.Meta.Kind == "photo":
			// byte-identical photos under two formats (a PNG saved as .jpg) are
			// two assets too: the mislabeled copy never swallows the real one
			key := r.Hash + "|" + kinds.NormalizeExt(kinds.Ext(r.Path))
			hashOnly[key] = append(hashOnly[key], r)
		default:
			hashOnly[r.Hash] = append(hashOnly[r.Hash], r)
		}
	}
	if o.VideoTolerance > 0 {
		dated = mergeVideoDurations(dated, o.VideoTolerance)
	}

	// finish records the asset and its drop statistics.
	finish := func(a Asset) {
		for _, m := range a.Members[1:] {
			switch a.Reasons[memberKey(m)] {
			case "exact-dupe":
				res_.ExactDupes++
			case "lower-res":
				res_.LowerRes++
			case "smaller":
				res_.SmallerVar++
			}
			res_.ReclaimBytes += m.Size
		}
		res_.GoldenBytes += a.Winner.Size
		res_.Assets++
		res_.Golden = append(res_.Golden, a)
		if len(a.Members) > 1 && len(res_.MultiExample) < 12 {
			res_.MultiExample = append(res_.MultiExample, a)
		}
	}

	// copyLess orders interchangeable copies: a name that is not a copy name
	// ("x (1).jpg"), the oldest mtime, the shortest path, then the path
	// itself. A total order: the winner never depends on the order the
	// catalog workers emitted the records in.
	copyLess := func(a, b catalog.Record) bool {
		if da, db := names.IsCopyName(a.Path), names.IsCopyName(b.Path); da != db {
			return !da
		}
		if ma, mb := mtimeKey(a), mtimeKey(b); ma != mb {
			return ma < mb
		}
		if len(a.Path) != len(b.Path) {
			return len(a.Path) < len(b.Path)
		}
		return a.Path < b.Path
	}

	// makeMedia elects the winner of a set of true media variants and records drops.
	makeMedia := func(members []catalog.Record) {
		// Winner: resolution, then HDR (Policy C), then byte size (larger wins -
		// e.g. a phone original over a jpegoptim'd room copy); equal on all
		// three (byte-identical copies, mostly): the earlier room, then as for
		// generic files.
		sort.SliceStable(members, func(i, j int) bool {
			if ri, rj := res(members[i]), res(members[j]); ri != rj {
				return ri > rj
			}
			if hi, hj := isHDR(members[i]), isHDR(members[j]); hi != hj {
				return hi
			}
			if members[i].Size != members[j].Size {
				return members[i].Size > members[j].Size
			}
			if ra, rb := roomRank[members[i].Room], roomRank[members[j].Room]; ra != rb {
				return ra < rb
			}
			return copyLess(members[i], members[j])
		})
		w := members[0]
		a := Asset{Winner: w, Members: members, Reasons: map[string]string{}}
		for _, m := range members[1:] {
			switch {
			case m.Hash == w.Hash:
				a.Reasons[memberKey(m)] = "exact-dupe"
			case res(m) < res(w):
				a.Reasons[memberKey(m)] = "lower-res"
			case m.Size < w.Size:
				a.Reasons[memberKey(m)] = "smaller"
			default:
				a.Reasons[memberKey(m)] = "variant"
			}
		}
		finish(a)
	}

	// makeGeneric elects the winner of a byte-identical generic group.
	makeGeneric := func(members []catalog.Record) {
		sort.SliceStable(members, func(i, j int) bool {
			if ra, rb := roomRank[members[i].Room], roomRank[members[j].Room]; ra != rb {
				return ra < rb
			}
			return copyLess(members[i], members[j])
		})
		a := Asset{Winner: members[0], Members: members, Reasons: map[string]string{}}
		for _, m := range members[1:] {
			a.Reasons[memberKey(m)] = "exact-dupe"
		}
		finish(a)
	}

	// splitCluster: within a time+duration cluster, separate genuinely-distinct
	// captures (different non-empty device) so they are NOT dropped as variants.
	// Members with empty device (LQ) or the winner's device are true variants.
	var splitCluster func(members []catalog.Record)
	splitCluster = func(members []catalog.Record) {
		best := members[0]
		for _, m := range members {
			if res(m) > res(best) || (res(m) == res(best) && m.Size > best.Size) {
				best = m
			}
		}
		var kin, aliens []catalog.Record
		for _, m := range members {
			if m.Meta.Device == "" || m.Meta.Device == best.Meta.Device {
				kin = append(kin, m)
			} else {
				aliens = append(aliens, m)
			}
		}
		makeMedia(kin)
		if len(aliens) > 0 {
			splitCluster(aliens) // distinct device -> own asset(s)
		}
	}

	for _, members := range dated {
		// Policy A: pull out edits (parallel artifacts) - they are NEVER dropped.
		// Identical copies of the same edit still collapse (by hash); each
		// distinct edit becomes its own golden asset.
		var edits, base []catalog.Record
		for _, m := range members {
			if isEdit(m) {
				edits = append(edits, m)
			} else {
				base = append(base, m)
			}
		}
		if len(base) > 0 {
			splitCluster(base)
		}
		editByHash := map[string][]catalog.Record{}
		for _, e := range edits {
			editByHash[e.Hash] = append(editByHash[e.Hash], e)
		}
		for _, g := range editByHash {
			makeMedia(g)
		}
	}

	// Media winners by hash: a generic copy of a media winner (e.g. IMG_1.jpg
	// saved as IMG_1.jpg.bak) is absorbed as its exact dupe.
	mediaWinner := map[string]int{}
	for i, a := range res_.Golden {
		mediaWinner[a.Winner.Hash] = i
	}
	var genericGroups [][]catalog.Record
	for _, members := range hashOnly {
		var media, generic []catalog.Record
		for _, m := range members {
			if kinds.IsMedia(m.Meta.Kind) || m.Meta.Kind == "" {
				media = append(media, m)
			} else {
				generic = append(generic, m)
			}
		}
		if len(media) > 0 {
			makeMedia(media)
			res_.HashOnly++
			mediaWinner[res_.Golden[len(res_.Golden)-1].Winner.Hash] = len(res_.Golden) - 1
		}
		if len(generic) > 0 {
			genericGroups = append(genericGroups, generic)
		}
	}
	for _, generic := range genericGroups {
		// Never let a mislabeled media file (e.g. a PNG named .jpg) swallow the
		// correctly named copy: both stay (conservative).
		if idx, ok := mediaWinner[generic[0].Hash]; ok && !res_.Golden[idx].Winner.Meta.ExtMismatch {
			absorb(res_, idx, generic)
			continue
		}
		makeGeneric(generic)
		res_.HashOnly++
	}
	return res_
}

// absorb attaches byte-identical generic copies to an existing media asset.
func absorb(r *Result, idx int, extra []catalog.Record) {
	a := &r.Golden[idx]
	for _, m := range extra {
		a.Members = append(a.Members, m)
		a.Reasons[memberKey(m)] = "exact-dupe"
		r.ExactDupes++
		r.ReclaimBytes += m.Size
	}
}

// mtimeKey orders oldest first; unknown mtime (0) sorts last.
func mtimeKey(r catalog.Record) int64 {
	if r.MTimeNs == 0 {
		return math.MaxInt64
	}
	return r.MTimeNs
}

// VerifyStats reports what VerifyGroups did.
type VerifyStats struct {
	Groups     int   // multi-member sparse-hash groups full-hashed
	Splits     int   // groups whose members turned out to differ
	Unreadable int   // members that could not be read (made unique: never dropped)
	Bytes      int64 // bytes read
}

// VerifyGroups re-hashes over the FULL content every member of every
// multi-member hash group (unless all members already carry a full hash).
// Groups with differing content are split (hash suffixed with the full-hash
// prefix); unreadable members get a unique hash so they are never treated as
// duplicates. Runs on `workers` goroutines.
func VerifyGroups(recs []catalog.Record, workers int) ([]catalog.Record, VerifyStats) {
	return VerifyGroupsProgress(recs, workers, nil)
}

// VerifyGroupsProgress is VerifyGroups reporting the bytes it reads to prog.
func VerifyGroupsProgress(recs []catalog.Record, workers int, prog catalog.Progress) ([]catalog.Record, VerifyStats) {
	return VerifyGroupsWith(recs, workers, prog, nil)
}

// VerifyGroupsWith is VerifyGroupsProgress with a pause/stop gate. A stopped
// gate skips the remaining reads (those members count as unreadable, so they
// are never treated as duplicates); callers check gate.Stopped() and discard
// the result.
func VerifyGroupsWith(recs []catalog.Record, workers int, prog catalog.Progress, gate *runctl.Gate) ([]catalog.Record, VerifyStats) {
	var st VerifyStats
	byHash := map[string][]int{}
	for i, rec := range recs {
		byHash[rec.Hash] = append(byHash[rec.Hash], i)
	}
	out := append([]catalog.Record(nil), recs...)
	var todo [][]int
	for _, idxs := range byHash {
		if len(idxs) < 2 {
			continue
		}
		allFull := true
		for _, i := range idxs {
			allFull = allFull && recs[i].Full
		}
		if !allFull {
			todo = append(todo, idxs)
		}
	}
	st.Groups = len(todo)
	full := make([]string, len(recs))
	jobs := make(chan int)
	var wg sync.WaitGroup
	var mu sync.Mutex
	if workers < 1 {
		workers = 1
	}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				var fh string
				var err error
				t0, tw := perf.Start(), time.Now()
				if gate.Wait() {
					fh, err = sparsehash.FullFile(recs[i].Path)
				} else {
					err = runctl.ErrStopped
				}
				perf.End("verify-full", t0, recs[i].Size)
				if prog != nil {
					if err != nil {
						if er, ok := prog.(interface{ ErrorAt(string, error) }); ok && err != runctl.ErrStopped {
							er.ErrorAt(recs[i].Path, fmt.Errorf("unreadable, kept apart: %w", err))
						} else {
							prog.Err()
						}
						prog.Advance(recs[i].Size)
					} else {
						prog.Item(recs[i].Size, time.Since(tw))
					}
				}
				mu.Lock()
				if err != nil {
					st.Unreadable++
					fh = fmt.Sprintf("unreadable%08d", i)
				} else {
					st.Bytes += recs[i].Size
				}
				full[i] = fh
				mu.Unlock()
			}
		}()
	}
	var reads []int
	for _, idxs := range todo {
		for _, i := range idxs {
			if c := recs[i].ContentHash; c != "" {
				full[i] = c // known whole-content digest (cache / full hash): no re-read
				continue
			}
			reads = append(reads, i)
			if prog != nil {
				prog.AddTotal(recs[i].Size)
			}
		}
	}
	if prog != nil {
		prog.TotalKnown()
	}
	for _, i := range reads {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	for _, idxs := range todo {
		for _, i := range idxs {
			if !strings.HasPrefix(full[i], "unreadable") {
				out[i].ContentHash = full[i]
			}
		}
	}
	for _, idxs := range todo {
		distinct := map[string]bool{}
		for _, i := range idxs {
			distinct[full[i]] = true
		}
		if len(distinct) == 1 {
			continue
		}
		st.Splits++
		for _, i := range idxs {
			f := full[i]
			// Shorten real digests only: the unreadable token is unique by its
			// full index, truncating it would make two unreadable members equal.
			if len(f) > 16 && !strings.HasPrefix(f, "unreadable") {
				f = f[:16]
			}
			out[i].Hash = recs[i].Hash + "-" + f
		}
	}
	return out, st
}

func verifyFull(recs []catalog.Record, r *Result) []catalog.Record {
	out, st := VerifyGroups(recs, runtime.NumCPU())
	r.VerifiedGroups, r.VerifySplits, r.VerifyUnreadable = st.Groups, st.Splits, st.Unreadable
	return out
}

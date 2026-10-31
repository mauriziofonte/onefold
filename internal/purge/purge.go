// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package purge deletes the input files of an executed merge whose content is
// in the output: `onefold purge-sources` and the GUI action
// after a merge. Dry-run unless Execute.
//
// Guarantees (pinned by purge_test.go):
//   - only files of the merge's Settled list, inside one of its input rooms,
//     never under the output, never the output's own files;
//   - right before each deletion the source and its keeper in the output are
//     re-read in full: equal content (and equal to the recorded content hash)
//     or nothing is deleted;
//   - every deletion is journaled (fsync'ed JSONL under <output>/.onefold);
//   - only directories emptied by the run are removed, deepest first, never a
//     room root nor the output or a directory holding it; OS droppings
//     (kinds.JunkNames) left alone in such a directory are deleted with it.
package purge

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mauriziofonte/onefold/internal/kinds"
	"github.com/mauriziofonte/onefold/internal/merge"
	"github.com/mauriziofonte/onefold/internal/report"
	"github.com/mauriziofonte/onefold/internal/runctl"
	"github.com/mauriziofonte/onefold/internal/sparsehash"
)

// Options configure a purge.
type Options struct {
	Items   []merge.Settle // merge.Result.Settled
	Output  string         // the merge output (keepers live there)
	Rooms   []string       // the merge input rooms (sources live there)
	Execute bool
	// Trash moves the files to the Recycle Bin instead of deleting them
	// (Windows only).
	Trash bool
	// Expect, when >= 0, must equal the number of files an execute deletes
	// (the typed confirmation); checked before anything is touched.
	Expect    int
	Workers   int
	Log       io.Writer // status lines; nil = silent
	Heartbeat time.Duration
	Gate      *runctl.Gate // pause/stop between files (GUI); nil = never
	// BeforeExecute is called with the plan before the journal is written; an
	// error aborts with nothing touched (GUI: executed plan == confirmed plan).
	BeforeExecute func(*Result) error
}

// Refusal is an item of the list that is not eligible, with why.
type Refusal struct {
	Src    string `json:"src"`
	Reason string `json:"reason"`
}

// Result is the plan and, after an execute, the outcome.
type Result struct {
	Output     string         `json:"output"`
	Rooms      []string       `json:"rooms"`
	Candidates []merge.Settle `json:"candidates"`
	Bytes      int64          `json:"bytes"`
	Refused    []Refusal      `json:"refused,omitempty"`

	Journal      string   `json:"journal,omitempty"`
	Deleted      int      `json:"deleted"`
	DeletedBytes int64    `json:"deletedBytes"`
	Gone         int      `json:"gone"` // already missing (a resumed run)
	Failed       []string `json:"failed,omitempty"`
	DirsRemoved  int      `json:"dirsRemoved"`
	JunkRemoved  int      `json:"junkRemoved"`
	Pending      int      `json:"pending,omitempty"` // not done: stopped
	Stopped      bool     `json:"stopped,omitempty"`
}

// ErrTrashUnsupported: the Recycle Bin option on an OS without one.
var ErrTrashUnsupported = errors.New("the Recycle Bin option is available on Windows only")

func fold(p string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}

// within reports whether p is dir or inside dir.
func within(p, dir string) bool {
	fp, fd := fold(filepath.Clean(p)), fold(filepath.Clean(dir))
	return fp == fd || strings.HasPrefix(fp, strings.TrimSuffix(fd, string(filepath.Separator))+string(filepath.Separator))
}

// Plan sorts the items into candidates and refusals.
func Plan(o Options) (*Result, error) {
	if o.Trash && !trashSupported {
		return nil, ErrTrashUnsupported
	}
	if strings.TrimSpace(o.Output) == "" || !filepath.IsAbs(o.Output) {
		return nil, fmt.Errorf("output %q is not an absolute path", o.Output)
	}
	res := &Result{Output: filepath.Clean(o.Output)}
	for _, r := range o.Rooms {
		if !filepath.IsAbs(r) {
			return nil, fmt.Errorf("room %q is not an absolute path", r)
		}
		res.Rooms = append(res.Rooms, filepath.Clean(r))
	}
	if o.Trash {
		vols := append([]string(nil), res.Rooms...)
		for _, it := range o.Items {
			if filepath.IsAbs(it.Src) {
				vols = append(vols, it.Src)
			}
		}
		if err := checkTrash(vols); err != nil {
			return nil, err
		}
	}
	seen := map[string]bool{}
	for _, it := range o.Items {
		reason := ""
		switch {
		case !filepath.IsAbs(it.Src) || !filepath.IsAbs(it.Keeper):
			reason = "not an absolute path"
		case seen[fold(filepath.Clean(it.Src))]:
			reason = "listed twice"
		case within(it.Src, res.Output):
			reason = "inside the output"
		case !within(it.Keeper, res.Output):
			reason = "its copy is not inside the output"
		case fold(filepath.Clean(it.Src)) == fold(filepath.Clean(it.Keeper)):
			reason = "same file as its copy"
		default:
			reason = "outside the input folders"
			for _, r := range res.Rooms {
				if within(it.Src, r) && fold(filepath.Clean(it.Src)) != fold(r) {
					reason = ""
				}
			}
		}
		seen[fold(filepath.Clean(it.Src))] = true
		if reason != "" {
			res.Refused = append(res.Refused, Refusal{Src: it.Src, Reason: reason})
			continue
		}
		res.Candidates = append(res.Candidates, it)
		res.Bytes += it.Size
	}
	sort.Slice(res.Candidates, func(i, j int) bool { return res.Candidates[i].Src < res.Candidates[j].Src })
	return res, nil
}

// Run plans and, with Execute, deletes.
func Run(o Options) (*Result, error) {
	res, err := Plan(o)
	if err != nil || !o.Execute {
		return res, err
	}
	if o.Expect >= 0 && o.Expect != len(res.Candidates) {
		return res, fmt.Errorf("confirmation mismatch: %d files to delete, %d confirmed", len(res.Candidates), o.Expect)
	}
	if o.BeforeExecute != nil {
		if err := o.BeforeExecute(res); err != nil {
			return res, err
		}
	}
	if o.Workers <= 0 {
		o.Workers = runtime.NumCPU()
	}
	j, err := openJournal(res, o.Trash)
	if err != nil {
		return res, err
	}
	defer j.close()
	logf(o, "purge: %d files (%s), journal %s\n", len(res.Candidates), report.HumanBytes(res.Bytes), res.Journal)
	deleted := remove(o, res, j)
	res.Stopped = o.Gate.Stopped()
	res.DirsRemoved, res.JunkRemoved = removeEmptyDirs(o, res, deleted, j)
	j.write(entry{Op: "end", Deleted: res.Deleted, Gone: res.Gone, Failed: len(res.Failed), Dirs: res.DirsRemoved, Junk: res.JunkRemoved})
	sort.Strings(res.Failed)
	if res.Stopped {
		return res, runctl.ErrStopped
	}
	return res, nil
}

func logf(o Options, format string, a ...any) {
	if o.Log != nil {
		fmt.Fprintf(o.Log, format, a...)
	}
}

// remove deletes every candidate whose content is confirmed in its keeper;
// returns the paths deleted (or already gone).
func remove(o Options, res *Result, j *journal) []string {
	var rep *report.Reporter
	if o.Log != nil {
		rep = report.New(report.Options{Label: "purge", TotalBytes: res.Bytes, Heartbeat: o.Heartbeat})
	}
	var mu sync.Mutex
	var done []string
	jobs := make(chan merge.Settle)
	var wg sync.WaitGroup
	for w := 0; w < o.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for it := range jobs {
				if !o.Gate.Wait() {
					mu.Lock()
					res.Pending++
					mu.Unlock()
					continue
				}
				mu.Lock()
				jerr := j.err
				mu.Unlock()
				var gone bool
				err := fmt.Errorf("journal not writable (%v): NOT deleted", jerr)
				if jerr == nil {
					gone, err = removeOne(it, o.Trash)
				}
				mu.Lock()
				switch {
				case err != nil:
					res.Failed = append(res.Failed, it.Src+": "+err.Error())
					j.write(entry{Op: "fail", Src: it.Src, Keeper: it.Keeper, Err: err.Error()})
					if rep != nil {
						rep.Event(fmt.Sprintf("ERR %s: %v", it.Src, err))
						rep.Err()
					}
				case gone:
					res.Gone++
					done = append(done, it.Src)
				default:
					res.Deleted++
					res.DeletedBytes += it.Size
					done = append(done, it.Src)
					j.write(entry{Op: "delete", Src: it.Src, Keeper: it.Keeper, Size: it.Size, Content: it.Content})
				}
				if rep != nil {
					rep.Item(it.Size, 0)
				}
				mu.Unlock()
			}
		}()
	}
	for _, it := range res.Candidates {
		jobs <- it
	}
	close(jobs)
	wg.Wait()
	if rep != nil {
		rep.Finish()
	}
	return done
}

// removeOne re-reads the source and its keeper in full and deletes the source
// only when both hold the same content (and the recorded one). gone = the
// source no longer exists (already purged).
func removeOne(it merge.Settle, trash bool) (gone bool, err error) {
	fi, err := os.Lstat(it.Src)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !fi.Mode().IsRegular() {
		return false, errors.New("not a regular file: NOT deleted")
	}
	sh, err := sparsehash.FullFile(it.Src)
	if err != nil {
		return false, err
	}
	kh, err := sparsehash.FullFile(it.Keeper)
	if err != nil {
		return false, fmt.Errorf("copy in the output unreadable (%v): NOT deleted", err)
	}
	if sh != kh || (it.Content != "" && sh != it.Content) {
		return false, errors.New("content differs from its copy in the output: NOT deleted")
	}
	return false, del(it.Src, trash)
}

func del(p string, trash bool) error {
	if trash {
		return moveToTrash(p)
	}
	return os.Remove(p)
}

// removeEmptyDirs removes the directories emptied by the run: the parents of
// the deleted files and their ancestors below the room root, deepest first.
// A directory holding only junk (kinds.JunkNames files) counts as empty.
func removeEmptyDirs(o Options, res *Result, deleted []string, j *journal) (dirs, junk int) {
	cand := map[string]bool{}
	for _, p := range deleted {
		var room string
		for _, r := range res.Rooms {
			if within(p, r) {
				room = r
			}
		}
		if room == "" {
			continue
		}
		for d := filepath.Dir(p); within(d, room) && fold(d) != fold(room); d = filepath.Dir(d) {
			cand[d] = true
		}
	}
	list := make([]string, 0, len(cand))
	for d := range cand {
		if within(res.Output, d) || within(d, res.Output) {
			continue // the output, or a directory holding it
		}
		list = append(list, d)
	}
	sort.Slice(list, func(a, b int) bool {
		if ca, cb := strings.Count(list[a], string(filepath.Separator)), strings.Count(list[b], string(filepath.Separator)); ca != cb {
			return ca > cb
		}
		return list[a] < list[b]
	})
	for _, d := range list {
		if o.Gate.Stopped() || j.err != nil {
			break
		}
		ents, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		var droppings []string
		empty := true
		for _, e := range ents {
			if e.Type().IsRegular() && kinds.IsJunk(e.Name()) {
				droppings = append(droppings, filepath.Join(d, e.Name()))
				continue
			}
			empty = false
			break
		}
		if !empty {
			continue
		}
		ok := true
		for _, f := range droppings {
			if err := del(f, o.Trash); err != nil {
				ok = false
				break
			}
			junk++
			j.write(entry{Op: "junk", Src: f})
		}
		if !ok {
			continue
		}
		if err := removeDir(d, o.Trash); err == nil {
			dirs++
			j.write(entry{Op: "rmdir", Src: d})
		}
	}
	return dirs, junk
}

// removeDir removes an empty directory (to the Recycle Bin with trash: it
// stays restorable with its files).
func removeDir(d string, trash bool) error {
	if trash {
		return moveToTrash(d)
	}
	return os.Remove(d)
}

// ---------------------------------------------------------------- journal

type entry struct {
	Op      string   `json:"op"` // start delete fail junk rmdir end
	Time    string   `json:"t,omitempty"`
	Output  string   `json:"output,omitempty"`
	Rooms   []string `json:"rooms,omitempty"`
	Files   int      `json:"files,omitempty"`
	Trash   bool     `json:"trash,omitempty"`
	Src     string   `json:"src,omitempty"`
	Keeper  string   `json:"keeper,omitempty"`
	Size    int64    `json:"size,omitempty"`
	Content string   `json:"content,omitempty"`
	Err     string   `json:"err,omitempty"`
	Deleted int      `json:"deleted,omitempty"`
	Gone    int      `json:"gone,omitempty"`
	Failed  int      `json:"failed,omitempty"`
	Dirs    int      `json:"dirs,omitempty"`
	Junk    int      `json:"junk,omitempty"`
}

type journal struct {
	f   *os.File
	err error
}

func openJournal(res *Result, trash bool) (*journal, error) {
	dir := filepath.Join(res.Output, merge.CacheDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	res.Journal = filepath.Join(dir, "purge-"+time.Now().Format("20060102-150405.000000000")+".jsonl")
	f, err := os.OpenFile(res.Journal, os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	j := &journal{f: f}
	j.write(entry{Op: "start", Output: res.Output, Rooms: res.Rooms, Files: len(res.Candidates), Trash: trash})
	if j.err != nil {
		f.Close()
		return nil, j.err
	}
	return j, nil
}

// write appends one fsync'ed line (callers serialize).
func (j *journal) write(e entry) {
	if j.err != nil {
		return
	}
	e.Time = time.Now().Format(time.RFC3339)
	b, _ := json.Marshal(e)
	if _, err := j.f.Write(append(b, '\n')); err != nil {
		j.err = err
		return
	}
	j.err = j.f.Sync()
}

func (j *journal) close() { j.f.Close() }

// ReadReport loads the settled list and rooms of a merge report (-report).
func ReadReport(path string) (Options, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Options{}, err
	}
	var r merge.Result
	if err := json.Unmarshal(b, &r); err != nil {
		return Options{}, fmt.Errorf("merge report %s: %w", path, err)
	}
	if len(r.Rooms) == 0 || r.Output == "" {
		return Options{}, fmt.Errorf("merge report %s: no output and rooms", path)
	}
	return Options{Items: r.Settled, Output: r.Output, Rooms: r.Rooms[1:]}, nil
}

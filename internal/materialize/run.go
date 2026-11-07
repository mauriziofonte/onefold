// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package materialize

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mauriziofonte/onefold/internal/fsx"
	"github.com/mauriziofonte/onefold/internal/plan"
	"github.com/mauriziofonte/onefold/internal/runctl"
	"github.com/mauriziofonte/onefold/internal/tools"
)

// Progress receives per-action progress (report.Reporter implements it).
type Progress interface {
	Item(bytes int64, dur time.Duration)
	Advance(bytes int64)
	Err()
	Event(msg string)
}

// RunOptions tune ApplyAll.
type RunOptions struct {
	Workers  int          // photo + generic workers (0 = NumCPU); videos are always serial
	Gate     *runctl.Gate // pause/stop between actions (GUI); nil = never
	Progress Progress     // optional
	Silver   Silver       // tags embedded into media copies (zero = defaults)
	// Link publishes hard links of the sources instead of copies (LinkOriginal):
	// byte-original, no silvering, same volume only.
	Link bool
	// Clone starts every byte copy as a clone of its source where the file
	// system allows it (fsx.Clone: same volume on APFS, Btrfs, XFS...); a
	// photo is then stamped on its clone, which leaves the source as it is.
	Clone bool
}

// RunStats is the outcome of ApplyAll.
type RunStats struct {
	Written    int      `json:"written"`
	Skipped    int      `json:"skipped"` // already materialized
	Failed     int      `json:"failed"`
	Pending    int      `json:"pending"`    // not started: gate stopped; a new run completes them
	Unsilvered int      `json:"unsilvered"` // media written as faithful copies without canonical metadata (also in Written)
	Cloned     int      `json:"cloned,omitempty"`
	Errors     []string `json:"errors,omitempty"`
	Stopped    bool     `json:"stopped,omitempty"`
}

// ApplyAll materializes every action into target. Photos and generic files
// run in a pool (each worker owns an exiftool -stay_open process when
// exiftool exists), videos serially (sequential reads on seek-slow disks), the
// two groups concurrently. Idempotent: existing destinations are skipped, so
// re-running after an interruption completes the tree.
func ApplyAll(acts []plan.Action, target string, o RunOptions) RunStats {
	var photos, videos []plan.Action
	for _, a := range acts {
		if a.Kind == "video" {
			videos = append(videos, a)
		} else {
			photos = append(photos, a)
		}
	}
	n := o.Workers
	if n <= 0 {
		n = runtime.NumCPU()
	}
	if len(photos) > 0 && n > len(photos) {
		n = len(photos)
	}
	var st RunStats
	var mu sync.Mutex
	var written, skipped, pending, unsilvered, cloned atomic.Int64
	var dirs fsx.DirSet // folders of the tree that gained a file: flushed at the end
	if o.Progress != nil && !o.Link {
		// one notice per missing tool instead of one warning per file
		if hasMedia(photos) && !tools.Has("exiftool") {
			o.Progress.Event("photos are copied without canonical metadata: " + NoExiftool)
		}
		if len(videos) > 0 && !tools.Has("ffmpeg") {
			o.Progress.Event("videos are copied without canonical metadata: " + NoFfmpeg)
		}
	}
	record := func(a plan.Action, res Result, err error, t0 time.Time) {
		switch {
		case err != nil:
			mu.Lock()
			st.Failed++
			st.Errors = append(st.Errors, fmt.Sprintf("%s -> %s: %v", a.SrcPath, a.CanonRel, err))
			mu.Unlock()
			if o.Progress != nil {
				o.Progress.Err()
				o.Progress.Event(fmt.Sprintf("ERR %s -> %s: %v", a.SrcPath, a.CanonRel, err))
				o.Progress.Advance(a.SrcSize)
			}
		case res.Skipped:
			skipped.Add(1)
			if o.Progress != nil {
				o.Progress.Advance(a.SrcSize)
			}
		default:
			written.Add(1)
			dirs.Add(res.Dst)
			if res.Cloned {
				cloned.Add(1)
			}
			if res.Unsilvered != "" {
				unsilvered.Add(1)
				if o.Progress != nil && res.Unsilvered != NoExiftool && res.Unsilvered != NoFfmpeg {
					o.Progress.Event(fmt.Sprintf("%s -> %s: copied without canonical metadata: %s", a.SrcPath, a.CanonRel, res.Unsilvered))
				}
			}
			if o.Progress != nil {
				o.Progress.Item(a.SrcSize, time.Since(t0))
			}
		}
	}
	one := func(a plan.Action, batch *ExifBatch) {
		if !o.Gate.Wait() {
			pending.Add(1)
			if o.Progress != nil {
				o.Progress.Advance(a.SrcSize)
			}
			return
		}
		t0 := time.Now()
		var res Result
		var err error
		if o.Link {
			res, err = LinkOriginal(a, target)
		} else {
			res, err = applyWith(a, target, batch, o.Silver, o.Clone)
		}
		record(a, res, err, t0)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		jobs := make(chan plan.Action, n)
		var pw sync.WaitGroup
		for w := 0; w < n; w++ {
			pw.Add(1)
			go func() {
				defer pw.Done()
				var batch *ExifBatch
				if !o.Link {
					batch, _ = NewExifBatch() // nil when exiftool is missing: per-file fallback
				}
				if batch != nil {
					defer batch.Close()
				}
				for a := range jobs {
					one(a, batch)
				}
			}()
		}
		for _, a := range photos {
			jobs <- a
		}
		close(jobs)
		pw.Wait()
	}()
	go func() {
		defer wg.Done()
		for _, a := range videos {
			one(a, nil)
		}
	}()
	wg.Wait()
	// Each copy was flushed before its rename; the renames are flushed here,
	// once per folder, so a power cut cannot drop names the run reported.
	if err := dirs.SyncAll(); err != nil {
		st.Failed++
		st.Errors = append(st.Errors, "flushing the target folders to disk: "+err.Error())
	}
	st.Written, st.Skipped, st.Pending = int(written.Load()), int(skipped.Load()), int(pending.Load())
	st.Unsilvered, st.Cloned = int(unsilvered.Load()), int(cloned.Load())
	st.Stopped = o.Gate.Stopped()
	return st
}

// hasMedia reports whether any action of the photo/generic group is a photo.
func hasMedia(acts []plan.Action) bool {
	for _, a := range acts {
		if a.IsMediaAction() {
			return true
		}
	}
	return false
}

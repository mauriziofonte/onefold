// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package stats measures what a set of folders or whole volumes hold: files
// and bytes per family, category, extension, year and size, the heaviest
// folders and files, and how much of it is duplicated. It only reads: file
// names and metadata for the counts, and file contents (sampled, or whole
// with full verification) only for the files that share a size with another.
package stats

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/mauriziofonte/onefold/internal/runctl"
)

// Duplicate detection modes.
const (
	DupsOff  = "off"  // no duplicate pass
	DupsFast = "fast" // same size and same sparse digest (a sample of the content)
	DupsFull = "full" // then confirmed on the whole content
)

// Progress receives the progress of one phase (report.Reporter implements it).
type Progress interface {
	AddTotal(n int64)
	TotalKnown()
	Item(bytes int64, d time.Duration)
	Advance(bytes int64)
	Err()
}

// Options select what is measured.
type Options struct {
	Roots   []string // absolute folders or mount points
	Workers int      // 0 = NumCPU
	// OneVolume keeps each walk on the volume of its root: mounted volumes
	// below it are listed in Result.OtherVolumes and not entered (Linux,
	// macOS; Windows walks never enter mounted folders, which are reparse
	// points).
	OneVolume bool
	Dups      string // DupsOff, DupsFast (default), DupsFull
	DupMin    int64  // smallest file size checked for duplicates (default 1 byte)
	Depth     int    // folder levels kept below each root (default 6)
	Top       int    // largest files and duplicate groups kept (default 100)
	// ReadOnlineOnly lets the duplicate pass read cloud files whose content
	// is not on this computer (fsx.OnlineOnly), which downloads them; by
	// default they are counted (Result.OnlineOnly) and never read.
	ReadOnlineOnly bool

	Gate *runctl.Gate // optional pause/stop
	// ProgressFor returns the progress of a phase ("scan", "dups") and its
	// stop function; nil = no progress.
	ProgressFor func(phase string) (Progress, func())
	// OnSnapshot receives the partial result every SnapshotEvery (default 1 s)
	// while the scan runs, for a live view.
	OnSnapshot    func(*Result)
	SnapshotEvery time.Duration
}

// FormatVersion is the version of the Result JSON.
const FormatVersion = 1

// Agg counts files and their bytes.
type Agg struct {
	Files int64 `json:"files"`
	Bytes int64 `json:"bytes"`
}

func (a *Agg) add(size int64) { a.Files++; a.Bytes += size }

// Bucket is one row of a breakdown.
type Bucket struct {
	Key      string `json:"key"`
	Family   string `json:"family,omitempty"`
	Category string `json:"category,omitempty"`
	Agg
	// Wasted: bytes of the extra copies (duplicate breakdowns).
	Wasted int64 `json:"wasted,omitempty"`
}

// YearBucket is one year of last modification ("0" = implausible dates).
type YearBucket struct {
	Year int `json:"year"`
	Agg
	Families map[string]int64 `json:"families"` // bytes per family
}

// SizeBucket is one band of file sizes, [Min, Max) (Max 0 = no upper bound).
type SizeBucket struct {
	Label string `json:"label"`
	Min   int64  `json:"min"`
	Max   int64  `json:"max"`
	Agg
}

// File is one notable file.
type File struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Ext    string `json:"ext"`
	Family string `json:"family"`
	MTime  int64  `json:"mtime"` // unix seconds
}

// Node is a folder with the totals of everything below it. Children are the
// heaviest subfolders; the lighter ones are summed in More*.
type Node struct {
	Name      string           `json:"name"`
	Path      string           `json:"path"`
	Files     int64            `json:"files"`
	Bytes     int64            `json:"bytes"`
	Families  map[string]int64 `json:"families"` // bytes per family
	Children  []*Node          `json:"children,omitempty"`
	More      int              `json:"more,omitempty"` // subfolders not listed
	MoreFiles int64            `json:"moreFiles,omitempty"`
	MoreBytes int64            `json:"moreBytes,omitempty"`
	Deeper    bool             `json:"deeper,omitempty"` // has subfolders below the kept depth
}

// Volume is the volume a root lives on.
type Volume struct {
	Root   string `json:"root"`
	Mount  string `json:"mount"`
	FSType string `json:"fsType,omitempty"`
	Total  int64  `json:"total"`
	Free   int64  `json:"free"`
}

// Problem is a file or folder that could not be read.
type Problem struct {
	Path string `json:"path"`
	Err  string `json:"err"`
}

// DupGroup is a set of files with the same content.
type DupGroup struct {
	Size     int64    `json:"size"`
	Hash     string   `json:"hash"`
	Count    int      `json:"count"`
	Wasted   int64    `json:"wasted"` // (count-1) * size
	Ext      string   `json:"ext"`
	Family   string   `json:"family"`
	Verified bool     `json:"verified"` // compared on the whole content
	Paths    []string `json:"paths"`    // at most maxGroupPaths
}

// DirPair is two folders that hold copies of the same files (A == B: copies
// inside one folder).
type DirPair struct {
	A     string `json:"a"`
	B     string `json:"b"`
	Files int64  `json:"files"`
	Bytes int64  `json:"bytes"`
}

// Dups is the duplicate analysis.
type Dups struct {
	Mode       string     `json:"mode"`
	MinSize    int64      `json:"minSize"`
	Candidates int64      `json:"candidates"` // files sharing their size with another
	Groups     int64      `json:"groups"`
	Files      int64      `json:"files"`  // files in a group
	Extra      int64      `json:"extra"`  // copies beyond the first of each group
	Wasted     int64      `json:"wasted"` // bytes of those copies
	Errors     int64      `json:"errors"`
	ByFamily   []Bucket   `json:"byFamily"`
	ByExt      []Bucket   `json:"byExt"`
	Top        []DupGroup `json:"top"`
	Pairs      []DirPair  `json:"pairs"`
}

// Result is the whole measurement (also the JSON of `stats -json`).
type Result struct {
	Version   int       `json:"version"`
	Roots     []string  `json:"roots"`
	Volumes   []Volume  `json:"volumes"`
	Started   time.Time `json:"started"`
	Elapsed   float64   `json:"elapsed"` // seconds
	Phase     string    `json:"phase"`   // scan, dups, done
	Stopped   bool      `json:"stopped,omitempty"`
	Partial   bool      `json:"partial,omitempty"` // a live snapshot
	Files     int64     `json:"files"`
	Dirs      int64     `json:"dirs"`
	Bytes     int64     `json:"bytes"`
	Allocated int64     `json:"allocated,omitempty"` // space used on disk (Linux, macOS)
	Empty     int64     `json:"empty"`               // 0-byte files
	Junk      Agg       `json:"junk"`                // OS droppings (.DS_Store, Thumbs.db...)
	Symlinks  int64     `json:"symlinks"`            // not followed
	// OnlineOnly: files counted above whose content is in the cloud, not on
	// this computer (OneDrive, iCloud, Dropbox placeholders: Windows, macOS);
	// never read unless Options.ReadOnlineOnly.
	OnlineOnly Agg `json:"onlineOnly"`
	// HardLinks: extra names of a file already counted (Linux, macOS)
	HardLinks    Agg       `json:"hardLinks"`
	OtherVolumes []string  `json:"otherVolumes,omitempty"` // mount points not entered
	Errors       int64     `json:"errors"`
	Problems     []Problem `json:"problems,omitempty"`

	Families   []Bucket          `json:"families"`
	Categories []Bucket          `json:"categories"`
	Exts       []Bucket          `json:"exts"`
	Unknown    []Bucket          `json:"unknown"` // extensions not in the registry ("" = no extension)
	Years      []YearBucket      `json:"years"`
	Sizes      []SizeBucket      `json:"sizes"`
	Largest    []File            `json:"largest"`
	LargestBy  map[string][]File `json:"largestBy"` // per family
	Tree       []*Node           `json:"tree"`      // one per root
	Dups       *Dups             `json:"dups,omitempty"`
}

// Run measures the roots.
func Run(o Options) (*Result, error) {
	if len(o.Roots) == 0 {
		return nil, errors.New("stats: no folder given")
	}
	if o.Workers <= 0 {
		o.Workers = runtime.NumCPU()
	}
	if o.Dups == "" {
		o.Dups = DupsFast
	}
	switch o.Dups {
	case DupsOff, DupsFast, DupsFull:
	default:
		return nil, fmt.Errorf("stats: duplicates mode %q: use off, fast or full", o.Dups)
	}
	if o.DupMin <= 0 {
		o.DupMin = 1
	}
	if o.Depth <= 0 {
		o.Depth = 6
	}
	if o.Top <= 0 {
		o.Top = 100
	}
	if o.SnapshotEvery <= 0 {
		o.SnapshotEvery = time.Second
	}
	roots, err := cleanRoots(o.Roots)
	if err != nil {
		return nil, err
	}
	o.Roots = roots
	s := newScan(o)
	err = s.walk()
	res := s.export(false)
	if err == nil && o.Dups != DupsOff && !o.Gate.Stopped() {
		res.Phase = "dups"
		res.Dups, err = s.duplicates()
	}
	res.Phase = "done"
	res.Elapsed = time.Since(s.started).Seconds()
	if o.Gate.Stopped() || errors.Is(err, runctl.ErrStopped) {
		res.Stopped = true
		return res, runctl.ErrStopped
	}
	return res, err
}

// cleanRoots makes the roots absolute and clean, refuses missing ones and
// drops a root inside another (it would be counted twice).
func cleanRoots(in []string) ([]string, error) {
	var out []string
	for _, r := range in {
		a, err := filepath.Abs(r)
		if err != nil {
			return nil, err
		}
		a = filepath.Clean(a)
		fi, err := os.Stat(a)
		if err != nil {
			return nil, err
		}
		if !fi.IsDir() {
			return nil, fmt.Errorf("stats: %s is not a folder", a)
		}
		out = append(out, a)
	}
	var kept []string
	for i, a := range out {
		inside := false
		for j, b := range out {
			if i != j && (within(a, b) && (a != b || j < i)) {
				inside = true
				break
			}
		}
		if !inside {
			kept = append(kept, a)
		}
	}
	return kept, nil
}

// within reports whether a is b or below it.
func within(a, b string) bool {
	if a == b {
		return true
	}
	rel, err := filepath.Rel(b, a)
	return err == nil && rel != "." && rel != ".." && !filepath.IsAbs(rel) &&
		!(len(rel) >= 3 && rel[:3] == ".."+string(filepath.Separator))
}

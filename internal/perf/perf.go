// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package perf is the --perf telemetry: per-stage cumulative busy time, call
// counts and bytes, plus per-phase wall clock. Disabled = one atomic load per
// probe. Stages are free-form names ("hash", "extract:document", "spawn:7z",
// "copy", ...); phases are sequential steps ("catalog", "plan", "execute").
package perf

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

var enabled atomic.Bool

// Enable turns telemetry on (off by default).
func Enable() { enabled.Store(true) }

// On reports whether telemetry is collecting.
func On() bool { return enabled.Load() }

type stage struct {
	calls atomic.Int64
	ns    atomic.Int64
	bytes atomic.Int64
	maxNs atomic.Int64
}

var (
	mu     sync.Mutex
	stages = map[string]*stage{}
	phases []Phase
	meta   = map[string]any{}
	start  = time.Now()
)

func get(name string) *stage {
	mu.Lock()
	s, ok := stages[name]
	if !ok {
		s = &stage{}
		stages[name] = s
	}
	mu.Unlock()
	return s
}

// Span measures one call of a stage: defer perf.Span("hash")(bytes) style is
// avoided (closure alloc); use t := perf.Start(); ...; perf.End("hash", t, n).
func Start() time.Time {
	if !enabled.Load() {
		return time.Time{}
	}
	return time.Now()
}

// End records a call started at t0 with n bytes processed.
func End(name string, t0 time.Time, n int64) {
	if t0.IsZero() {
		return
	}
	d := time.Since(t0).Nanoseconds()
	s := get(name)
	s.calls.Add(1)
	s.ns.Add(d)
	s.bytes.Add(n)
	for {
		m := s.maxNs.Load()
		if d <= m || s.maxNs.CompareAndSwap(m, d) {
			break
		}
	}
}

// Slow is one outlier call kept for diagnosis.
type Slow struct {
	Stage string  `json:"stage"`
	Item  string  `json:"item"`
	Sec   float64 `json:"sec"`
}

var slows []Slow

// Outlier records item when a single call of stage exceeded 2 s (top 20 kept).
func Outlier(stage, item string, t0 time.Time) {
	if t0.IsZero() {
		return
	}
	d := time.Since(t0).Seconds()
	if d < 2 {
		return
	}
	mu.Lock()
	slows = append(slows, Slow{stage, item, d})
	sort.Slice(slows, func(i, j int) bool { return slows[i].Sec > slows[j].Sec })
	if len(slows) > 20 {
		slows = slows[:20]
	}
	mu.Unlock()
}

// Count adds to a counter-only stage (no timing).
func Count(name string, n int64) {
	if !enabled.Load() {
		return
	}
	get(name).calls.Add(n)
}

// Phase is one sequential step with its wall time and parallelism.
type Phase struct {
	Name    string  `json:"name"`
	WallSec float64 `json:"wallSec"`
	Workers int     `json:"workers"`
	Items   int64   `json:"items"`
	Bytes   int64   `json:"bytes"`
}

// PhaseDone records a finished phase.
func PhaseDone(name string, t0 time.Time, workers int, items, bytes int64) {
	if !enabled.Load() {
		return
	}
	mu.Lock()
	phases = append(phases, Phase{Name: name, WallSec: time.Since(t0).Seconds(), Workers: workers, Items: items, Bytes: bytes})
	mu.Unlock()
}

// Meta attaches a key/value to the report (workers, disk, options...).
func Meta(k string, v any) {
	if !enabled.Load() {
		return
	}
	mu.Lock()
	meta[k] = v
	mu.Unlock()
}

// StageStat is the exported view of a stage.
type StageStat struct {
	Name    string  `json:"name"`
	Calls   int64   `json:"calls"`
	BusySec float64 `json:"busySec"`
	AvgMs   float64 `json:"avgMs"`
	MaxMs   float64 `json:"maxMs"`
	Bytes   int64   `json:"bytes"`
	MBps    float64 `json:"mbPerBusySec"`
}

// Report is the full telemetry snapshot.
type Report struct {
	GOOS       string         `json:"goos"`
	NumCPU     int            `json:"numCPU"`
	GOMAXPROCS int            `json:"gomaxprocs"`
	TotalSec   float64        `json:"totalSec"`
	Meta       map[string]any `json:"meta"`
	Phases     []Phase        `json:"phases"`
	Stages     []StageStat    `json:"stages"`
	Slowest    []Slow         `json:"slowest"`
}

// Snapshot returns the current report (stages sorted by busy time).
func Snapshot() Report {
	mu.Lock()
	defer mu.Unlock()
	r := Report{GOOS: runtime.GOOS, NumCPU: runtime.NumCPU(), GOMAXPROCS: runtime.GOMAXPROCS(0),
		TotalSec: time.Since(start).Seconds(), Meta: map[string]any{}, Phases: append([]Phase(nil), phases...)}
	for k, v := range meta {
		r.Meta[k] = v
	}
	for n, s := range stages {
		st := StageStat{Name: n, Calls: s.calls.Load(), BusySec: float64(s.ns.Load()) / 1e9,
			MaxMs: float64(s.maxNs.Load()) / 1e6, Bytes: s.bytes.Load()}
		if st.Calls > 0 {
			st.AvgMs = st.BusySec * 1000 / float64(st.Calls)
		}
		if st.BusySec > 0 && st.Bytes > 0 {
			st.MBps = float64(st.Bytes) / (1 << 20) / st.BusySec
		}
		r.Stages = append(r.Stages, st)
	}
	sort.Slice(r.Stages, func(i, j int) bool { return r.Stages[i].BusySec > r.Stages[j].BusySec })
	r.Slowest = append([]Slow(nil), slows...)
	return r
}

// Print writes a human table to w.
func Print(w io.Writer) {
	r := Snapshot()
	fmt.Fprintf(w, "\n== PERF (%s, %d CPU, GOMAXPROCS %d, total %.2fs)\n", r.GOOS, r.NumCPU, r.GOMAXPROCS, r.TotalSec)
	keys := make([]string, 0, len(r.Meta))
	for k := range r.Meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(w, "  %s: %v\n", k, r.Meta[k])
	}
	fmt.Fprintf(w, "  %-12s %9s %7s %9s %12s\n", "phase", "wall s", "workers", "items", "MB")
	for _, p := range r.Phases {
		fmt.Fprintf(w, "  %-12s %9.2f %7d %9d %12.1f\n", p.Name, p.WallSec, p.Workers, p.Items, float64(p.Bytes)/(1<<20))
	}
	fmt.Fprintf(w, "  %-22s %9s %10s %9s %9s %12s %9s\n", "stage", "calls", "busy s", "avg ms", "max ms", "MB", "MB/busy-s")
	for _, s := range r.Stages {
		fmt.Fprintf(w, "  %-22s %9d %10.2f %9.3f %9.1f %12.1f %9.1f\n", trunc(s.Name, 22), s.Calls, s.BusySec, s.AvgMs, s.MaxMs,
			float64(s.Bytes)/(1<<20), s.MBps)
	}
	if len(r.Slowest) > 0 {
		fmt.Fprintf(w, "  slowest single calls (>2s):\n")
		for _, x := range r.Slowest {
			fmt.Fprintf(w, "    %8.1fs %-18s %s\n", x.Sec, x.Stage, x.Item)
		}
	}
}

// WriteJSON dumps the report to path.
func WriteJSON(path string) error {
	b, err := json.MarshalIndent(Snapshot(), "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "~"
}

// Reset clears everything (tests / repeated runs in one process).
func Reset() {
	mu.Lock()
	stages, phases, meta, start, slows = map[string]*stage{}, nil, map[string]any{}, time.Now(), nil
	mu.Unlock()
}

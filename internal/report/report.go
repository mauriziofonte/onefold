// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package report is the single progress/status output of the CLI. On a
// terminal (Linux or Windows console: mpb drives the Windows console API
// natively) it draws a live mpb bar; when output is a pipe/file/background it
// emits human-readable zerolog lines with a periodic heartbeat, so the run can
// be sent to the background and followed with `tail -f`. Results go to stdout,
// progress and status to stderr.
//
// Byte progress is measured in SOURCE bytes per completed action (not bytes
// literally written), so an ffmpeg remux weighs by the file's full size and the
// total matches `--with-stats`.
package report

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"github.com/vbauerster/mpb/v8"
	"github.com/vbauerster/mpb/v8/decor"
	"golang.org/x/term"
)

// DefaultHeartbeat is the non-TTY progress log interval.
const DefaultHeartbeat = 10 * time.Second

// Unit is what a Reporter counts.
type Unit int

const (
	Bytes Unit = iota // progress = source bytes (copies, uploads, hashing)
	Items             // progress = files (catalog, renames)
)

// Options configures a Reporter.
type Options struct {
	Label      string        // task name shown in the bar / logs
	Unit       Unit          // Bytes (default) or Items
	TotalBytes int64         // Bytes: total source bytes (enables % and ETA)
	Total      int64         // Items: total items; with Growing, the initial value
	Growing    bool          // total discovered while running (AddTotal, then TotalKnown)
	Heartbeat  time.Duration // non-TTY log interval (0 -> DefaultHeartbeat)
	Out        io.Writer     // default os.Stderr
	ForceLog   bool          // force non-TTY log mode even on a terminal
}

// Reporter accumulates progress and renders it dual-mode. Safe for concurrent use.
type Reporter struct {
	label     string
	unit      Unit
	total     atomic.Int64
	final     atomic.Bool // total no longer grows
	start     time.Time
	doneBytes atomic.Int64
	doneItems atomic.Int64
	skipped   atomic.Int64
	errs      atomic.Int64

	tty bool
	out io.Writer
	p   *mpb.Progress
	bar *mpb.Bar

	sink     func(Event)
	lastEmit atomic.Int64 // unix nano of the last progress event

	log    zerolog.Logger
	stopHB chan struct{}
	hbWG   sync.WaitGroup
	once   sync.Once
}

// active is the Reporter currently drawing, so Printf never tears its bar.
var active atomic.Pointer[Reporter]

// Event is what a sink receives instead of a bar or a log line (the GUI).
type Event struct {
	Kind    string        `json:"kind"` // start, progress, done, status, warn, error
	Label   string        `json:"label,omitempty"`
	Unit    string        `json:"unit,omitempty"` // bytes | items
	Done    int64         `json:"done"`
	Total   int64         `json:"total"`
	Final   bool          `json:"final"` // total no longer grows
	Items   int64         `json:"items"`
	Skipped int64         `json:"skipped"`
	Errors  int64         `json:"errors"`
	Elapsed time.Duration `json:"elapsed"`
	Msg     string        `json:"msg,omitempty"`
	Path    string        `json:"path,omitempty"`
}

var sink atomic.Pointer[func(Event)]

// SetSink routes every Reporter created afterwards, every Printf and every
// error to f instead of the terminal (nil restores the terminal). f must be
// safe for concurrent use and must not block.
func SetSink(f func(Event)) {
	if f == nil {
		sink.Store(nil)
		return
	}
	sink.Store(&f)
}

func currentSink() func(Event) {
	if p := sink.Load(); p != nil {
		return *p
	}
	return nil
}

// sinkThrottle is the minimum interval between two progress events.
const sinkThrottle = 150 * time.Millisecond

// New builds a Reporter, auto-detecting whether Out is a terminal.
func New(o Options) *Reporter {
	out := o.Out
	if out == nil {
		out = os.Stderr
	}
	hb := o.Heartbeat
	if hb <= 0 {
		hb = DefaultHeartbeat
	}
	r := &Reporter{label: o.Label, unit: o.Unit, start: time.Now(), out: out, tty: !o.ForceLog && isTTY(out)}
	if o.Unit == Bytes {
		r.total.Store(o.TotalBytes)
	} else {
		r.total.Store(o.Total)
	}
	r.final.Store(!o.Growing)

	if fn := currentSink(); fn != nil {
		r.sink, r.tty = fn, false
		r.emit("start", "", "", true)
		active.Store(r)
		return r
	}
	if r.tty {
		r.p = mpb.New(mpb.WithOutput(out), mpb.WithWidth(36))
		counters := decor.CountersKibiByte("% .1f/% .1f")
		speed := decor.EwmaSpeed(decor.SizeB1024(0), "% .1f", 30)
		if o.Unit == Items {
			counters = decor.CountersNoUnit("%d/%d")
			speed = decor.EwmaSpeed(nil, "%.0f/s", 30)
		}
		r.bar = r.p.New(r.total.Load(), mpb.BarStyle(),
			mpb.PrependDecorators(decor.Name(o.Label+" "), counters,
				decor.Any(func(decor.Statistics) string {
					if r.final.Load() {
						return ""
					}
					return "+" // still discovering
				})),
			mpb.AppendDecorators(
				decor.Percentage(decor.WC{W: 5}),
				decor.Name("  "), decor.OnComplete(speed, ""),
				decor.OnComplete(decor.Name("  ETA "), ""), decor.OnComplete(decor.EwmaETA(decor.ET_STYLE_GO, 30), ""),
			),
		)
		active.Store(r)
		return r
	}
	cw := zerolog.ConsoleWriter{Out: out, TimeFormat: "15:04:05", NoColor: true}
	r.log = zerolog.New(cw).With().Timestamp().Logger()
	r.log.Info().Str("task", o.Label).Str("total", r.fmtAmount(r.total.Load())).Msg("start")
	r.stopHB = make(chan struct{})
	r.hbWG.Add(1)
	go r.heartbeat(hb)
	active.Store(r)
	return r
}

// isTTY: a terminal with a usable width (a 0-column pseudo terminal would
// swallow every bar frame).
func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok || !term.IsTerminal(int(f.Fd())) {
		return false
	}
	cols, _, err := term.GetSize(int(f.Fd()))
	return err == nil && cols > 0
}

// emit sends one event to the sink; progress events are throttled unless force.
func (r *Reporter) emit(kind, msg, path string, force bool) {
	if r.sink == nil {
		return
	}
	now := time.Now()
	if kind == "progress" && !force {
		last := r.lastEmit.Load()
		if now.UnixNano()-last < int64(sinkThrottle) || !r.lastEmit.CompareAndSwap(last, now.UnixNano()) {
			return
		}
	}
	unit := "bytes"
	if r.unit == Items {
		unit = "items"
	}
	r.sink(Event{Kind: kind, Label: r.label, Unit: unit, Done: r.done(), Total: r.total.Load(), Final: r.final.Load(),
		Items: r.doneItems.Load() + r.skipped.Load(), Skipped: r.skipped.Load(), Errors: r.errs.Load(),
		Elapsed: now.Sub(r.start), Msg: msg, Path: path})
}

// amount is the progress measure of one item.
func (r *Reporter) amount(bytes int64) int64 {
	if r.unit == Items {
		return 1
	}
	return bytes
}

// AddTotal grows the total by one discovered item of the given size.
func (r *Reporter) AddTotal(bytes int64) {
	t := r.total.Add(r.amount(bytes))
	if r.tty {
		r.bar.SetTotal(t, false)
	}
	r.emit("progress", "", "", false)
}

// TotalKnown marks the end of discovery: from now on % and ETA are exact.
func (r *Reporter) TotalKnown() {
	r.final.Store(true)
	if r.tty {
		r.bar.SetTotal(r.total.Load(), false)
	}
	r.emit("progress", "", "", true)
}

// Item records one completed action of the given source size and wall duration.
// The duration feeds the EWMA speed/ETA so a slow ffmpeg remux weighs correctly.
func (r *Reporter) Item(bytes int64, dur time.Duration) {
	r.doneBytes.Add(bytes)
	r.doneItems.Add(1)
	if r.tty {
		r.bar.EwmaIncrInt64(r.amount(bytes), dur)
	}
	r.emit("progress", "", "", false)
}

// Advance moves the bar for an item that needed no work (idempotent skip,
// reuse) or failed, so the bar still reaches 100%. No throughput sample.
func (r *Reporter) Advance(bytes int64) {
	r.doneBytes.Add(bytes)
	r.skipped.Add(1)
	if r.tty {
		r.bar.IncrInt64(r.amount(bytes))
	}
	r.emit("progress", "", "", false)
}

// Err records a failed item.
func (r *Reporter) Err() { r.errs.Add(1) }

// ErrorAt records a failed item with its path. On a terminal it only counts
// (the CLI summary reports the total); a sink receives the path and the error.
func (r *Reporter) ErrorAt(path string, err error) {
	r.errs.Add(1)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	r.emit("error", msg, path, true)
}

// Event logs a one-off warning: above the bar on a terminal, as a log line
// otherwise. With a sink, a message starting with "ERR " is an error event.
func (r *Reporter) Event(msg string) {
	if r.sink != nil {
		kind := "warn"
		if strings.HasPrefix(msg, "ERR ") {
			kind, msg = "error", strings.TrimPrefix(msg, "ERR ")
		}
		r.emit(kind, msg, "", true)
		return
	}
	if r.tty {
		fmt.Fprintln(r.p, msg)
		return
	}
	r.log.Warn().Msg(msg)
}

// Printf writes a status line to stderr without tearing an active bar.
func Printf(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	if fn := currentSink(); fn != nil {
		fn(Event{Kind: "status", Msg: strings.TrimSpace(msg)})
		return
	}
	if r := active.Load(); r != nil {
		if r.tty {
			fmt.Fprint(r.p, msg)
		} else {
			fmt.Fprint(r.out, msg)
		}
		return
	}
	fmt.Fprint(os.Stderr, msg)
}

// Writer is an io.Writer over Printf (for packages that take a log writer).
var Writer io.Writer = printfWriter{}

type printfWriter struct{}

func (printfWriter) Write(b []byte) (int, error) { Printf("%s", b); return len(b), nil }

func (r *Reporter) heartbeat(every time.Duration) {
	defer r.hbWG.Done()
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-r.stopHB:
			return
		case <-t.C:
			r.logProgress()
		}
	}
}

func (r *Reporter) done() int64 {
	if r.unit == Items {
		return r.doneItems.Load() + r.skipped.Load()
	}
	return r.doneBytes.Load()
}

func (r *Reporter) logProgress() {
	done, total := r.done(), r.total.Load()
	el := time.Since(r.start).Seconds()
	var rate float64
	if el > 0 {
		rate = float64(done) / el
	}
	ev := r.log.Info().Int64("items", r.doneItems.Load()+r.skipped.Load()).Str("done", r.fmtAmount(done))
	if total > 0 {
		t := r.fmtAmount(total)
		if !r.final.Load() {
			t += "+"
		}
		ev = ev.Str("of", t)
		if r.final.Load() {
			ev = ev.Str("pct", fmt.Sprintf("%.1f%%", 100*float64(done)/float64(total)))
		}
		if rate > 0 && r.final.Load() {
			eta := time.Duration(float64(total-done)/rate) * time.Second
			ev = ev.Str("eta", eta.Round(time.Second).String())
		}
	}
	ev.Str("thr", r.fmtRate(rate)).Msg(r.label)
}

func (r *Reporter) fmtAmount(n int64) string {
	if r.unit == Items {
		return fmt.Sprintf("%d", n)
	}
	return HumanBytes(n)
}

func (r *Reporter) fmtRate(v float64) string {
	if r.unit == Items {
		return fmt.Sprintf("%.0f/s", v)
	}
	return HumanBytes(int64(v)) + "/s"
}

// Stop ends rendering (idempotent). The bar is completed at its current value
// first: failed items never leave mpb waiting for a total that cannot come.
func (r *Reporter) Stop() {
	r.once.Do(func() {
		if r.sink != nil {
			r.final.Store(true)
			r.emit("done", "", "", true)
		} else if r.tty {
			r.bar.SetTotal(-1, true)
			r.p.Wait()
		} else {
			close(r.stopHB)
			r.hbWG.Wait()
			r.logProgress()
		}
		active.CompareAndSwap(r, nil)
	})
}

// Finish stops rendering and prints the final one-line summary to stdout.
func (r *Reporter) Finish() {
	r.Stop()
	if r.sink != nil {
		return // the sink got the done event; stdout is not the user's channel
	}
	el := time.Since(r.start)
	done := r.doneBytes.Load()
	var bps int64
	if s := el.Seconds(); s > 0 {
		bps = int64(float64(done) / s)
	}
	fmt.Printf("%s done: %d items (%s) in %s, avg %s/s, skipped %d, errors %d\n",
		r.label, r.doneItems.Load(), HumanBytes(done), el.Round(time.Second),
		HumanBytes(bps), r.skipped.Load(), r.errs.Load())
}

// HumanBytes formats n in binary units (1.50 MB).
func HumanBytes(n int64) string {
	const u = 1024
	if n < u {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(u), 0
	for x := n / u; x >= u; x /= u {
		div *= u
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package schedule runs onefold commands on a timetable through the scheduler
// of the system: a systemd user timer on Linux, a launchd agent on macOS, a
// Task Scheduler task on Windows. onefold writes the native definition and
// its own record of the job in <state dir>/schedule; the native task runs
// `onefold schedule run -state DIR NAME`, which runs the command with its
// output appended to <state dir>/schedule/NAME.log and records the result.
// Only commands that never delete or replace a file can be scheduled: a
// deletion needs a person reading its preview.
package schedule

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mauriziofonte/onefold/internal/fsx"
	"github.com/mauriziofonte/onefold/internal/statedir"
	"github.com/mauriziofonte/onefold/internal/tools"
)

// How often a job runs.
const (
	Hourly = "hourly"
	Daily  = "daily"
	Weekly = "weekly"
)

// Allowed are the commands a job may run: none of them deletes or replaces a
// file (rearrange, purge-sources, dedupe and backup -restore stay out).
var Allowed = map[string]bool{
	"catalog": true, "stats": true, "backup": true, "merge": true, "apply": true, "plan": true,
	"golden-index": true, "match": true, "similar": true, "export-text": true, "push": true,
}

// Days of a weekly job, as `-day` takes them.
var Days = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

// Job is one scheduled command.
type Job struct {
	Name    string   `json:"name"`
	Every   string   `json:"every"`
	At      string   `json:"at"`            // HH:MM in local time; hourly: the minute
	Day     string   `json:"day,omitempty"` // weekly: sun ... sat
	Args    []string `json:"args"`          // the onefold command and its flags
	Dir     string   `json:"dir"`           // where it runs (relative paths of Args)
	Path    string   `json:"path,omitempty"`
	Exe     string   `json:"exe"`   // the onefold the system starts
	State   string   `json:"state"` // the state dir it was added to
	Created string   `json:"created"`
}

// Last is the outcome of the last run of a job.
type Last struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Exit  int       `json:"exit"`
	Error string    `json:"error,omitempty"`
}

var nameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// Validate reports what is wrong with j, if anything.
func (j *Job) Validate() error {
	if !nameRe.MatchString(j.Name) {
		return fmt.Errorf("name %q: 1 to 40 lowercase letters, digits and dashes", j.Name)
	}
	switch j.Every {
	case Hourly, Daily, Weekly:
	default:
		return fmt.Errorf("every %q: hourly, daily or weekly", j.Every)
	}
	if _, _, err := clock(j.At); err != nil {
		return err
	}
	if (j.Every == Weekly) != (j.Day != "") {
		return fmt.Errorf("a weekly job needs -day (and only a weekly one takes it)")
	}
	if j.Day != "" && day(j.Day) < 0 {
		return fmt.Errorf("day %q: one of %s", j.Day, strings.Join(Days, ", "))
	}
	if len(j.Args) == 0 {
		return fmt.Errorf("no command to run")
	}
	if !Allowed[j.Args[0]] {
		var ok []string
		for c := range Allowed {
			ok = append(ok, c)
		}
		sort.Strings(ok)
		return fmt.Errorf("%q cannot be scheduled: only commands that never delete or replace a file (%s)", j.Args[0], strings.Join(ok, ", "))
	}
	if j.Args[0] == "backup" {
		for _, a := range j.Args[1:] {
			if f := strings.TrimLeft(a, "-"); f == "restore" || strings.HasPrefix(f, "restore=") || f == "resume" || strings.HasPrefix(f, "resume=") {
				return fmt.Errorf("backup -restore replaces files: it cannot be scheduled")
			}
		}
	}
	if !filepath.IsAbs(j.Exe) || !filepath.IsAbs(j.State) || !filepath.IsAbs(j.Dir) {
		return fmt.Errorf("the program, state and working folders must be absolute")
	}
	return nil
}

// clock parses HH:MM.
func clock(s string) (h, m int, err error) {
	hs, ms, ok := strings.Cut(s, ":")
	h, e1 := strconv.Atoi(hs)
	m, e2 := strconv.Atoi(ms)
	if !ok || e1 != nil || e2 != nil || len(hs) != 2 || len(ms) != 2 || h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("at %q: a time as HH:MM (00:00 to 23:59)", s)
	}
	return h, m, nil
}

func day(s string) int {
	for i, d := range Days {
		if d == s {
			return i
		}
	}
	return -1
}

// When describes the timetable in words.
func (j *Job) When() string {
	switch j.Every {
	case Hourly:
		return "every hour at minute " + j.At[3:]
	case Weekly:
		return "every " + j.Day + " at " + j.At
	}
	return "every day at " + j.At
}

// Dir is <state dir>/schedule.
func Dir() (string, error) {
	d, err := statedir.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "schedule"), nil
}

func recordPath(dir, name string) string { return filepath.Join(dir, name+".json") }
func lastPath(dir, name string) string   { return filepath.Join(dir, name+".last.json") }

// LogPath is where the runs of a job write their output.
func LogPath(dir, name string) string { return filepath.Join(dir, name+".log") }

// writeNew writes data to p under a temporary name, then renames it into
// place without replacing anything there.
func writeNew(p string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	tmp := fsx.TempFor(p, false)
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := fsx.RenameNoReplace(tmp, p); err != nil {
		os.Remove(tmp)
		if errors.Is(err, fsx.ErrExists) {
			return fmt.Errorf("%s already exists: remove it first (onefold schedule remove)", p)
		}
		return err
	}
	return nil
}

// Command is one command of the system scheduler, run or to run by hand.
type Command struct {
	Args []string `json:"args"`
	Ran  bool     `json:"ran"`
	Err  string   `json:"error,omitempty"`
}

// String is the command as typed in a terminal.
func (c Command) String() string {
	q := make([]string, len(c.Args))
	for i, a := range c.Args {
		q[i] = shellQuote(a)
	}
	return strings.Join(q, " ")
}

func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"'$`\\!*?[](){};&|<>~#") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// run runs each command through the tool registry while they succeed; with
// the tool missing (or external tools disabled) the rest is left to do by
// hand.
func run(cmds [][]string) []Command {
	out := make([]Command, len(cmds))
	stop := false
	for i, c := range cmds {
		out[i].Args = c
		if stop {
			continue
		}
		if _, ok := tools.Path(c[0]); !ok {
			stop = true
			continue
		}
		b, err := tools.Command(c[0], c[1:]...).CombinedOutput()
		if err != nil {
			out[i].Err = strings.TrimSpace(err.Error() + ": " + string(b))
			stop = true
			continue
		}
		out[i].Ran = true
	}
	return out
}

// Added is what Add did.
type Added struct {
	Files    []string  `json:"files"`    // the native definition
	Commands []Command `json:"commands"` // what turns it on: run, or to run by hand
}

// Add records j and writes its native definition for this system; with
// enable it also turns it on. Nothing that exists is replaced.
func Add(j *Job, enable bool) (*Added, error) {
	if err := j.Validate(); err != nil {
		return nil, err
	}
	dir := filepath.Join(j.State, "schedule")
	if _, err := os.Stat(recordPath(dir, j.Name)); err == nil {
		return nil, fmt.Errorf("a job named %q exists: remove it first (onefold schedule remove -name %s)", j.Name, j.Name)
	}
	n, err := nativeHere(j)
	if err != nil {
		return nil, err
	}
	if err := statedir.Ensure(j.State); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, statedir.DirMode); err != nil {
		return nil, err
	}
	a := &Added{}
	for _, f := range n.Files {
		if err := writeNew(f.Path, f.Data, 0o644); err != nil {
			return a, err
		}
		a.Files = append(a.Files, f.Path)
	}
	b, _ := json.MarshalIndent(j, "", "  ")
	if err := writeNew(recordPath(dir, j.Name), append(b, '\n'), statedir.FileMode); err != nil {
		return a, err
	}
	if enable {
		a.Commands = run(n.On)
	} else {
		for _, c := range n.On {
			a.Commands = append(a.Commands, Command{Args: c})
		}
	}
	return a, nil
}

// Status is a job with its last run.
type Status struct {
	Job
	Last *Last `json:"last,omitempty"`
}

// List reads every job of the state dir.
func List() ([]Status, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Status
	for _, e := range ents {
		name, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || strings.HasSuffix(name, ".last") || fsx.IsTemp(e.Name()) {
			continue
		}
		j, err := Load(dir, name)
		if err != nil {
			return out, err
		}
		s := Status{Job: *j}
		if b, err := os.ReadFile(lastPath(dir, name)); err == nil {
			var l Last
			if json.Unmarshal(b, &l) == nil {
				s.Last = &l
			}
		}
		out = append(out, s)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out, nil
}

// Load reads the record of a job.
func Load(dir, name string) (*Job, error) {
	if !nameRe.MatchString(name) {
		return nil, fmt.Errorf("no job named %q", name)
	}
	b, err := os.ReadFile(recordPath(dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("no job named %q in %s", name, dir)
	}
	if err != nil {
		return nil, err
	}
	var j Job
	if err := json.Unmarshal(b, &j); err != nil {
		return nil, fmt.Errorf("%s: %w", recordPath(dir, name), err)
	}
	return &j, nil
}

// Removed is what Remove did.
type Removed struct {
	Commands []Command `json:"commands"` // what turned it off: run, or to run by hand
	Files    []string  `json:"files"`    // deleted: the native definition and the record
}

// Remove turns a job off and deletes its native definition and its record
// (only files onefold wrote: a native file without onefold's first line is
// left and reported). The log of its runs stays.
func Remove(name string) (*Removed, error) {
	dir, err := Dir()
	if err != nil {
		return nil, err
	}
	j, err := Load(dir, name)
	if err != nil {
		return nil, err
	}
	n, err := nativeHere(j)
	if err != nil {
		return nil, err
	}
	r := &Removed{Commands: run(n.Off)}
	for _, f := range n.Files {
		b, err := os.ReadFile(f.Path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return r, err
		}
		if !ours(b) {
			return r, fmt.Errorf("%s was not written by onefold: left as it is", f.Path)
		}
		if err := os.Remove(f.Path); err != nil {
			return r, err
		}
		r.Files = append(r.Files, f.Path)
	}
	if len(n.AfterOff) > 0 && (len(r.Commands) == 0 || r.Commands[len(r.Commands)-1].Ran) {
		r.Commands = append(r.Commands, run(n.AfterOff)...)
	}
	for _, p := range []string{recordPath(dir, name), lastPath(dir, name)} {
		if err := os.Remove(p); err == nil {
			r.Files = append(r.Files, p)
		} else if !errors.Is(err, os.ErrNotExist) {
			return r, err
		}
	}
	return r, nil
}

// logCap: past it the log is renamed NAME.log.old (the older one dropped).
const logCap = 8 << 20

// Run runs a job now, as the system scheduler does: the onefold recorded
// with it runs the command in the recorded folder; its output and a line at
// the start and the end go to the job's log, the outcome to NAME.last.json.
// It returns the exit code of the command.
func Run(name string) (int, error) {
	dir, err := Dir()
	if err != nil {
		return 1, err
	}
	j, err := Load(dir, name)
	if err != nil {
		return 1, err
	}
	lp := LogPath(dir, name)
	if fi, err := os.Stat(lp); err == nil && fi.Size() > logCap {
		_ = os.Remove(lp + ".old")
		_ = os.Rename(lp, lp+".old")
	}
	lf, err := os.OpenFile(lp, os.O_CREATE|os.O_WRONLY|os.O_APPEND, statedir.FileMode)
	if err != nil {
		return 1, err
	}
	defer lf.Close()
	l := Last{Start: time.Now()}
	fmt.Fprintf(lf, "=== %s onefold %s\n", l.Start.Format(time.RFC3339), strings.Join(j.Args, " "))
	// the onefold recorded with the job: the one the system scheduler starts
	cmd := exec.Command(j.Exe, j.Args...)
	cmd.Dir = j.Dir
	cmd.Stdout, cmd.Stderr = lf, lf
	cmd.Env = os.Environ()
	if j.Path != "" {
		cmd.Env = append(cmd.Env, "PATH="+j.Path)
	}
	cmd.Env = append(cmd.Env, "ONEFOLD_STATE_DIR="+j.State)
	err = cmd.Run()
	l.End = time.Now()
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		l.Exit = ee.ExitCode()
	case err != nil:
		l.Exit, l.Error = 1, err.Error()
	}
	fmt.Fprintf(lf, "=== %s exit %d after %s%s\n", l.End.Format(time.RFC3339), l.Exit, l.End.Sub(l.Start).Round(time.Second), errText(l.Error))
	b, _ := json.Marshal(&l)
	tmp := fsx.TempFor(lastPath(dir, name), false)
	if werr := os.WriteFile(tmp, append(b, '\n'), statedir.FileMode); werr == nil {
		if rerr := os.Rename(tmp, lastPath(dir, name)); rerr != nil {
			os.Remove(tmp)
		}
	}
	return l.Exit, nil
}

func errText(s string) string {
	if s == "" {
		return ""
	}
	return ": " + s
}

// Tail returns the last n lines of a job's log.
func Tail(name string, n int, w io.Writer) error {
	dir, err := Dir()
	if err != nil {
		return err
	}
	b, err := os.ReadFile(LogPath(dir, name))
	if err != nil {
		return err
	}
	lines := strings.SplitAfter(string(b), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	_, err = io.WriteString(w, strings.Join(lines, ""))
	return err
}

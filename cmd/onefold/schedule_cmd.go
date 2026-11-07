// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mauriziofonte/onefold/internal/schedule"
	"github.com/mauriziofonte/onefold/internal/statedir"
)

const scheduleUsage = `usage:
  onefold schedule add -name NAME -every daily|weekly|hourly [-at HH:MM] [-day mon] [-no-enable] -- COMMAND [FLAGS...]
  onefold schedule list [-json]
  onefold schedule log -name NAME [-n LINES]
  onefold schedule remove -name NAME
  onefold schedule run [-state DIR] NAME     (what the system scheduler starts)

A job runs a onefold command on a timetable through the scheduler of the
system: a systemd user timer (Linux), a launchd agent (macOS), a Task
Scheduler task (Windows). Its output goes to <state dir>/schedule/NAME.log.
Only commands that never delete or replace a file can be scheduled: ` + "catalog, stats, backup (not -restore), merge, apply, plan, golden-index, match, similar, export-text, push." + `

  onefold schedule add -name cold -every weekly -day sun -at 03:00 -- backup -execute /golden /mnt/cold
  onefold schedule add -name audit -every daily -at 04:30 -- backup -check -tree /golden /mnt/cold
`

// cmdSchedule adds, lists and removes scheduled jobs, and runs one when the
// system scheduler starts it.
func cmdSchedule(args []string) {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, scheduleUsage)
		os.Exit(2)
	}
	switch args[0] {
	case "add":
		scheduleAdd(args[1:])
	case "list":
		scheduleList(args[1:])
	case "log":
		scheduleLog(args[1:])
	case "remove":
		scheduleRemove(args[1:])
	case "run":
		scheduleRun(args[1:])
	case "-h", "--help", "help":
		fmt.Print(scheduleUsage)
	default:
		fmt.Fprintf(os.Stderr, "unknown schedule command %q\n\n%s", args[0], scheduleUsage)
		os.Exit(2)
	}
}

func scheduleFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet("schedule "+name, flag.ExitOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, scheduleUsage); fs.PrintDefaults() }
	return fs
}

func scheduleAdd(args []string) {
	fs := scheduleFlags("add")
	name := fs.String("name", "", "the job's name: lowercase letters, digits and dashes")
	every := fs.String("every", schedule.Daily, "hourly, daily or weekly")
	at := fs.String("at", "03:00", "the time, HH:MM in local time (hourly: only the minute counts)")
	day := fs.String("day", "", "the day of a weekly job: "+strings.Join(schedule.Days, ", "))
	noEnable := fs.Bool("no-enable", false, "only write the definition: print the commands that turn it on")
	_ = fs.Parse(args)
	cmd := fs.Args()
	if len(cmd) > 0 && cmd[0] == "--" {
		cmd = cmd[1:]
	}
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		fatal(1, err)
	}
	st, err := statedir.Dir()
	if err != nil {
		fatal(1, err)
	}
	wd, err := os.Getwd()
	if err != nil {
		fatal(1, err)
	}
	j := &schedule.Job{Name: *name, Every: *every, At: *at, Day: strings.ToLower(*day), Args: cmd, Dir: wd,
		Path: os.Getenv("PATH"), Exe: exe, State: st, Created: time.Now().Format(time.RFC3339)}
	if err := j.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n\n%s", err, scheduleUsage)
		os.Exit(2)
	}
	a, err := schedule.Add(j, !*noEnable)
	if a != nil {
		for _, f := range a.Files {
			fmt.Printf("wrote %s\n", f)
		}
	}
	if err != nil {
		fatal(1, err)
	}
	fmt.Printf("job %s: onefold %s, %s; output in %s\n", j.Name, strings.Join(j.Args, " "), j.When(), schedule.LogPath(filepath.Join(st, "schedule"), j.Name))
	if !printCommands(a.Commands, "turn it on") {
		os.Exit(1)
	}
}

// printCommands reports the scheduler commands: run, failed, or to run by
// hand. It returns false when one failed.
func printCommands(cs []schedule.Command, what string) bool {
	var todo []string
	ok := true
	for _, c := range cs {
		switch {
		case c.Ran:
			fmt.Printf("ran: %s\n", c)
		case c.Err != "":
			fmt.Fprintf(os.Stderr, "failed: %s: %s\n", c, c.Err)
			ok = false
			todo = append(todo, c.String())
		default:
			todo = append(todo, c.String())
		}
	}
	if len(todo) > 0 {
		fmt.Printf("to %s, run:\n  %s\n", what, strings.Join(todo, "\n  "))
	}
	return ok
}

func scheduleList(args []string) {
	fs := scheduleFlags("list")
	asJSON := fs.Bool("json", false, "print the jobs as JSON")
	_ = fs.Parse(args)
	jobs, err := schedule.List()
	if err != nil {
		fatal(1, err)
	}
	if *asJSON {
		b, _ := json.MarshalIndent(jobs, "", "  ")
		fmt.Println(string(b))
		return
	}
	if len(jobs) == 0 {
		fmt.Println("no scheduled jobs")
		return
	}
	for _, j := range jobs {
		last := "never ran"
		if j.Last != nil {
			last = fmt.Sprintf("last %s, exit %d", j.Last.Start.Format("2006-01-02 15:04"), j.Last.Exit)
		}
		fmt.Printf("%-16s %-28s %s  onefold %s\n", j.Name, j.When(), last, strings.Join(j.Args, " "))
	}
}

func scheduleLog(args []string) {
	fs := scheduleFlags("log")
	name := fs.String("name", "", "the job")
	n := fs.Int("n", 40, "lines from the end")
	_ = fs.Parse(args)
	if err := schedule.Tail(*name, *n, os.Stdout); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fatal(1, fmt.Errorf("job %q has not run yet", *name))
		}
		fatal(1, err)
	}
}

func scheduleRemove(args []string) {
	fs := scheduleFlags("remove")
	name := fs.String("name", "", "the job")
	_ = fs.Parse(args)
	r, err := schedule.Remove(*name)
	if r != nil {
		for _, f := range r.Files {
			fmt.Printf("removed %s\n", f)
		}
	}
	if err != nil {
		fatal(1, err)
	}
	if !printCommands(r.Commands, "turn it off") {
		os.Exit(1)
	}
}

func scheduleRun(args []string) {
	fs := scheduleFlags("run")
	state := fs.String("state", "", "the state dir of the job (the system scheduler passes it)")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		fmt.Fprint(os.Stderr, scheduleUsage)
		os.Exit(2)
	}
	if *state != "" {
		os.Setenv("ONEFOLD_STATE_DIR", *state)
	}
	code, err := schedule.Run(fs.Arg(0))
	if err != nil {
		fatal(1, err)
	}
	os.Exit(code)
}

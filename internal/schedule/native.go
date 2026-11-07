// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package schedule

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"
)

// marker is in every native file onefold writes: Remove deletes only those.
const marker = "written by onefold schedule"

// LaunchdPrefix starts the label of a launchd agent; TaskFolder holds the
// Task Scheduler tasks.
const (
	LaunchdPrefix = "io.github.mauriziofonte.onefold."
	TaskFolder    = `onefold\`
)

// File is one native file of a job.
type File struct {
	Path string
	Data []byte
}

// Native is what the scheduler of one system needs for a job.
type Native struct {
	Files    []File
	On       [][]string // turns the job on (after the files are written)
	Off      [][]string // turns it off (before the files are deleted)
	AfterOff [][]string // after the files are deleted
}

// Env is where the native files of a system go.
type Env struct {
	GOOS      string
	ConfigDir string // Linux: $XDG_CONFIG_HOME or ~/.config
	Home      string // macOS: the home folder
	UID       int    // macOS: the user id (launchctl gui/UID)
}

func nativeHere(j *Job) (*Native, error) {
	e := Env{GOOS: runtime.GOOS, UID: os.Getuid()}
	var err error
	switch runtime.GOOS {
	case "linux":
		e.ConfigDir, err = os.UserConfigDir()
	case "darwin":
		e.Home, err = os.UserHomeDir()
	}
	if err != nil {
		return nil, err
	}
	return NativeFor(j, e)
}

// NativeFor builds the native definition of j for the system e describes.
func NativeFor(j *Job, e Env) (*Native, error) {
	h, m, err := clock(j.At)
	if err != nil {
		return nil, err
	}
	run := []string{j.Exe, "schedule", "run", "-state", j.State, j.Name}
	switch e.GOOS {
	case "linux":
		return systemd(j, e, run, h, m), nil
	case "darwin":
		return launchd(j, e, run, h, m), nil
	case "windows":
		return taskScheduler(j, run, h, m), nil
	}
	return nil, fmt.Errorf("scheduled jobs: no system scheduler known on %s", e.GOOS)
}

// systemd: a user service and its timer. A user timer runs while the user
// is logged in, or always after `loginctl enable-linger`.
func systemd(j *Job, e Env, run []string, h, m int) *Native {
	unit := "onefold-" + j.Name
	dir := filepath.Join(e.ConfigDir, "systemd", "user")
	q := make([]string, len(run))
	for i, a := range run {
		q[i] = systemdQuote(a)
	}
	service := fmt.Sprintf(`# onefold job %s, %s: removed by onefold schedule remove -name %s
[Unit]
Description=onefold: %s

[Service]
Type=oneshot
ExecStart=%s
Nice=10
IOSchedulingClass=idle
`, j.Name, marker, j.Name, j.Name, strings.Join(q, " "))
	cal := fmt.Sprintf("*-*-* %02d:%02d:00", h, m)
	switch j.Every {
	case Hourly:
		cal = fmt.Sprintf("*-*-* *:%02d:00", m)
	case Weekly:
		cal = strings.ToUpper(j.Day[:1]) + j.Day[1:] + " " + cal
	}
	timer := fmt.Sprintf(`# onefold job %s, %s: removed by onefold schedule remove -name %s
[Unit]
Description=onefold: %s, %s

[Timer]
OnCalendar=%s
Persistent=true

[Install]
WantedBy=timers.target
`, j.Name, marker, j.Name, j.Name, j.When(), cal)
	return &Native{
		Files:    []File{{filepath.Join(dir, unit+".service"), []byte(service)}, {filepath.Join(dir, unit+".timer"), []byte(timer)}},
		On:       [][]string{{"systemctl", "--user", "daemon-reload"}, {"systemctl", "--user", "enable", "--now", unit + ".timer"}},
		Off:      [][]string{{"systemctl", "--user", "disable", "--now", unit + ".timer"}},
		AfterOff: [][]string{{"systemctl", "--user", "daemon-reload"}},
	}
}

// systemdQuote quotes one word of ExecStart: specifiers (%) and variables
// ($) are escaped so a path is taken as written.
func systemdQuote(s string) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "%", "%%"), "$", "$$")
	if s != "" && !strings.ContainsAny(s, " \t\"'\\;") {
		return s
	}
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

// launchd: an agent of the user. A run missed while the Mac slept starts at
// the wake.
func launchd(j *Job, e Env, run []string, h, m int) *Native {
	label := LaunchdPrefix + j.Name
	p := filepath.Join(e.Home, "Library", "LaunchAgents", label+".plist")
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
`)
	fmt.Fprintf(&b, "<!-- onefold job %s, %s: removed by onefold schedule remove -name %s -->\n", j.Name, marker, j.Name)
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	fmt.Fprintf(&b, "\t<key>Label</key>\n\t<string>%s</string>\n\t<key>ProgramArguments</key>\n\t<array>\n", label)
	for _, a := range run {
		fmt.Fprintf(&b, "\t\t<string>%s</string>\n", html.EscapeString(a))
	}
	b.WriteString("\t</array>\n\t<key>StartCalendarInterval</key>\n\t<dict>\n")
	if j.Every != Hourly {
		fmt.Fprintf(&b, "\t\t<key>Hour</key>\n\t\t<integer>%d</integer>\n", h)
	}
	fmt.Fprintf(&b, "\t\t<key>Minute</key>\n\t\t<integer>%d</integer>\n", m)
	if j.Every == Weekly {
		fmt.Fprintf(&b, "\t\t<key>Weekday</key>\n\t\t<integer>%d</integer>\n", day(j.Day)) // 0 = Sunday
	}
	b.WriteString("\t</dict>\n\t<key>ProcessType</key>\n\t<string>Background</string>\n\t<key>LowPriorityIO</key>\n\t<true/>\n\t<key>Nice</key>\n\t<integer>10</integer>\n</dict>\n</plist>\n")
	return &Native{
		Files: []File{{p, []byte(b.String())}},
		On:    [][]string{{"launchctl", "bootstrap", fmt.Sprintf("gui/%d", e.UID), p}},
		Off:   [][]string{{"launchctl", "bootout", fmt.Sprintf("gui/%d/%s", e.UID, label)}},
	}
}

var taskDays = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}

// taskScheduler: a task of the user, run when the user is logged on (a
// console window shows while it works); a run missed while the computer was
// off starts when it is back (StartWhenAvailable). The XML is UTF-16, as
// schtasks reads it.
func taskScheduler(j *Job, run []string, h, m int) *Native {
	p := filepath.Join(j.State, "schedule", j.Name+".task.xml")
	start := fmt.Sprintf("2026-01-01T%02d:%02d:00", h, m)
	var sched string
	switch j.Every {
	case Hourly:
		start = fmt.Sprintf("2026-01-01T00:%02d:00", m)
		sched = "<Repetition><Interval>PT1H</Interval><Duration>P1D</Duration></Repetition><ScheduleByDay><DaysInterval>1</DaysInterval></ScheduleByDay>"
	case Weekly:
		sched = fmt.Sprintf("<ScheduleByWeek><DaysOfWeek><%s /></DaysOfWeek><WeeksInterval>1</WeeksInterval></ScheduleByWeek>", taskDays[day(j.Day)])
	default:
		sched = "<ScheduleByDay><DaysInterval>1</DaysInterval></ScheduleByDay>"
	}
	args := make([]string, len(run)-1)
	for i, a := range run[1:] {
		args[i] = winQuote(a)
	}
	x := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<!-- onefold job %s, %s: removed by onefold schedule remove -name %s -->
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>onefold: %s, %s</Description>
  </RegistrationInfo>
  <Triggers>
    <CalendarTrigger>
      <StartBoundary>%s</StartBoundary>
      %s
    </CalendarTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <StartWhenAvailable>true</StartWhenAvailable>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <Priority>7</Priority>
    <Enabled>true</Enabled>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>%s</Command>
      <Arguments>%s</Arguments>
    </Exec>
  </Actions>
</Task>
`, j.Name, marker, j.Name, html.EscapeString(j.Name), html.EscapeString(j.When()), start, sched,
		html.EscapeString(run[0]), html.EscapeString(strings.Join(args, " ")))
	task := TaskFolder + j.Name
	return &Native{
		Files: []File{{p, utf16le(strings.ReplaceAll(x, "\n", "\r\n"))}},
		On:    [][]string{{"schtasks", "/Create", "/TN", task, "/XML", p, "/F"}},
		Off:   [][]string{{"schtasks", "/Delete", "/TN", task, "/F"}},
	}
}

// winQuote quotes one argument as CommandLineToArgvW reads it back (the
// rules of syscall.EscapeArg, which exists only on Windows).
func winQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			slashes++
		case '"':
			b.WriteString(strings.Repeat(`\`, slashes+1))
			slashes = 0
		default:
			slashes = 0
		}
		b.WriteByte(c)
	}
	b.WriteString(strings.Repeat(`\`, slashes))
	b.WriteByte('"')
	return b.String()
}

func utf16le(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 2+2*len(u))
	b[0], b[1] = 0xFF, 0xFE
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[2+2*i:], c)
	}
	return b
}

// ours reports whether a native file carries onefold's marker (UTF-8 or the
// UTF-16 of a task).
func ours(b []byte) bool {
	if bytes.Contains(b, []byte(marker)) {
		return true
	}
	return bytes.Contains(b, utf16le(marker)[2:])
}

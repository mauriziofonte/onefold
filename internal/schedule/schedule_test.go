// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package schedule

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/mauriziofonte/onefold/internal/testfix"
)

func job(t *testing.T) *Job {
	t.Helper()
	d := testfix.TempDir(t)
	return &Job{Name: "cold", Every: Weekly, At: "03:05", Day: "sun", Args: []string{"backup", "-execute", "/golden", "/mnt/cold 2"},
		Dir: d, Exe: filepath.Join(d, "my apps", "onefold%1"), State: filepath.Join(d, "state")}
}

func TestValidate(t *testing.T) {
	ok := job(t)
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := []func(j *Job){
		func(j *Job) { j.Name = "Cold" },
		func(j *Job) { j.Name = "a/b" },
		func(j *Job) { j.Name = strings.Repeat("a", 41) },
		func(j *Job) { j.Every = "monthly" },
		func(j *Job) { j.At = "3:05" },
		func(j *Job) { j.At = "24:00" },
		func(j *Job) { j.Day = "" },
		func(j *Job) { j.Day = "sunday" },
		func(j *Job) { j.Every = Daily }, // a day on a daily job
		func(j *Job) { j.Args = nil },
		func(j *Job) { j.Args = []string{"rearrange", "-execute", "/x"} },
		func(j *Job) { j.Args = []string{"purge-sources", "-execute"} },
		func(j *Job) { j.Args = []string{"dedupe", "-execute"} },
		func(j *Job) { j.Args = []string{"backup", "-restore", "-execute", "-tree", "/g", "/c"} },
		func(j *Job) { j.Args = []string{"backup", "--resume=x"} },
		func(j *Job) { j.Exe = "onefold" },
	}
	for i, f := range bad {
		j := *job(t)
		f(&j)
		if j.Validate() == nil {
			t.Errorf("case %d accepted: %+v", i, j)
		}
	}
}

// The native definitions of the three systems, built on any system.
func TestNativeFor(t *testing.T) {
	j := job(t)
	n, err := NativeFor(j, Env{GOOS: "linux", ConfigDir: "/home/u/.config"})
	if err != nil {
		t.Fatal(err)
	}
	if len(n.Files) != 2 || n.Files[0].Path != filepath.Join("/home/u/.config", "systemd", "user", "onefold-cold.service") {
		t.Fatalf("files %+v", n.Files)
	}
	service, timer := string(n.Files[0].Data), string(n.Files[1].Data)
	for _, want := range []string{"ExecStart=" + systemdQuote(j.Exe) + " schedule run -state " + systemdQuote(j.State) + " cold\n", "%%1", "Type=oneshot", marker} {
		if !strings.Contains(service, want) {
			t.Errorf("service lacks %q:\n%s", want, service)
		}
	}
	if !strings.Contains(timer, "OnCalendar=Sun *-*-* 03:05:00\n") || !strings.Contains(timer, "Persistent=true") {
		t.Errorf("timer:\n%s", timer)
	}
	if got := strings.Join(n.On[1], " "); got != "systemctl --user enable --now onefold-cold.timer" {
		t.Errorf("on: %s", got)
	}
	for every, cal := range map[string]string{Daily: "*-*-* 03:05:00", Hourly: "*-*-* *:05:00"} {
		h := *j
		h.Every, h.Day = every, ""
		n, _ := NativeFor(&h, Env{GOOS: "linux", ConfigDir: "/c"})
		if !strings.Contains(string(n.Files[1].Data), "OnCalendar="+cal+"\n") {
			t.Errorf("%s: %s", every, n.Files[1].Data)
		}
	}
	if q := systemdQuote(`a "b"\c $X`); q != `"a \"b\"\\c $$X"` {
		t.Errorf("systemd quote: %s", q)
	}

	n, err = NativeFor(j, Env{GOOS: "darwin", Home: "/Users/u", UID: 501})
	if err != nil {
		t.Fatal(err)
	}
	plist := n.Files[0].Data
	if n.Files[0].Path != filepath.Join("/Users/u", "Library", "LaunchAgents", LaunchdPrefix+"cold.plist") {
		t.Fatalf("plist path %s", n.Files[0].Path)
	}
	var args []string
	d := xml.NewDecoder(bytes.NewReader(plist))
	d.Strict = true
	inArray := false
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("plist is not XML: %v\n%s", err, plist)
		}
		switch e := tok.(type) {
		case xml.StartElement:
			inArray = inArray || e.Name.Local == "array"
		case xml.EndElement:
			if e.Name.Local == "array" {
				inArray = false
			}
		case xml.CharData:
			if inArray && strings.TrimSpace(string(e)) != "" {
				args = append(args, string(e))
			}
		}
	}
	if strings.Join(args, "|") != strings.Join([]string{j.Exe, "schedule", "run", "-state", j.State, "cold"}, "|") {
		t.Errorf("plist arguments %q", args)
	}
	if !bytes.Contains(plist, []byte("<key>Weekday</key>\n\t\t<integer>0</integer>")) || !bytes.Contains(plist, []byte("<key>Hour</key>\n\t\t<integer>3</integer>")) {
		t.Errorf("plist calendar:\n%s", plist)
	}
	if got := strings.Join(n.On[0], " "); got != "launchctl bootstrap gui/501 "+n.Files[0].Path {
		t.Errorf("on: %s", got)
	}

	n, err = NativeFor(j, Env{GOOS: "windows"})
	if err != nil {
		t.Fatal(err)
	}
	raw := n.Files[0].Data
	if raw[0] != 0xFF || raw[1] != 0xFE {
		t.Fatal("task XML without a UTF-16 byte order mark")
	}
	u := make([]uint16, (len(raw)-2)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(raw[2+2*i:])
	}
	task := string(utf16.Decode(u))
	var parsed struct {
		Actions struct {
			Exec struct{ Command, Arguments string }
		}
		Triggers struct {
			CalendarTrigger struct{ StartBoundary string }
		}
	}
	// encoding/xml reads UTF-8 only: the declaration says UTF-16
	if err := xml.Unmarshal([]byte(strings.Replace(task, `encoding="UTF-16"`, `encoding="UTF-8"`, 1)), &parsed); err != nil {
		t.Fatalf("task XML: %v\n%s", err, task)
	}
	if parsed.Actions.Exec.Command != j.Exe || parsed.Triggers.CalendarTrigger.StartBoundary != "2026-01-01T03:05:00" {
		t.Errorf("task: %+v", parsed)
	}
	if got := splitWin(parsed.Actions.Exec.Arguments); strings.Join(got, "|") != strings.Join([]string{"schedule", "run", "-state", j.State, "cold"}, "|") {
		t.Errorf("task arguments %q -> %q", parsed.Actions.Exec.Arguments, got)
	}
	if !strings.Contains(task, "<Sunday />") || !strings.Contains(task, "\r\n") || !ours(raw) {
		t.Errorf("task:\n%s", task)
	}
	if _, err := NativeFor(j, Env{GOOS: "plan9"}); err == nil {
		t.Error("plan9 accepted")
	}
}

// splitWin is CommandLineToArgvW for the arguments (no program name).
func splitWin(s string) []string {
	var out []string
	var cur strings.Builder
	in, have := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\\':
			n := 0
			for i < len(s) && s[i] == '\\' {
				n++
				i++
			}
			if i < len(s) && s[i] == '"' {
				cur.WriteString(strings.Repeat(`\`, n/2))
				if n%2 == 1 {
					cur.WriteByte('"')
				} else {
					in = !in
				}
			} else {
				cur.WriteString(strings.Repeat(`\`, n))
				i--
			}
			have = true
		case c == '"':
			in, have = !in, true
		case (c == ' ' || c == '\t') && !in:
			if have {
				out = append(out, cur.String())
				cur.Reset()
				have = false
			}
		default:
			cur.WriteByte(c)
			have = true
		}
	}
	if have {
		out = append(out, cur.String())
	}
	return out
}

func TestWinQuote(t *testing.T) {
	for _, s := range []string{`plain`, `C:\Program Files\onefold`, `a "quoted" word`, `trailing\`, `C:\dir with space\`, `x\"y`, ``} {
		if got := splitWin(winQuote(s)); len(got) != 1 || got[0] != s {
			t.Errorf("%q -> %s -> %q", s, winQuote(s), got)
		}
	}
}

// TestHelperProcess is the command a test job runs (not a test on its own).
func TestHelperProcess(t *testing.T) {
	if os.Getenv("ONEFOLD_SCHEDULE_HELPER") != "1" {
		t.Skip("helper process")
	}
	wd, _ := os.Getwd()
	os.Stdout.WriteString("helper ran in " + wd + " with state " + os.Getenv("ONEFOLD_STATE_DIR") + "\n")
	os.Exit(3)
}

// Add on this system (with external tools disabled: nothing is turned on,
// the commands are listed), List, Run and Remove.
func TestAddListRunRemove(t *testing.T) {
	root := testfix.TempDir(t)
	state := filepath.Join(root, "state")
	t.Setenv("ONEFOLD_STATE_DIR", state)
	t.Setenv("ONEFOLD_NO_EXTERNAL", "1")
	t.Setenv("HOME", root)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	j := &Job{Name: "audit", Every: Daily, At: "04:30", Args: []string{"stats", "/x"}, Dir: root, Exe: exe, State: state}
	a, err := Add(j, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Files) == 0 || len(a.Commands) == 0 || a.Commands[0].Ran {
		t.Fatalf("added %+v", a)
	}
	for _, f := range a.Files {
		if _, err := os.Stat(f); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Add(j, false); err == nil {
		t.Fatal("a second job with the same name was accepted")
	}
	// the run: the recorded program with the recorded arguments, in the recorded folder
	dir := filepath.Join(state, "schedule")
	rec, err := Load(dir, "audit")
	if err != nil {
		t.Fatal(err)
	}
	rec.Args = []string{"-test.run=^TestHelperProcess$"}
	b, _ := json.MarshalIndent(rec, "", "  ")
	if err := os.WriteFile(recordPath(dir, "audit"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ONEFOLD_SCHEDULE_HELPER", "1")
	code, err := Run("audit")
	if err != nil || code != 3 {
		t.Fatalf("run: exit %d, %v", code, err)
	}
	logb, _ := os.ReadFile(LogPath(dir, "audit"))
	if !strings.Contains(string(logb), "helper ran in "+root+" with state "+state) || !strings.Contains(string(logb), "exit 3 after") {
		t.Fatalf("log:\n%s", logb)
	}
	jobs, err := List()
	if err != nil || len(jobs) != 1 || jobs[0].Last == nil || jobs[0].Last.Exit != 3 {
		t.Fatalf("list: %+v %v", jobs, err)
	}
	r, err := Remove("audit")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range a.Files {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("%s still there", f)
		}
	}
	if len(r.Commands) == 0 || r.Commands[0].Ran {
		t.Errorf("remove commands %+v", r.Commands)
	}
	if jobs, _ := List(); len(jobs) != 0 {
		t.Errorf("listed after remove: %+v", jobs)
	}
	if _, err := os.Stat(LogPath(dir, "audit")); err != nil {
		t.Errorf("the log goes with the job: %v", err)
	}
	if runtime.GOOS == "windows" || runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		// a native file someone else wrote under the job's name stays
		if _, err := Add(j, false); err != nil {
			t.Fatal(err)
		}
		n, _ := nativeHere(j)
		if err := os.WriteFile(n.Files[0].Path, []byte("mine"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Remove("audit"); err == nil {
			t.Error("removed a file onefold did not write")
		}
		if b, _ := os.ReadFile(n.Files[0].Path); string(b) != "mine" {
			t.Error("the foreign file changed")
		}
	}
}

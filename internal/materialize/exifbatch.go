// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package materialize

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/mauriziofonte/onefold/internal/tools"
)

// ExifBatch wraps a persistent `exiftool -stay_open` process so many photos are
// stamped by ONE exiftool instance instead of paying the ~185ms Perl spawn per
// file (measured 4.1x faster). One ExifBatch belongs to one worker: Stamp is
// serialized by a mutex, so a batch is used by a single goroutine at a time.
type ExifBatch struct {
	cmd   *exec.Cmd
	tree  *tools.Tree // kills exiftool with its children (Windows launcher -> perl)
	stdin io.WriteCloser
	out   *bufio.Scanner
	mu    sync.Mutex
	n     int
	dead  bool
	errs  lastLine // exiftool stderr: the reason of a refused file
}

var errNoExiftool = errors.New("exiftool not available")

// NewExifBatch starts an exiftool stay_open process reading argument-files from
// stdin. Returns an error if exiftool is not available (caller falls back to
// per-file stamping).
func NewExifBatch() (*ExifBatch, error) {
	cmd := tools.Command("exiftool", "-stay_open", "True", "-@", "-")
	if cmd == nil {
		return nil, errNoExiftool
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	b := &ExifBatch{cmd: cmd}
	cmd.Stderr = &b.errs
	cmd.WaitDelay = 10 * time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if b.tree, err = tools.StartTree(cmd); err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	b.stdin, b.out = stdin, sc
	return b, nil
}

// StampError is a file exiftool refused (corrupt or unwritable format): the
// batch process is fine, a retry would fail the same way.
type StampError struct{ Msg string }

func (e *StampError) Error() string { return e.Msg }

// Stamp sends one argument set (one arg per line, terminated by -execute) and
// waits for the matching {readyN} sentinel. args must already include the target
// file path as the last element. exiftool keeps running past a per-file
// warning, so a stamped-with-warnings file returns nil; a file exiftool reports
// as not updated returns a *StampError; a dead or hung process (killed after
// toolTimeout) is any other error, and the batch is unusable from then on.
func (e *ExifBatch) Stamp(args []string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.dead {
		return errBatchDead
	}
	for _, a := range args {
		if strings.ContainsAny(a, "\r\n") {
			return errors.New("argument with a line break: not expressible in an argument file")
		}
	}
	e.n++
	tag := fmt.Sprintf("{ready%d}", e.n)
	var b strings.Builder
	for _, a := range args {
		b.WriteString(a)
		b.WriteByte('\n')
	}
	// -executeN makes exiftool print {readyN}, so we resync deterministically.
	fmt.Fprintf(&b, "-execute%d\n", e.n)
	e.errs.reset()
	if _, err := io.WriteString(e.stdin, b.String()); err != nil {
		e.dead = true
		return err
	}
	hang := time.AfterFunc(toolTimeout, e.tree.Kill)
	defer hang.Stop()
	refused := false
	for e.out.Scan() {
		line := strings.TrimSpace(e.out.Text())
		if line == tag {
			if refused {
				return &StampError{Msg: e.errs.wait(200 * time.Millisecond)}
			}
			return nil
		}
		if m := notUpdated.FindStringSubmatch(line); m != nil && m[1] != "0" {
			refused = true
		}
	}
	e.dead = true
	if err := e.out.Err(); err != nil {
		return err
	}
	return io.ErrUnexpectedEOF // process died (or was killed as hung)
}

var (
	errBatchDead = errors.New("exiftool batch process is gone")
	notUpdated   = regexp.MustCompile(`^(\d+) files? weren't updated due to errors`)
)

// lastLine keeps the last non-empty line written to it (exiftool's stderr).
type lastLine struct {
	mu sync.Mutex
	l  string
}

func (w *lastLine) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, l := range strings.Split(string(p), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			w.l = l
		}
	}
	return len(p), nil
}

func (w *lastLine) reset() { w.mu.Lock(); w.l = ""; w.mu.Unlock() }

func (w *lastLine) last() string { w.mu.Lock(); defer w.mu.Unlock(); return w.l }

// wait returns the last stderr line, giving the stderr copier up to d to
// deliver it (stdout and stderr are separate pipes: the summary on stdout can
// arrive first).
func (w *lastLine) wait(d time.Duration) string {
	for end := time.Now().Add(d); ; time.Sleep(10 * time.Millisecond) {
		if l := w.last(); l != "" {
			return l
		}
		if time.Now().After(end) {
			return "file not updated"
		}
	}
}

// Close tells exiftool to exit and reaps the process.
func (e *ExifBatch) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	io.WriteString(e.stdin, "-stay_open\nFalse\n")
	e.stdin.Close()
	err := e.cmd.Wait()
	e.tree.Close()
	return err
}

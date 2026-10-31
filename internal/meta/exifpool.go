// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mauriziofonte/onefold/internal/perf"
	"github.com/mauriziofonte/onefold/internal/tools"
)

// exiftool readers kept running (`-stay_open`) for the catalog: the generic
// kinds that need exiftool (legacy office, AVI, images and audio without a
// native reader) pay its Perl start once per reader instead of once per
// file. A reader that fails or hangs is killed and that file is read by a
// one-shot exiftool: never a lost record.

// exifReadTimeout bounds one file in a reader (exiftool reads headers only,
// -fast2).
const exifReadTimeout = 2 * time.Minute

type exifProc struct {
	cmd   *exec.Cmd
	tree  *tools.Tree
	stdin io.WriteCloser
	out   *bufio.Scanner
	n     int
}

type exifPool struct {
	mu    sync.Mutex
	zone  string // TZ the readers run with: a new zone starts new readers
	free  chan *exifProc
	slots chan struct{} // one per reader that may run
	off   bool          // exiftool missing: one-shot path (which reports it)
	gen   int           // bumped by every reset: readers of an older one retire
}

var readers = &exifPool{}

// useReaders: tests turn the readers off to compare with the one-shot path.
var useReaders = true

// reset retires the idle readers (busy ones retire when they come back:
// their gen is old) and prepares a pool for zone. Called with p.mu held.
func (p *exifPool) reset(zone string) {
	p.drain()
	p.gen++
	n := min(4, max(1, runtime.NumCPU()))
	p.zone, p.off = zone, false
	p.free = make(chan *exifProc, n)
	p.slots = make(chan struct{}, n)
	for i := 0; i < n; i++ {
		p.slots <- struct{}{}
	}
}

// get returns a running reader for the current zone, nil when none can run.
// drain stops the idle readers. Called with p.mu held.
func (p *exifPool) drain() {
	for p.free != nil {
		select {
		case pr := <-p.free:
			pr.close()
		default:
			return
		}
	}
}

// get returns a running reader for the current zone and the generation of
// the pool it belongs to; nil when none can run.
func (p *exifPool) get() (*exifProc, int) {
	zone := Zone().String()
	p.mu.Lock()
	if p.free == nil || p.zone != zone {
		p.reset(zone)
	}
	free, slots, off, gen := p.free, p.slots, p.off, p.gen
	p.mu.Unlock()
	if off {
		return nil, 0
	}
	select {
	case pr := <-free:
		return pr, gen
	default:
	}
	select {
	case pr := <-free:
		return pr, gen
	case <-slots:
		pr, err := startExif(zone)
		if err != nil {
			slots <- struct{}{}
			if errors.Is(err, errNoExif) {
				p.mu.Lock()
				p.off = true
				p.mu.Unlock()
			}
			return nil, 0
		}
		return pr, gen
	}
}

// put gives a reader back to its pool, or retires it when the pool was reset
// since (or when it failed: then its slot may start a new one).
func (p *exifPool) put(pr *exifProc, gen int, failed bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	current := gen == p.gen && p.free != nil
	if failed || !current {
		pr.close()
		if current {
			p.slots <- struct{}{}
		}
		return
	}
	p.free <- pr // capacity = slots: never blocks
}

var errNoExif = errors.New("exiftool not available")

func startExif(zone string) (*exifProc, error) {
	cmd := tools.Command("exiftool", "-stay_open", "True", "-@", "-")
	if cmd == nil {
		return nil, errNoExif
	}
	cmd.Env = append(os.Environ(), "TZ="+zone)
	cmd.WaitDelay = 10 * time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	tree, err := tools.StartTree(cmd)
	if err != nil {
		return nil, err
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	return &exifProc{cmd: cmd, tree: tree, stdin: stdin, out: sc}, nil
}

// read runs one request: the args, then -executeN; the reply ends at {readyN}.
func (pr *exifProc) read(args []string) ([]byte, error) {
	for _, a := range args {
		if strings.ContainsAny(a, "\r\n") {
			return nil, errors.New("argument with a line break")
		}
	}
	pr.n++
	tag := fmt.Sprintf("{ready%d}", pr.n)
	var b strings.Builder
	for _, a := range args {
		b.WriteString(a)
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "-execute%d\n", pr.n)
	if _, err := io.WriteString(pr.stdin, b.String()); err != nil {
		return nil, err
	}
	hang := time.AfterFunc(exifReadTimeout, pr.tree.Kill)
	defer hang.Stop()
	var out []byte
	for pr.out.Scan() {
		line := pr.out.Bytes()
		if strings.TrimSpace(string(line)) == tag {
			return out, nil
		}
		out = append(append(out, line...), '\n')
	}
	if err := pr.out.Err(); err != nil {
		return nil, err
	}
	return nil, io.ErrUnexpectedEOF
}

func (pr *exifProc) close() {
	io.WriteString(pr.stdin, "-stay_open\nFalse\n")
	pr.stdin.Close()
	done := make(chan struct{})
	go func() { pr.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		pr.tree.Kill()
	}
	pr.tree.Close()
}

// exifOutput is tools.Output for exiftool through a reader; ok=false when no
// reader could run (the caller uses the one-shot path).
func exifOutput(args []string) (out []byte, ok bool) {
	if !useReaders {
		return nil, false
	}
	pr, gen := readers.get()
	if pr == nil {
		return nil, false
	}
	t0 := perf.Start()
	out, err := pr.read(args)
	perf.End("exiftool:reader", t0, 0)
	if err != nil {
		pr.tree.Kill()
	}
	readers.put(pr, gen, err != nil)
	return out, err == nil
}

// CloseExifReaders stops the running readers (they also exit when the
// program ends: their stdin closes).
func CloseExifReaders() {
	readers.mu.Lock()
	defer readers.mu.Unlock()
	readers.drain()
	readers.gen++
	readers.free = nil
}

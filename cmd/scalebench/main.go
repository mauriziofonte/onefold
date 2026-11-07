// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command scalebench measures onefold on a synthetic tree of millions of
// files: it writes the tree (deterministic, tiny files, a tenth of them
// duplicated), runs the measured commands of a prebuilt onefold binary on
// it, records each one's wall time and peak memory, and fails when a step
// exceeds its budget in bench/scale.tsv. Not part of the product; the weekly
// `scale` workflow runs it (Linux).
//
//	go run ./cmd/scalebench -files 1000000 -dir /mnt/scale -bin ./onefold -o result.json
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// exts are generic types the default scope keeps, read whole (tiny files).
var exts = []string{"txt", "csv", "json", "md", "xml", "yaml", "ini", "log"}

const perDir = 1000 // files per leaf folder

type step struct {
	Name    string   `json:"name"`
	Args    []string `json:"args"`
	Seconds float64  `json:"seconds"`
	RSSMiB  float64  `json:"rssMiB"`
	MaxSec  float64  `json:"maxSeconds,omitempty"`
	MaxRSS  float64  `json:"maxRssMiB,omitempty"`
	Over    string   `json:"over,omitempty"`
	Skipped bool     `json:"skipped,omitempty"` // not run at this size (its budget is "-")
}

type result struct {
	Files     int     `json:"files"`
	GenSec    float64 `json:"generateSeconds"`
	GOOS      string  `json:"goos"`
	CPUs      int     `json:"cpus"`
	Steps     []*step `json:"steps"`
	Failed    bool    `json:"failed"`
	Generated string  `json:"generated"`
}

func main() {
	files := flag.Int("files", 1_000_000, "files in the synthetic tree")
	dir := flag.String("dir", "", "empty folder for the tree and the outputs (a file system with enough inodes)")
	bin := flag.String("bin", "", "onefold binary to measure")
	budgets := flag.String("budgets", "bench/scale.tsv", "time and memory budgets per step and size")
	out := flag.String("o", "", "write the result as JSON here")
	reuse := flag.Bool("reuse", false, "the tree already exists in -dir (written by an earlier run)")
	workDir := flag.String("work", "", "folder for the outputs of the steps (manifests, reports; default: -dir); they are large")
	flag.Parse()
	if *dir == "" || *bin == "" {
		flag.Usage()
		os.Exit(2)
	}
	tree := filepath.Join(*dir, "tree")
	res := &result{Files: *files, GOOS: runtime.GOOS, CPUs: runtime.NumCPU(), Generated: time.Now().UTC().Format(time.RFC3339)}
	if !*reuse {
		t0 := time.Now()
		if err := generate(tree, *files); err != nil {
			fatal(err)
		}
		res.GenSec = time.Since(t0).Seconds()
		fmt.Printf("generated %d files in %.1fs\n", *files, res.GenSec)
	}
	if *workDir == "" {
		*workDir = *dir
	}
	state := filepath.Join(*workDir, "state")
	work := filepath.Join(*workDir, "work")
	for _, d := range []string{state, work} {
		os.RemoveAll(d)
		if err := os.MkdirAll(d, 0o755); err != nil {
			fatal(err)
		}
	}
	res.Steps = []*step{
		{Name: "stats", Args: []string{"stats", "-dups", "fast", "-json", filepath.Join(work, "stats.json"), tree}},
		{Name: "stats-full", Args: []string{"stats", "-dups", "full", "-json", filepath.Join(work, "stats-full.json"), tree}},
		{Name: "catalog", Args: []string{"catalog", "-preset", "all", "-allow-mixed", "-o", filepath.Join(work, "m.jsonl"), tree}},
		{Name: "merge-preview", Args: []string{"merge", "-input", tree, "-output", filepath.Join(work, "out"), "-report", filepath.Join(work, "merge.json")}},
		{Name: "dedupe-preview", Args: []string{"dedupe", "-action", "link", "-json", filepath.Join(work, "dedupe.json"), tree}},
	}
	b, err := loadBudgets(*budgets, *files)
	if err != nil {
		fatal(err)
	}
	for _, s := range res.Steps {
		if lim, ok := b[s.Name]; ok {
			s.MaxSec, s.MaxRSS = lim[0], lim[1]
		}
		if s.MaxSec < 0 {
			s.Skipped = true
			fmt.Printf("%-15s skipped at this size (bench/scale.tsv)\n", s.Name)
			continue
		}
		if err := run(*bin, s, state); err != nil {
			fatal(fmt.Errorf("%s: %w", s.Name, err))
		}
		// large outputs (a manifest of millions of records) are not kept: disk
		for _, a := range s.Args {
			if fi, err := os.Stat(a); err == nil && fi.Mode().IsRegular() && fi.Size() > 8<<20 && strings.HasPrefix(a, work) {
				os.Remove(a)
			}
		}
		var over []string
		if s.MaxSec > 0 && s.Seconds > s.MaxSec {
			over = append(over, fmt.Sprintf("%.1fs > %.0fs", s.Seconds, s.MaxSec))
		}
		if s.MaxRSS > 0 && s.RSSMiB > s.MaxRSS {
			over = append(over, fmt.Sprintf("%.0f MiB > %.0f MiB", s.RSSMiB, s.MaxRSS))
		}
		s.Over = strings.Join(over, ", ")
		res.Failed = res.Failed || s.Over != ""
		mark := "ok"
		if s.Over != "" {
			mark = "OVER BUDGET: " + s.Over
		}
		fmt.Printf("%-15s %8.1fs %8.0f MiB   budget %6.0fs %6.0f MiB   %s\n", s.Name, s.Seconds, s.RSSMiB, s.MaxSec, s.MaxRSS, mark)
	}
	if *out != "" {
		j, _ := json.MarshalIndent(res, "", " ")
		if err := os.WriteFile(*out, append(j, '\n'), 0o644); err != nil {
			fatal(err)
		}
	}
	if res.Failed {
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "scalebench:", err)
	os.Exit(1)
}

// generate writes n files: tree/dAAA/dBBB/fNNNNNNNN.ext, 1000 per folder,
// one modification time, content "file N" except every tenth file, which
// repeats the content of the file before it (a duplicate pair).
func generate(root string, n int) error {
	if _, err := os.Stat(root); err == nil {
		return fmt.Errorf("%s exists: use an empty -dir, or -reuse", root)
	}
	mt := time.Date(2020, 6, 1, 12, 0, 0, 0, time.UTC)
	dirs := (n + perDir - 1) / perDir
	var wg sync.WaitGroup
	errc := make(chan error, 1)
	jobs := make(chan int)
	for w := 0; w < max(4, runtime.NumCPU()); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for d := range jobs {
				if err := writeDir(root, d, n, mt); err != nil {
					select {
					case errc <- err:
					default:
					}
				}
			}
		}()
	}
	for d := 0; d < dirs; d++ {
		jobs <- d
	}
	close(jobs)
	wg.Wait()
	select {
	case err := <-errc:
		return err
	default:
	}
	return nil
}

func writeDir(root string, d, n int, mt time.Time) error {
	dir := filepath.Join(root, fmt.Sprintf("d%03d", d/1000), fmt.Sprintf("d%03d", d%1000))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for i := d * perDir; i < min(n, (d+1)*perDir); i++ {
		body := i
		if i%10 == 9 {
			body = i - 1
		}
		p := filepath.Join(dir, fmt.Sprintf("f%08d.%s", i, exts[i%len(exts)]))
		if err := os.WriteFile(p, []byte("file "+strconv.Itoa(body)+"\n"), 0o644); err != nil {
			return err
		}
		if err := os.Chtimes(p, mt, mt); err != nil {
			return err
		}
	}
	return nil
}

// run executes one step and records its wall time and peak resident memory.
func run(bin string, s *step, state string) error {
	cmd := exec.Command(bin, s.Args...)
	cmd.Env = append(os.Environ(), "ONEFOLD_STATE_DIR="+state, "ONEFOLD_NO_HASH_CACHE=1", "TZ=UTC")
	var errb strings.Builder
	cmd.Stdout, cmd.Stderr = nil, &limitWriter{w: &errb, n: 64 << 10}
	t0 := time.Now()
	err := cmd.Run()
	s.Seconds = time.Since(t0).Seconds()
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 0 {
			return fmt.Errorf("%v\n%s", err, errb.String())
		}
	}
	s.RSSMiB = maxRSSMiB(cmd.ProcessState)
	return nil
}

type limitWriter struct {
	w *strings.Builder
	n int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.n > 0 {
		k := min(len(p), l.n)
		l.w.Write(p[:k])
		l.n -= k
	}
	return len(p), nil
}

// loadBudgets reads the rows of bench/scale.tsv for this size: step,
// files, max seconds, max MiB; "-" seconds = the step does not run at that
// size (more memory than the runner has).
func loadBudgets(p string, files int) (map[string][2]float64, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string][2]float64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		c := strings.Split(line, "\t")
		if len(c) != 4 {
			return nil, fmt.Errorf("%s: %q: want step, files, seconds, MiB", p, line)
		}
		if c[2] == "-" {
			c[2], c[3] = "-1", "0"
		}
		n, err1 := strconv.Atoi(c[1])
		sec, err2 := strconv.ParseFloat(c[2], 64)
		mib, err3 := strconv.ParseFloat(c[3], 64)
		if err := errors.Join(err1, err2, err3); err != nil {
			return nil, fmt.Errorf("%s: %q: %w", p, line, err)
		}
		if n == files {
			out[c[0]] = [2]float64{sec, mib}
		}
	}
	return out, sc.Err()
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package kinds

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The registry is the only place that knows extensions, and code, comments,
// CLI text, README, LLMS.md and test names never name the older tools some
// logic came from (fixture content excepted).
func TestChokePoint(t *testing.T) {
	root := filepath.Join("..", "..")
	legacy := regexp.MustCompile(`(?i)rearrangel|\brfeo\b|RecursiveFileExtension|ffmpeg-scripts`)
	dotted := regexp.MustCompile(`"\.([a-z0-9]{1,5})"`)
	extCmp := regexp.MustCompile(`\bext\w* *(==|!=) *"[^"]|switch (strings\.ToLower\()?ext\w*\)? *\{`)
	// Names onefold itself writes, container members and OS executables: not
	// file-type knowledge.
	exempt := map[string]map[string]bool{
		"materialize.go": {".part": true},
		"planreport.go":  {".md": true, ".part": true, ".html": true, ".htm": true},
		"backup.go":      {".jsonl": true},
		"dedupe.go":      {".jsonl": true},
		"purge.go":       {".jsonl": true},
		"rearrange.go":   {".jsonl": true},
		"restore.go":     {".jsonl": true},
		"sftp.go":        {".part": true},
		"textexport.go":  {".txt": true, ".part": true, ".xml": true, ".xhtml": true, ".html": true, ".htm": true},
		"tools.go":       {".exe": true, ".bat": true, ".cmd": true},
	}
	testName := regexp.MustCompile(`^func (Test|Benchmark)\w+`)

	for _, doc := range []string{"README.md", "LLMS.md"} {
		b, err := os.ReadFile(filepath.Join(root, doc))
		if err != nil {
			t.Fatal(err)
		}
		if m := legacy.Find(b); m != nil {
			t.Errorf("%s names a legacy tool: %q", doc, m)
		}
	}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == ".git" || n == "testdata" || n == ".claude" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		isTest := strings.HasSuffix(p, "_test.go")
		inKinds := filepath.Base(filepath.Dir(p)) == "kinds"
		for i, line := range strings.Split(string(b), "\n") {
			code, comment := line, ""
			if c := strings.Index(code, "//"); c >= 0 {
				code, comment = code[:c], code[c:]
			}
			where := p + ":" + strconv.Itoa(i+1)
			if legacy.MatchString(comment) {
				t.Errorf("%s: legacy tool name in a comment: %s", where, strings.TrimSpace(line))
			}
			if isTest {
				if m := testName.FindString(code); m != "" && legacy.MatchString(m) {
					t.Errorf("%s: test name names a legacy tool: %s", where, m)
				}
				continue
			}
			if legacy.MatchString(code) {
				t.Errorf("%s: legacy tool name in code: %s", where, strings.TrimSpace(line))
			}
			if inKinds {
				continue
			}
			for _, m := range dotted.FindAllStringSubmatch(code, -1) {
				if Known(m[1]) && !exempt[filepath.Base(p)][m[0][1:len(m[0])-1]] {
					t.Errorf("%s: extension literal %s outside the registry", where, m[0])
				}
			}
			if extCmp.MatchString(code) {
				t.Errorf("%s: extension switch/compare outside the registry (use kinds.Format/Group/Kind): %s", where, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

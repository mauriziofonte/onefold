// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package plan

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mauriziofonte/onefold/internal/catalog"
)

// Read loads a plan JSONL. Blank lines are ignored; any other line that is not
// a valid action fails the whole read: a plan applied with lines missing would
// silently leave assets out of the golden tree. The plan must carry the header
// with the canonical zone of this run: its times become file times.
//
// Every destination (canonRel, a sidecar's rel) must be a relative path
// inside the target: apply creates what it names, so a plan edited to point
// at "../x" or an absolute path is refused as a whole.
func Read(path string) ([]Action, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return read(f, path)
}

// read is Read on an open plan; path names it in the errors.
func read(r io.Reader, path string) ([]Action, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var acts []Action
	zone := ""
	bad, first := 0, 0
	for n := 1; sc.Scan(); n++ {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if h, ok := catalog.ParseHeader(line); ok {
			if h.Kind != catalog.KindPlan {
				return nil, fmt.Errorf("%s: a onefold %s, not a plan", path, h.Kind)
			}
			zone = h.Zone
			continue
		}
		var a Action
		if json.Unmarshal(line, &a) != nil {
			if bad++; bad == 1 {
				first = n
			}
			continue
		}
		if !inTarget(a.CanonRel) {
			return nil, fmt.Errorf("%s: line %d: canonRel %q is not a path inside the target", path, n, a.CanonRel)
		}
		for _, s := range a.Sidecars {
			if !inTarget(s.Rel) {
				return nil, fmt.Errorf("%s: line %d: sidecar rel %q is not a path inside the target", path, n, s.Rel)
			}
		}
		acts = append(acts, a)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if bad > 0 {
		return nil, fmt.Errorf("%s: %d malformed line(s), first at line %d: not a complete plan (write it again with plan -o)", path, bad, first)
	}
	if err := catalog.CheckZone(path, catalog.KindPlan, zone); err != nil {
		return nil, err
	}
	return acts, nil
}

// inTarget reports whether rel (slash-separated) names an entry below the
// target: not absolute, no volume, no "..", not the target itself.
func inTarget(rel string) bool {
	p := filepath.FromSlash(rel)
	return rel != "" && filepath.IsLocal(p) && filepath.Clean(p) != "."
}

// Write writes a plan: the header line (canonical zone), then one action per
// line.
func Write(w io.Writer, acts []Action) error {
	bw := bufio.NewWriterSize(w, 1<<20)
	if err := catalog.WriteHeader(bw, catalog.KindPlan); err != nil {
		return err
	}
	enc := json.NewEncoder(bw)
	for i := range acts {
		if err := enc.Encode(&acts[i]); err != nil {
			return err
		}
	}
	return bw.Flush()
}

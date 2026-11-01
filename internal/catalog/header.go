// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/mauriziofonte/onefold/internal/meta"
)

// Kinds of onefold JSONL files, named by their header line.
const (
	KindManifest = "manifest"     // catalog output, merge/rearrange caches
	KindPlan     = "plan"         // plan -o
	KindIndex    = "golden-index" // golden-index -o
)

// Header is the first line of a manifest or a plan: what the file is and the
// canonical zone its "2006-01-02 15:04:05" times are wall clocks in.
type Header struct {
	Kind string `json:"onefold"`
	Zone string `json:"zone"`
}

// WriteHeader writes the header line of a file of this kind in the canonical
// zone of the run.
func WriteHeader(w io.Writer, kind string) error {
	b, err := json.Marshal(Header{Kind: kind, Zone: meta.ZoneName()})
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

// DefaultHost is the host label of a manifest when none is given: this
// machine's name ("local" when the system has none).
func DefaultHost() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "local"
}

// ParseHeader recognizes a header line (an object with the "onefold" key).
func ParseHeader(line []byte) (Header, bool) {
	if !bytes.Contains(line, []byte(`"onefold"`)) {
		return Header{}, false
	}
	var h Header
	if json.Unmarshal(line, &h) != nil || h.Kind == "" {
		return Header{}, false
	}
	return h, true
}

// CheckZone refuses a file whose times are not in the canonical zone of this
// run: zone "" means the file has no header.
func CheckZone(path, kind, zone string) error {
	if zone == "" {
		return fmt.Errorf("%s: no onefold %s header (zone unknown): write it again with this version", path, kind)
	}
	if cur := meta.ZoneName(); zone != cur {
		return fmt.Errorf("%s: %s times are in %s, this run uses %s: set the same zone (onefold config set timezone %s) or write it again", path, kind, zone, cur, zone)
	}
	return nil
}

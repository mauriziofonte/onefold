// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package golden

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// BenchmarkLoad measures reading a golden index of 300k assets (the size
// class of a family archive) into its lookups: the evidence behind keeping
// the index as JSONL instead of a database.
//
//	go test ./internal/golden -run x -bench Load -benchmem
func BenchmarkLoad(b *testing.B) {
	es := make([]Entry, 300000)
	for i := range es {
		es[i] = Entry{ID: fmt.Sprintf("%032x", i), Kind: "photo", CanonRel: fmt.Sprintf("PICTURES/JPG/2020/IMG_%08d.jpg", i),
			Time: "2020-01-02 03:04:05", SubSecNs: int64(i + 1), Width: 4032, Height: 3024, Size: 3 << 20,
			SrcPath: fmt.Sprintf("/archive/DCIM/IMG_%08d.jpg", i), CanonDevice: "Apple iPhone 12", Hashes: []string{fmt.Sprintf("%032x|jpg", i+1<<40)}}
	}
	var buf bytes.Buffer
	if err := Write(&buf, es); err != nil {
		b.Fatal(err)
	}
	p := filepath.Join(b.TempDir(), "index.jsonl")
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(buf.Len()))
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if _, err := Load(p); err != nil {
			b.Fatal(err)
		}
	}
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package catalog

import (
	"os"
	"path/filepath"
	"testing"
)

// The manifest reader (plan, -reuse, the merge and rearrange caches) never
// panics on any bytes, and a manifest it accepts has no malformed line.
func FuzzReadManifest(f *testing.F) {
	f.Add([]byte(`{"onefold":"manifest","zone":"UTC"}` + "\n" + `{"room":"r","path":"/r/a.jpg","size":3,"hash":"h","host":"x","meta":{"kind":"photo","time":"2020-01-02 03:04:05"}}` + "\n"))
	f.Add([]byte(`{"onefold":"plan","zone":"UTC"}` + "\n"))
	f.Add([]byte(`{"path":"/r/b.t`))
	f.Fuzz(func(t *testing.T, b []byte) {
		ParseHeader(b)
		p := filepath.Join(t.TempDir(), "m.jsonl")
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		_, _, bad, first, err := readManifest(p)
		if err == nil && (bad > 0) != (first > 0) {
			t.Fatalf("bad %d first %d", bad, first)
		}
		LoadManifestByPath(p)
	})
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package sparsehash

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// writeFile creates a deterministic pseudo-random file of n bytes (seeded, so
// tests are reproducible without Math.rand nondeterminism).
func writeFile(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	x := uint32(2166136261)
	for i := range b {
		x = x*16777619 + uint32(i) // simple deterministic fill
		b[i] = byte(x >> 13)
	}
	p := filepath.Join(t.TempDir(), "f.bin")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDeterministic(t *testing.T) {
	p := writeFile(t, 4<<20) // 4 MiB → sparse path
	a, err := HashFile(p, DefaultCoverage, DefaultSampleSize)
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashFile(p, DefaultCoverage, DefaultSampleSize)
	if err != nil {
		t.Fatal(err)
	}
	if a.Hex != b.Hex {
		t.Fatalf("non-deterministic: %s != %s", a.Hex, b.Hex)
	}
	if a.Full {
		t.Fatalf("4 MiB file should use sparse path, got full")
	}
	if len(a.Hex) != 32 {
		t.Fatalf("expected 32-hex XXH128, got %d chars: %s", len(a.Hex), a.Hex)
	}
	// Sparse must read far less than the whole file.
	if a.SampledBytes >= a.Size {
		t.Fatalf("sparse read %d >= size %d (no I/O win)", a.SampledBytes, a.Size)
	}
}

func TestSmallFileFallsBackToFull(t *testing.T) {
	p := writeFile(t, 1000) // too small for 5%/4096 sampling
	r, err := HashFile(p, DefaultCoverage, DefaultSampleSize)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Full {
		t.Fatalf("tiny file should use full hash, got sparse")
	}
}

// TestSizeSensitivity: two files with identical sampled regions but different
// size must differ, because the sparse path appends the file size.
func TestSizeSensitivity(t *testing.T) {
	p := writeFile(t, 4<<20)
	longer := p + ".2"
	data, _ := os.ReadFile(p)
	// Append a zero byte far from any sampled offset is hard to guarantee, so we
	// simply make a byte-identical-prefix file one byte longer; size differs.
	if err := os.WriteFile(longer, append(data, 0), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _ := HashFile(p, DefaultCoverage, DefaultSampleSize)
	b, _ := HashFile(longer, DefaultCoverage, DefaultSampleSize)
	if a.Hex == b.Hex {
		t.Fatalf("different-size files collided: %s", a.Hex)
	}
}

// HashFileWhole / HashBytes must produce exactly HashFile's digest.
func TestWholeEqualsSeek(t *testing.T) {
	d := t.TempDir()
	sizes := []int{0, 1, 511, 4096, 100_000, 327_679, 327_680, 400_000, 3_000_000, 9_000_000}
	for i, n := range sizes {
		for _, binary := range []bool{true, false} {
			b := make([]byte, n)
			for k := range b {
				b[k] = byte('a' + (k*7+i)%26)
			}
			if binary && n > 0 {
				b[n/2%512] = 0
			}
			p := filepath.Join(d, fmt.Sprintf("f%d-%v", n, binary))
			os.WriteFile(p, b, 0o644)
			want, err := HashFile(p, 5, 4096)
			if err != nil {
				t.Fatal(err)
			}
			got, buf, err := HashFileWhole(p, 5, 4096, 1<<30)
			if err != nil || got.Hex != want.Hex || got.Full != want.Full || len(buf) != n {
				t.Fatalf("size %d binary %v: whole %+v vs seek %+v (%v)", n, binary, got, want, err)
			}
			big, nb, _ := HashFileWhole(p, 5, 4096, 10)
			if big.Hex != want.Hex || (n > 10 && nb != nil) {
				t.Fatalf("size %d: over-threshold path differs", n)
			}
		}
	}
}

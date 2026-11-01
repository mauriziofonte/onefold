// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package sparsehash is a Go port of the sparse strategy of the PHP library
// mfonte/sparsedigest: a sub-linear file fingerprint that samples a
// small percentage of the file (first two + last two blocks + evenly spaced
// middle blocks) and appends the file size, instead of hashing every byte.
//
// It exists because the golden-dump indexer must fingerprint terabytes of media
// on a slow (DRAMless QLC) disk: reading ~5% of each file instead of 100% is the
// single biggest I/O win.
//
// Digest choice: XXH128 (zeebo/xxh3), 128 bits, where the PHP library picks
// XXH3-64: 128 bits halve the birthday-collision exponent for free across
// millions of files. Parity anchor: the low 64 bits of our XXH128 match the
// library's XXH3-64 exactly (verified), proving the sampled byte stream and
// size-append are identical.
//
// Semantics mirror the library's sparse strategy:
//   - coverage is a PERCENTAGE (default 5), sample is a block size (4096)
//   - if fewer than 4 blocks fit in the covered budget, OR the file is not
//     "likely binary", it falls back to a full-file hash (no size appended)
//   - the sparse path appends the decimal file size for extra uniqueness
package sparsehash

import (
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"

	"github.com/zeebo/xxh3"

	"github.com/mauriziofonte/onefold/internal/perf"
)

// Defaults: 5% coverage, 4096-byte blocks.
const (
	DefaultCoverage   = 5    // percent of the file to sample (a ceiling, capped by MaxBlocks)
	DefaultSampleSize = 4096 // bytes per sampled block
	sniffSize         = 512  // bytes read to decide binary vs text
)

// MaxBlocks caps the number of sampled windows REGARDLESS of file size. This is
// the critical divergence from pure-percentage sampling: at 5% a
// 90 MB video would sample ~1100 scattered 4 KB blocks (~1100 seeks), a 4 GB
// GoPro ~50000 — catastrophic on a seek-slow (SMR/HDD) disk. Capping to a fixed
// small count makes hashing CONSTANT-TIME per file (a handful of seeks, not
// thousands). Fewer windows = fewer seeks: on a single-head SMR/HDD, dropping
// the middle windows (blocks=4 = head+tail only) roughly DOUBLES throughput on
// large video, because it avoids big head-movements across a multi-GB file
// (measured: nikon 161 MB/file, 6→13 files/s cold). head+tail + appended size
// still distinguishes real files with negligible collision risk. Default 8 is a
// safety/speed balance; set to 4 for max speed on seek-slow media.
var MaxBlocks = 8

// Result is the outcome of hashing one file.
type Result struct {
	Hex          string // 32-char hex XXH128 (canonical big-endian), matches PHP toHex()
	Full         bool   // true if the full-file fallback was used
	Size         int64  // logical file size in bytes
	SampledBytes int64  // bytes actually read from disk (the I/O we paid)
}

// HashFile fingerprints one file. coverage<=0 / sample<=0 use the defaults.
func HashFile(path string, coverage, sample int) (Result, error) {
	if coverage <= 0 {
		coverage = DefaultCoverage
	}
	if sample <= 0 {
		sample = DefaultSampleSize
	}

	to := perf.Start()
	f, err := os.Open(path)
	perf.End("open", to, 0)
	if err != nil {
		return Result{}, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return Result{}, err
	}
	size := fi.Size()

	offsets, ok := computeOffsets(size, coverage, sample)
	tb := perf.Start()
	bin := ok && likelyBinary(f)
	perf.End("read:sniff512", tb, sniffSize)
	if !bin {
		tf := perf.Start()
		r, err := fullHash(f, size)
		perf.End("read:full", tf, size)
		return r, err
	}
	ts := perf.Start()
	r, err := sparseHash(f, size, sample, offsets)
	perf.End("read:sparse", ts, r.SampledBytes)
	return r, err
}

// computeOffsets replicates the library's computeSampleOffsets. Returns ok=false
// when the file is too small for the requested coverage (caller falls back).
func computeOffsets(size int64, coverage, sample int) ([]int64, bool) {
	s := int64(sample)
	totalSampleBytes := size * int64(coverage) / 100
	totalBlocks := totalSampleBytes / s
	if totalBlocks < 4 || size < totalBlocks*s {
		return nil, false
	}
	// Cap the window count so hashing is constant-time on large media (see MaxBlocks).
	if MaxBlocks > 0 && totalBlocks > int64(MaxBlocks) {
		totalBlocks = int64(MaxBlocks)
	}

	intermediate := totalBlocks - 4
	offsets := make([]int64, 0, totalBlocks)
	// First two blocks.
	offsets = append(offsets, 0, s)
	// Intermediate blocks, evenly distributed between the first two and last two.
	start := 2 * s
	end := size - 2*s
	step := float64(end-start) / float64(intermediate+1)
	for i := int64(1); i <= intermediate; i++ {
		offsets = append(offsets, int64(math.Round(float64(start)+float64(i)*step)))
	}
	// Last two blocks.
	offsets = append(offsets, size-2*s, size-s)
	return offsets, true
}

func sparseHash(f *os.File, size int64, sample int, offsets []int64) (Result, error) {
	h := xxh3.New()
	buf := make([]byte, sample)
	var sampled int64
	for _, off := range offsets {
		n, err := f.ReadAt(buf, off)
		if err != nil && err != io.EOF {
			return Result{}, fmt.Errorf("readat %d: %w", off, err)
		}
		h.Write(buf[:n])
		sampled += int64(n)
	}
	// Append the decimal file size, exactly like PHP's hash_update((string)size).
	h.Write([]byte(strconv.FormatInt(size, 10)))
	return Result{Hex: sum128Hex(h), Full: false, Size: size, SampledBytes: sampled}, nil
}

func fullHash(f *os.File, size int64) (Result, error) {
	h := xxh3.New()
	// Large reads: on seek/IOPS-bound disks (DRAMless SATA SSD, HDD) the default
	// 32 KB io.Copy chunks cost one request each (measured 11 MB/s vs >70 MB/s).
	buf := make([]byte, fullBuf(size))
	if _, err := io.CopyBuffer(h, struct{ io.Reader }{io.NewSectionReader(f, 0, size)}, buf); err != nil {
		return Result{}, err
	}
	return Result{Hex: sum128Hex(h), Full: true, Size: size, SampledBytes: size}, nil
}

// fullBuf sizes the read buffer: whole file when small, 4 MiB chunks otherwise.
func fullBuf(size int64) int {
	const max = 4 << 20
	if size < 4096 {
		return 4096
	}
	if size < max {
		return int(size)
	}
	return max
}

func sum128Hex(h *xxh3.Hasher) string {
	u := h.Sum128()
	b := u.Bytes() // canonical big-endian (Hi||Lo), same as PHP xxh128 raw output
	return hex.EncodeToString(b[:])
}

// likelyBinary sniffs the first sniffSize bytes for a NUL byte. Media is always
// binary; text files (which a single unsampled edit could change) go to fullHash.
func likelyBinary(f *os.File) bool {
	buf := make([]byte, sniffSize)
	n, err := f.ReadAt(buf, 0)
	if err != nil && err != io.EOF {
		return true // unreadable head: treat as binary, we still only sample
	}
	for _, c := range buf[:n] {
		if c == 0 {
			return true
		}
	}
	return false
}

// FullFile hashes every byte of path (XXH128). Used by `plan --verify-full` to
// confirm that sparse-hash duplicates are truly byte-identical.
func FullFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	r, err := fullHash(f, fi.Size())
	if err != nil {
		return "", err
	}
	return r.Hex, nil
}

// HashFileWhole is HashFile with one I/O for small files: when the size is
// <= maxWhole the file is read ONCE, sequentially, and the identical digest
// (same sampled windows, same size append, same full-hash fallback) is computed
// from memory. The buffer is returned so callers can sniff/extract without
// reopening the file (on Windows each first open triggers an on-access AV scan).
// Larger files (or maxWhole <= 0) use the seek-based HashFile and return nil.
func HashFileWhole(path string, coverage, sample int, maxWhole int64) (Result, []byte, error) {
	if coverage <= 0 {
		coverage = DefaultCoverage
	}
	if sample <= 0 {
		sample = DefaultSampleSize
	}
	to := perf.Start()
	f, err := os.Open(path)
	perf.End("open", to, 0)
	if err != nil {
		return Result{}, nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return Result{}, nil, err
	}
	size := fi.Size()
	if maxWhole <= 0 || size > maxWhole {
		offsets, ok := computeOffsets(size, coverage, sample)
		if !ok || !likelyBinary(f) {
			tf := perf.Start()
			r, err := fullHash(f, size)
			perf.End("read:full", tf, size)
			return r, nil, err
		}
		ts := perf.Start()
		r, err := sparseHash(f, size, sample, offsets)
		perf.End("read:sparse", ts, r.SampledBytes)
		return r, nil, err
	}
	tw := perf.Start()
	buf := make([]byte, size)
	n, err := io.ReadFull(f, buf)
	perf.End("read:whole", tw, int64(n))
	if err != nil {
		return Result{}, nil, err
	}
	return HashBytes(buf, coverage, sample), buf, nil
}

// HashBytes computes the digest HashFile would compute for a file with exactly
// these contents.
func HashBytes(b []byte, coverage, sample int) Result {
	size := int64(len(b))
	offsets, ok := computeOffsets(size, coverage, sample)
	head := b
	if len(head) > sniffSize {
		head = head[:sniffSize]
	}
	binary := false
	for _, c := range head {
		if c == 0 {
			binary = true
			break
		}
	}
	h := xxh3.New()
	if !ok || !binary {
		h.Write(b)
		return Result{Hex: sum128Hex(h), Full: true, Size: size, SampledBytes: size}
	}
	var sampled int64
	for _, off := range offsets {
		end := off + int64(sample)
		if end > size {
			end = size
		}
		h.Write(b[off:end])
		sampled += end - off
	}
	h.Write([]byte(strconv.FormatInt(size, 10)))
	return Result{Hex: sum128Hex(h), Full: false, Size: size, SampledBytes: sampled}
}

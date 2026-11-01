// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package scan walks a directory tree in parallel and sparse-hashes every media
// file, reporting throughput. It is the Phase-0 spike: it proves the hashing
// pipeline flies on a slow disk before the indexer/planner/applier are built.
//
// Concurrency is bounded and tunable (-j). Sparse hashing already minimises I/O
// (it seek-reads ~5% of each file), so the disk stays the limit, not the CPU;
// fastwalk hashes the files of a directory on the goroutine that listed it, so
// -j beyond the directory fan-out does not help (the catalog has no such limit).
package scan

import (
	"io/fs"
	"sync/atomic"
	"time"

	"github.com/charlievieth/fastwalk"

	"github.com/mauriziofonte/onefold/internal/kinds"
	"github.com/mauriziofonte/onefold/internal/sparsehash"
)

// DefaultMediaExts is the whitelist used when no explicit extension set is
// given: the registry "visual" preset (lowercase, no dot).
var DefaultMediaExts, _, _ = kinds.PresetExts([]string{"visual"})

// Stats is the aggregate outcome of an index run. All counters are atomic during
// the walk and safe to read once Index returns.
type Stats struct {
	Files        int64
	Sparse       int64
	Full         int64
	Errors       int64
	Skipped      int64 // non-media / non-regular entries
	LogicalBytes int64 // sum of file sizes seen
	SampledBytes int64 // bytes actually read from disk
	Elapsed      time.Duration
}

// Result pairs a file with its hash, streamed to onFile as it is computed.
type Result struct {
	Path string
	sparsehash.Result
}

// Index walks root with `workers` goroutines, sparse-hashing every file whose
// extension is in exts. A nil exts means "match every file" (the -all mode);
// callers wanting the media whitelist pass DefaultMediaExts explicitly. onFile,
// if non-nil, is called concurrently for every successfully hashed file and must
// be safe for parallel use.
func Index(root string, workers, coverage, sample int, exts map[string]bool, onFile func(Result)) (Stats, error) {
	matchAll := exts == nil
	var st Stats
	start := time.Now()

	walkFn := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			atomic.AddInt64(&st.Errors, 1)
			return nil // keep going past unreadable entries
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		if !matchAll && !exts[kinds.Ext(path)] {
			atomic.AddInt64(&st.Skipped, 1)
			return nil
		}

		res, herr := sparsehash.HashFile(path, coverage, sample)
		if herr != nil {
			atomic.AddInt64(&st.Errors, 1)
			return nil
		}

		atomic.AddInt64(&st.Files, 1)
		atomic.AddInt64(&st.LogicalBytes, res.Size)
		atomic.AddInt64(&st.SampledBytes, res.SampledBytes)
		if res.Full {
			atomic.AddInt64(&st.Full, 1)
		} else {
			atomic.AddInt64(&st.Sparse, 1)
		}
		if onFile != nil {
			onFile(Result{Path: path, Result: res})
		}
		return nil
	}

	conf := fastwalk.Config{NumWorkers: workers}
	if err := fastwalk.Walk(&conf, root, walkFn); err != nil {
		return st, err
	}
	st.Elapsed = time.Since(start)
	return st, nil
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package fsx

import (
	"os"
	"path/filepath"
	"sync"
)

// DirSet syncs each directory once: a run that publishes thousands of files
// into a few hundred folders pays one directory flush per folder, not per file.
// The zero value is ready to use and safe for concurrent use.
type DirSet struct {
	mu   sync.Mutex
	dirs map[string]bool
}

// Add records the directory holding path.
func (s *DirSet) Add(path string) {
	s.mu.Lock()
	if s.dirs == nil {
		s.dirs = map[string]bool{}
	}
	s.dirs[filepath.Dir(path)] = true
	s.mu.Unlock()
}

// SyncAll flushes every recorded directory (SyncDir) and returns the first
// error; the set is emptied. A folder that no longer exists is skipped: it
// holds no entry left to flush.
func (s *DirSet) SyncAll() error {
	s.mu.Lock()
	dirs := s.dirs
	s.dirs = nil
	s.mu.Unlock()
	var first error
	for d := range dirs {
		if err := SyncDir(d); err != nil && !os.IsNotExist(err) && first == nil {
			first = err
		}
	}
	return first
}

// Durable flushes the file at path and, once per directory in seen, the
// directory that names it: after it returns, the file's bytes and its name
// survive a power cut. A deletion that relies on another copy calls it on
// that copy first.
func Durable(path string, seen *DirSet) error {
	if err := SyncFile(path); err != nil {
		return err
	}
	dir := filepath.Dir(path)
	seen.mu.Lock()
	done := seen.dirs[dir]
	seen.mu.Unlock()
	if done {
		return nil
	}
	if err := SyncDir(dir); err != nil {
		return err
	}
	seen.Add(path)
	return nil
}

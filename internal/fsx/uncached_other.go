// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !linux && !windows && !darwin

package fsx

import (
	"errors"
	"os"
)

// OpenUncached is not available on this OS: the caller reads through the cache.
func OpenUncached(p string) (*os.File, error) {
	return nil, errors.New("uncached reads are not available on this OS")
}

// UncachedAlign is the buffer alignment OpenUncached reads need.
const UncachedAlign = 1

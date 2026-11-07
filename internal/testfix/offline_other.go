// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !windows

package testfix

// SetOffline is Windows only (ErrNoOffline elsewhere).
func SetOffline(string) error { return ErrNoOffline }

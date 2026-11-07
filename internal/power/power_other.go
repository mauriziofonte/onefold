// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !windows && !darwin && !linux

package power

import "errors"

var errUnsupported = errors.New("not supported on this system")

func keepAwake(string) (func(), error) { return nil, errUnsupported }

func setLowPriority(bool) error { return errUnsupported }

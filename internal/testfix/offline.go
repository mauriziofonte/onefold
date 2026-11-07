// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package testfix

import "errors"

// ErrNoOffline: only Windows lets a program mark a file as online-only
// (SetOffline); macOS sets SF_DATALESS only for a file provider, Linux has no
// marker.
var ErrNoOffline = errors.New("no settable online-only marker on this system")

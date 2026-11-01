// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package naming

import (
	"sync/atomic"

	"github.com/mauriziofonte/onefold/internal/names"
)

// Languages of month names in folder and file names (config names-language,
// D88): {month-name} follows it, {month-name-en} and {month-name-it} do not.
const (
	NamesEN = "en"
	NamesIT = "it"
)

var namesLanguage atomic.Value // string

// IsNamesLanguage reports whether l is a supported names-language.
func IsNamesLanguage(l string) bool { return l == NamesEN || l == NamesIT }

// SetNamesLanguage sets the language of {month-name} ("" = English).
func SetNamesLanguage(l string) { namesLanguage.Store(l) }

func monthNames() []string {
	if l, _ := namesLanguage.Load().(string); l == NamesIT {
		return names.MonthIT
	}
	return names.MonthEN
}

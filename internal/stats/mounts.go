// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package stats

import "sort"

// Mount is a volume a person can measure: a drive letter on Windows, a
// mounted file system on Linux and macOS (system and virtual ones left out).
type Mount struct {
	Path      string `json:"path"`
	Label     string `json:"label,omitempty"`
	FSType    string `json:"fsType"`
	Device    string `json:"device,omitempty"`
	Total     int64  `json:"total"`
	Free      int64  `json:"free"`
	Remote    bool   `json:"remote,omitempty"`
	Removable bool   `json:"removable,omitempty"`
	ReadOnly  bool   `json:"readOnly,omitempty"`
}

// Mounts lists the volumes, sorted by path. Errors on one volume leave it out.
func Mounts() []Mount {
	var out []Mount
	for _, m := range rawMounts() {
		if m.Total > 0 {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

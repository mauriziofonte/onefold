// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package gui

import (
	"embed"
	"io/fs"
)

// web/dist is the built Svelte frontend, committed so `go build` needs no
// Node (`make web` regenerates it; TestWebDistInSync checks it is current).
//
//go:embed all:web/dist
var dist embed.FS

func distFS() fs.FS {
	sub, err := fs.Sub(dist, "web/dist")
	if err != nil {
		panic(err)
	}
	return sub
}

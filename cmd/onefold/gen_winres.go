// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build ignore

// gen_winres writes the Windows resources (icon + manifest, and with
// -version the version information) linked into onefold.exe:
// `go generate ./cmd/onefold` (internal/winres); `make windows` adds the
// version of the build and restores the committed file afterwards.
package main

import (
	"flag"
	"log"
	"os"

	"github.com/mauriziofonte/onefold/internal/winres"
)

func main() {
	version := flag.String("version", "", "write a VS_VERSIONINFO for this version (release builds; the committed file has none)")
	flag.Parse()
	b, err := winres.BuildVersion(*version)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(winres.SysoName, b, 0o644); err != nil {
		log.Fatal(err)
	}
}

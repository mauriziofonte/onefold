// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build ignore

// gen_macapp writes the zip of onefold.app around a darwin binary:
// `go run internal/pack/gen_macapp.go -exe dist/onefold-darwin-arm64
// -version v0.0.2 -o dist/onefold-macos-arm64.zip` (make darwin-app).
package main

import (
	"bytes"
	"flag"
	"log"
	"os"

	"github.com/mauriziofonte/onefold/internal/pack"
)

func main() {
	exe := flag.String("exe", "", "the darwin binary to put in the bundle")
	version := flag.String("version", "dev", "the version of that binary")
	out := flag.String("o", "", "the zip to write")
	flag.Parse()
	if *exe == "" || *out == "" {
		log.Fatal("usage: gen_macapp -exe BINARY -version V -o ZIP")
	}
	b, err := os.ReadFile(*exe)
	if err != nil {
		log.Fatal(err)
	}
	var z bytes.Buffer
	if err := pack.MacApp(&z, b, *version); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, z.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build ignore

// gen_manifests writes the Homebrew, Scoop and winget files of a release
// from its SHA256SUMS: `go run internal/pack/gen_manifests.go -tag v0.0.2
// -date 2026-11-07 -sums out/SHA256SUMS -o out/manifests` (release.yml keeps
// them as the package-manifests artifact; submitting them is a manual step,
// see packaging/README.md).
package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/mauriziofonte/onefold/internal/pack"
)

func main() {
	tag := flag.String("tag", "", "the release tag (vX.Y.Z[-pre])")
	date := flag.String("date", "", "the release date, YYYY-MM-DD")
	sumsPath := flag.String("sums", "", "the release's SHA256SUMS")
	out := flag.String("o", "", "the folder to write into (must not exist)")
	flag.Parse()
	if *tag == "" || *date == "" || *sumsPath == "" || *out == "" {
		log.Fatal("usage: gen_manifests -tag vX.Y.Z -date YYYY-MM-DD -sums SHA256SUMS -o DIR")
	}
	f, err := os.Open(*sumsPath)
	if err != nil {
		log.Fatal(err)
	}
	sums, err := pack.ReadSums(f)
	f.Close()
	if err != nil {
		log.Fatal(err)
	}
	m, err := pack.Manifests(*tag, *date, sums)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.Mkdir(*out, 0o755); err != nil {
		log.Fatal(err)
	}
	for _, name := range pack.ManifestNames(m) {
		p := filepath.Join(*out, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			log.Fatal(err)
		}
		if err := os.WriteFile(p, m[name], 0o644); err != nil {
			log.Fatal(err)
		}
	}
}

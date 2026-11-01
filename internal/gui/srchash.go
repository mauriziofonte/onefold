// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package gui

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// webSourceFiles are the frontend inputs besides src/ (same list as
// web/scripts/srchash.mjs).
var webSourceFiles = []string{"index.html", "package.json", "package-lock.json", "vite.config.js"}

// SourceHash is the hash of the frontend sources in webDir: sorted
// slash-separated paths, each with the sha256 of its content without CR
// bytes (a CRLF checkout hashes the same). web/scripts/srchash.mjs writes it
// into dist/.source-hash at build time.
func SourceHash(webDir string) (string, error) {
	var files []string
	err := filepath.WalkDir(filepath.Join(webDir, "src"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(webDir, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	files = append(files, webSourceFiles...)
	sort.Strings(files)
	h := sha256.New()
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(webDir, filepath.FromSlash(f)))
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(bytes.ReplaceAll(b, []byte{'\r'}, nil))
		h.Write([]byte(f + "\n" + hex.EncodeToString(sum[:]) + "\n"))
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

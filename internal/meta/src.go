// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import (
	"archive/zip"
	"bytes"
	"os"
	"sync"
)

// preloaded holds file contents already read by the catalog (one sequential
// read per small file), keyed by path, for the duration of ExtractWith. Every
// extractor opens files through openSrc, so a preloaded file is never reopened
// (on Windows each first open of a file costs an on-access AV scan).
var preloaded sync.Map // path -> []byte

type src interface {
	Read([]byte) (int, error)
	ReadAt([]byte, int64) (int, error)
	Seek(int64, int) (int64, error)
	Close() error
}

type memSrc struct{ *bytes.Reader }

func (memSrc) Close() error { return nil }

// openSrc returns the preloaded bytes or opens the file, plus its size.
func openSrc(p string) (src, int64, error) {
	if v, ok := preloaded.Load(p); ok {
		b := v.([]byte)
		return memSrc{bytes.NewReader(b)}, int64(len(b)), nil
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, 0, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, fi.Size(), nil
}

// openZip is zip.OpenReader through openSrc.
func openZip(p string) (*zip.Reader, func(), error) {
	s, size, err := openSrc(p)
	if err != nil {
		return nil, nil, err
	}
	zr, err := zip.NewReader(s, size)
	if err != nil {
		s.Close()
		return nil, nil, err
	}
	return zr, func() { s.Close() }, nil
}

// preloadedHead returns the first n bytes if the file is preloaded.
func preloadedHead(p string, n int) ([]byte, bool) {
	v, ok := preloaded.Load(p)
	if !ok {
		return nil, false
	}
	b := v.([]byte)
	if len(b) > n {
		b = b[:n]
	}
	return b, true
}

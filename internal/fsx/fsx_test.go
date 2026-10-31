// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package fsx

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"unsafe"
)

func TestTempNames(t *testing.T) {
	dir := filepath.Join("a", "b")
	for _, c := range []struct {
		dst     string
		keepExt bool
		want    string
	}{
		{filepath.Join(dir, "x.jpg"), false, filepath.Join(dir, ".x.jpg.onefold-tmp")},
		{filepath.Join(dir, "VID_1.mp4"), true, filepath.Join(dir, ".VID_1.onefold-tmp.mp4")},
		{filepath.Join(dir, "noext"), true, filepath.Join(dir, ".noext.onefold-tmp")},
	} {
		got := TempFor(c.dst, c.keepExt)
		if got != c.want {
			t.Errorf("TempFor(%q, %v) = %q want %q", c.dst, c.keepExt, got, c.want)
		}
		if !IsTemp(filepath.Base(got)) {
			t.Errorf("IsTemp(%q) = false", got)
		}
	}
	if !IsTemp(".x.jpg.onefold-tmp_exiftool_tmp") || !IsTemp("a.jpg.123.onefold-tmp") {
		t.Error("exiftool temp of a onefold temp / merge temp not recognized")
	}
	for _, n := range []string{"x.jpg", "onefold-tmp", "x.onefold-tmpfile", "x.part", "user.onefold-tmp.mp4"} {
		if IsTemp(n) {
			t.Errorf("IsTemp(%q) = true", n)
		}
	}
}

// An uncached read gives the bytes of the file, through an aligned buffer.
func TestOpenUncached(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f.bin")
	want := make([]byte, 3*UncachedAlign+123)
	for i := range want {
		want[i] = byte(i * 7)
	}
	if err := os.WriteFile(p, want, 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := OpenUncached(p)
	if err != nil {
		if runtime.GOOS == "linux" || runtime.GOOS == "windows" {
			t.Fatal(err)
		}
		t.Skip(err)
	}
	defer f.Close()
	buf := AlignedBuffer(2 * UncachedAlign)
	if uintptr(unsafe.Pointer(&buf[0]))%UncachedAlign != 0 || len(buf)%UncachedAlign != 0 {
		t.Fatal("buffer not aligned")
	}
	var got []byte
	for {
		n, err := f.Read(buf)
		got = append(got, buf[:n]...)
		if err != nil || n == 0 {
			break
		}
	}
	if string(got) != string(want) {
		t.Fatalf("read %d bytes, want %d", len(got), len(want))
	}
}

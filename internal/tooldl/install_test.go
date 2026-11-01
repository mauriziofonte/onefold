// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package tooldl

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mauriziofonte/onefold/internal/fsx"
	"github.com/mauriziofonte/onefold/internal/tools"
)

func zipOf(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// serve answers every request with body (Range supported by ServeContent) and
// counts the requests carrying a Range header.
func serve(t *testing.T, body []byte, ranged *atomic.Int64) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" && ranged != nil {
			ranged.Add(1)
		}
		http.ServeContent(w, r, "pkg.zip", time.Time{}, bytes.NewReader(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/pkg.zip"
}

func zipPkg(t *testing.T, version string, files map[string]string, mapping []File) (Package, []byte) {
	body := zipOf(t, files)
	return Package{ID: "demo", Name: "Demo", Version: version, Provides: []string{"demo"}, Size: int64(len(body)),
		SHA256: sum(body), Format: "zip", Files: mapping}, body
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInstallZip(t *testing.T) {
	p, body := zipPkg(t, "1", map[string]string{
		"demo-1/bin/demo.exe":      "EXE",
		"demo-1/files/lib/a.dll":   "DLL",
		"demo-1/files/b.txt":       "B",
		"demo-1/README":            "not mapped",
		"demo-1/files/../evil.txt": "escape attempt",
	}, []File{{From: "demo-1/bin/demo.exe", To: "demo.exe"}, {From: "demo-1/files/", To: "demo_files/"}})
	p.URL = serve(t, body, nil)
	dir := t.TempDir()
	var last Progress
	in := &Installer{Dir: dir, OnProgress: func(pr Progress) { last = pr }}
	if err := in.Install(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dir, "demo.exe")) != "EXE" || read(t, filepath.Join(dir, "demo_files", "lib", "a.dll")) != "DLL" ||
		read(t, filepath.Join(dir, "demo_files", "b.txt")) != "B" {
		t.Fatal("mapped files not installed")
	}
	for _, bad := range []string{"README", "evil.txt", filepath.Join("..", "evil.txt")} {
		if _, err := os.Stat(filepath.Join(dir, bad)); err == nil {
			t.Fatalf("%s must not be installed", bad)
		}
	}
	if last.Phase != PhaseDone || last.Done != p.Size {
		t.Fatalf("last progress %+v", last)
	}
	got := ReadInstalled(dir)["demo"]
	if got.Version != "1" || strings.Join(got.Files, ",") != "demo.exe,demo_files/" {
		t.Fatalf("installed.json: %+v", got)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".download", "*")); len(left) != 0 {
		t.Fatalf("download leftovers: %v", left)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".staging-*")); len(left) != 0 {
		t.Fatalf("staging leftovers: %v", left)
	}
}

func TestChecksumMismatchInstallsNothing(t *testing.T) {
	p, body := zipPkg(t, "1", map[string]string{"x/demo.exe": "EXE"}, []File{{From: "x/demo.exe", To: "demo.exe"}})
	p.URL = serve(t, body, nil)
	p.SHA256 = strings.Repeat("0", 64)
	dir := t.TempDir()
	var last Progress
	err := (&Installer{Dir: dir, OnProgress: func(pr Progress) { last = pr }}).Install(context.Background(), p)
	if err == nil || !strings.Contains(err.Error(), "pinned checksum") {
		t.Fatalf("want checksum error, got %v", err)
	}
	if last.Phase != PhaseError {
		t.Fatalf("last progress %+v", last)
	}
	if _, err := os.Stat(filepath.Join(dir, "demo.exe")); err == nil {
		t.Fatal("tool installed despite the mismatch")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".download", "*")); len(left) != 0 {
		t.Fatalf("mismatching download kept: %v", left)
	}
	if len(ReadInstalled(dir)) != 0 {
		t.Fatal("mismatch recorded as installed")
	}
}

func TestResumeUsesRange(t *testing.T) {
	files := map[string]string{"x/demo.exe": strings.Repeat("payload ", 50000)}
	p, body := zipPkg(t, "1", files, []File{{From: "x/demo.exe", To: "demo.exe"}})
	var ranged atomic.Int64
	p.URL = serve(t, body, &ranged)
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".download"), 0o755)
	if err := os.WriteFile(filepath.Join(dir, ".download", "demo-1"+fsx.TempSuffix), body[:len(body)/2], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := (&Installer{Dir: dir}).Install(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if ranged.Load() != 1 {
		t.Fatalf("want one ranged request, got %d", ranged.Load())
	}
	if read(t, filepath.Join(dir, "demo.exe")) != files["x/demo.exe"] {
		t.Fatal("resumed file differs")
	}
}

func TestReinstallReplacesOldFiles(t *testing.T) {
	dir := t.TempDir()
	p1, b1 := zipPkg(t, "1", map[string]string{"v1/demo.exe": "ONE", "v1/old.dll": "OLD"},
		[]File{{From: "v1/demo.exe", To: "demo.exe"}, {From: "v1/old.dll", To: "old.dll"}})
	p1.URL = serve(t, b1, nil)
	if err := (&Installer{Dir: dir}).Install(context.Background(), p1); err != nil {
		t.Fatal(err)
	}
	p2, b2 := zipPkg(t, "2", map[string]string{"v2/demo.exe": "TWO"}, []File{{From: "v2/demo.exe", To: "demo.exe"}})
	p2.URL = serve(t, b2, nil)
	if err := (&Installer{Dir: dir}).Install(context.Background(), p2); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dir, "demo.exe")) != "TWO" {
		t.Fatal("not replaced")
	}
	if _, err := os.Stat(filepath.Join(dir, "old.dll")); err == nil {
		t.Fatal("file of the previous version left behind")
	}
	if ReadInstalled(dir)["demo"].Version != "2" {
		t.Fatal("installed.json not updated")
	}
}

func TestMissingMappedFileFails(t *testing.T) {
	p, body := zipPkg(t, "1", map[string]string{"x/other.exe": "X"}, []File{{From: "x/demo.exe", To: "demo.exe"}})
	p.URL = serve(t, body, nil)
	if err := (&Installer{Dir: t.TempDir()}).Install(context.Background(), p); err == nil {
		t.Fatal("want an error for a missing member")
	}
}

func TestMapMember(t *testing.T) {
	tree := File{From: "a/files/", To: "t/"}
	for name, want := range map[string]string{
		"a/files/x.dll":     "t/x.dll",
		`a\files\sub\y.txt`: "t/sub/y.txt",
		"a/files/sub/":      "t/sub/",
		"a/files/../e.txt":  "",
		"a/files/":          "",
		"a/other/x.dll":     "",
	} {
		got, ok := mapMember(tree, name)
		if (want == "") == ok || got != want {
			t.Errorf("%q -> %q %v, want %q", name, got, ok, want)
		}
	}
}

func TestSFXInstaller(t *testing.T) {
	arch, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "archives", "sample.7z"))
	if err != nil {
		t.Fatal(err)
	}
	// a stub that contains the 7z signature itself (as the real 7-Zip one does)
	stub := append(append([]byte("MZ stub "), sevenZipSig...), bytes.Repeat([]byte{0x90}, 1000)...)
	body := append(stub, arch...)
	p := Package{ID: "sfx", Name: "SFX", Version: "1", Size: int64(len(body)), SHA256: sum(body), Format: "7z-sfx",
		Files: []File{{From: "file1.txt", To: "one.txt"}}}
	p.URL = serve(t, body, nil)
	dir := t.TempDir()
	if err := (&Installer{Dir: dir}).Install(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "one.txt")); err != nil || fi.Size() != 38 {
		t.Fatalf("member not extracted: %v", err)
	}
}

func TestCatalogPins(t *testing.T) {
	hex64 := regexp.MustCompile(`^[0-9a-f]{64}$`)
	known := map[string]bool{}
	for _, n := range tools.Known {
		known[n] = true
	}
	ids := map[string]bool{}
	for _, p := range windowsAMD64 {
		if ids[p.ID] {
			t.Errorf("duplicate id %s", p.ID)
		}
		ids[p.ID] = true
		if !hex64.MatchString(p.SHA256) || p.Size <= 0 || !strings.HasPrefix(p.URL, "https://") || len(p.Files) == 0 {
			t.Errorf("%s: incomplete pin", p.ID)
		}
		if !strings.Contains(p.URL, p.Version) {
			t.Errorf("%s: URL does not carry the pinned version %s", p.ID, p.Version)
		}
		for _, n := range p.Provides {
			if !known[n] {
				t.Errorf("%s provides unknown tool %s", p.ID, n)
			}
		}
	}
}

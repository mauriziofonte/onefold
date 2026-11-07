// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package pack

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"encoding/xml"
	"image/png"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// plistDict reads the top dict of a property list as key -> value ("true"
// for <true/>).
func plistDict(t *testing.T, b []byte) map[string]string {
	t.Helper()
	d := xml.NewDecoder(bytes.NewReader(b))
	d.Strict = true
	out := map[string]string{}
	var key, text string
	depth := 0
	for {
		tok, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("plist: %v", err)
		}
		switch e := tok.(type) {
		case xml.StartElement:
			depth++
			text = ""
			if e.Name.Local == "true" || e.Name.Local == "false" {
				out[key] = e.Name.Local
			}
		case xml.CharData:
			text += string(e)
		case xml.EndElement:
			depth--
			switch e.Name.Local {
			case "key":
				key = text
			case "string":
				out[key] = text
			}
		}
	}
	return out
}

func TestInfoPlist(t *testing.T) {
	for _, c := range []struct{ version, short, info string }{
		{"v1.2.3-alpha", "1.2.3", "onefold v1.2.3-alpha"},
		{"dev", "0.0.0", "onefold dev"},
		{"v0.0.2-<x>&", "0.0.2", "onefold v0.0.2-<x>&"},
	} {
		d := plistDict(t, InfoPlist(c.version))
		want := map[string]string{
			"CFBundleExecutable":         "onefold",
			"CFBundleIdentifier":         BundleID,
			"CFBundleIconFile":           "onefold",
			"CFBundlePackageType":        "APPL",
			"CFBundleShortVersionString": c.short,
			"CFBundleVersion":            c.short,
			"CFBundleGetInfoString":      c.info,
			"LSMinimumSystemVersion":     MinMacOS,
			"LSUIElement":                "true",
		}
		for k, v := range want {
			if d[k] != v {
				t.Errorf("%s: %s = %q, want %q", c.version, k, d[k], v)
			}
		}
	}
}

func TestIcns(t *testing.T) {
	b, err := Icns()
	if err != nil {
		t.Fatal(err)
	}
	if string(b[:4]) != "icns" || int(binary.BigEndian.Uint32(b[4:8])) != len(b) {
		t.Fatalf("header %q, length %d of %d", b[:4], binary.BigEndian.Uint32(b[4:8]), len(b))
	}
	var got []string
	for p := 8; p < len(b); {
		typ, n := string(b[p:p+4]), int(binary.BigEndian.Uint32(b[p+4:p+8]))
		img, err := png.Decode(bytes.NewReader(b[p+8 : p+n]))
		if err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		got = append(got, typ)
		for _, e := range icnsTypes {
			if e.typ == typ && (img.Bounds().Dx() != e.size || img.Bounds().Dy() != e.size) {
				t.Errorf("%s: %v, want %d px", typ, img.Bounds(), e.size)
			}
		}
		p += n
	}
	if len(got) != len(icnsTypes) {
		t.Errorf("entries %v", got)
	}
}

func TestMacApp(t *testing.T) {
	exe := []byte("\xcf\xfa\xed\xfe not a real binary")
	var a, b bytes.Buffer
	if err := MacApp(&a, exe, "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	if err := MacApp(&b, exe, "v1.0.0"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("two builds of the same bundle differ")
	}
	z, err := zip.NewReader(bytes.NewReader(a.Bytes()), int64(a.Len()))
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		name string
		mode fs.FileMode
	}{
		{"onefold.app/", fs.ModeDir | 0o755},
		{"onefold.app/Contents/", fs.ModeDir | 0o755},
		{"onefold.app/Contents/Info.plist", 0o644},
		{"onefold.app/Contents/PkgInfo", 0o644},
		{"onefold.app/Contents/MacOS/", fs.ModeDir | 0o755},
		{"onefold.app/Contents/MacOS/onefold", 0o755},
		{"onefold.app/Contents/Resources/", fs.ModeDir | 0o755},
		{"onefold.app/Contents/Resources/onefold.icns", 0o644},
	}
	if len(z.File) != len(want) {
		t.Fatalf("%d entries, want %d", len(z.File), len(want))
	}
	for i, f := range z.File {
		if f.Name != want[i].name || f.Mode() != want[i].mode {
			t.Errorf("entry %d: %s %v, want %s %v", i, f.Name, f.Mode(), want[i].name, want[i].mode)
		}
		if !f.Modified.Equal(modTime) {
			t.Errorf("%s: time %v", f.Name, f.Modified)
		}
		if f.CreatorVersion>>8 != 3 {
			t.Errorf("%s: made by %d, want Unix (3): macOS would drop the modes", f.Name, f.CreatorVersion>>8)
		}
		if strings.HasSuffix(f.Name, "/") {
			continue
		}
		r, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(r)
		switch {
		case strings.HasSuffix(f.Name, "MacOS/onefold") && !bytes.Equal(body, exe):
			t.Error("the executable changed in the zip")
		case strings.HasSuffix(f.Name, "PkgInfo") && string(body) != "APPL????":
			t.Errorf("PkgInfo %q", body)
		case strings.HasSuffix(f.Name, "Info.plist") && plistDict(t, body)["CFBundleShortVersionString"] != "1.0.0":
			t.Error("Info.plist does not carry the version")
		}
	}
}

// The menu entry of the Linux packages (packaging/linux/onefold.desktop,
// installed by packaging/nfpm.yaml) starts the GUI with the icon of the
// packages; CI also runs desktop-file-validate on it.
func TestDesktopEntry(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "packaging", "linux", "onefold.desktop"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if lines[0] != "[Desktop Entry]" {
		t.Fatalf("first line %q", lines[0])
	}
	got := map[string]string{}
	for _, l := range lines[1:] {
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			t.Fatalf("line %q", l)
		}
		got[k] = v
	}
	for k, v := range map[string]string{"Type": "Application", "Name": "onefold", "Exec": "onefold gui", "Icon": "onefold", "Terminal": "false"} {
		if got[k] != v {
			t.Errorf("%s=%q, want %q", k, got[k], v)
		}
	}
}

func TestManifests(t *testing.T) {
	sums := map[string]string{}
	for i, a := range managerAssets {
		sums[a] = strings.Repeat(string(rune('a'+i)), 64)
	}
	m, err := Manifests("v1.2.3-beta.1", "2026-11-07", sums)
	if err != nil {
		t.Fatal(err)
	}
	wd := "winget/manifests/m/MaurizioFonte/onefold/1.2.3-beta.1/"
	want := []string{"homebrew/onefold.rb", "scoop/onefold.json",
		wd + "MaurizioFonte.onefold.installer.yaml", wd + "MaurizioFonte.onefold.locale.en-US.yaml", wd + "MaurizioFonte.onefold.yaml"}
	sort.Strings(want)
	if got := ManifestNames(m); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("files %v", got)
	}
	base := "https://github.com/mauriziofonte/onefold/releases/download/v1.2.3-beta.1/"
	rb := string(m["homebrew/onefold.rb"])
	for _, a := range managerAssets[:4] {
		if !strings.Contains(rb, `url "`+base+a+`"`+"\n      sha256 \""+sums[a]+`"`) {
			t.Errorf("formula: %s without its url and hash", a)
		}
	}
	opens, ends := 0, 0
	for _, l := range strings.Split(rb, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasSuffix(l, " do") || strings.HasPrefix(l, "class ") || strings.HasPrefix(l, "def ") {
			opens++
		}
		if l == "end" {
			ends++
		}
	}
	if !strings.Contains(rb, `version "1.2.3-beta.1"`) || opens != ends || opens < 8 {
		t.Errorf("formula: version, or %d blocks opened and %d closed:\n%s", opens, ends, rb)
	}
	var sc struct {
		Version      string
		Bin          string
		Architecture map[string]struct{ URL, Hash string }
		Autoupdate   struct {
			Architecture map[string]struct{ URL string }
		}
	}
	if err := json.Unmarshal(m["scoop/onefold.json"], &sc); err != nil {
		t.Fatal(err)
	}
	if sc.Version != "1.2.3-beta.1" || sc.Bin != "onefold.exe" ||
		sc.Architecture["64bit"].URL != base+"onefold-windows-amd64.exe#/onefold.exe" || sc.Architecture["64bit"].Hash != sums["onefold-windows-amd64.exe"] ||
		!strings.Contains(sc.Autoupdate.Architecture["64bit"].URL, "/v$version/") {
		t.Errorf("scoop: %+v", sc)
	}
	inst := string(m[wd+"MaurizioFonte.onefold.installer.yaml"])
	for _, s := range []string{"PackageVersion: 1.2.3-beta.1\n", "InstallerType: portable\n", "ReleaseDate: 2026-11-07\n",
		"InstallerUrl: " + base + "onefold-windows-amd64.exe\n", "InstallerSha256: " + strings.ToUpper(sums["onefold-windows-amd64.exe"]) + "\n"} {
		if !strings.Contains(inst, s) {
			t.Errorf("winget installer lacks %q", s)
		}
	}
	for _, name := range want {
		if strings.HasSuffix(name, "yaml") {
			for _, l := range strings.Split(string(m[name]), "\n") {
				if strings.Contains(l, "\t") || strings.HasSuffix(l, " ") {
					t.Errorf("%s: line %q", name, l)
				}
			}
			if !strings.Contains(string(m[name]), "ManifestVersion: 1.9.0\n") {
				t.Errorf("%s: no manifest version", name)
			}
		}
	}

	delete(sums, "onefold-linux-arm64")
	if _, err := Manifests("v1.2.3", "2026-11-07", sums); err == nil {
		t.Error("a missing hash was accepted")
	}
	if _, err := Manifests("1.2.3", "2026-11-07", sums); err == nil {
		t.Error("a version without v was accepted")
	}
}

func TestReadSums(t *testing.T) {
	h := strings.Repeat("0", 64)
	got, err := ReadSums(strings.NewReader(h + "  onefold-linux-amd64\n" + h + " *onefold-windows-amd64.exe\n\n"))
	if err != nil || got["onefold-linux-amd64"] != h || got["onefold-windows-amd64.exe"] != h || len(got) != 2 {
		t.Fatalf("%v %v", got, err)
	}
	if _, err := ReadSums(strings.NewReader("xyz  file\n")); err == nil {
		t.Error("a bad line was accepted")
	}
}

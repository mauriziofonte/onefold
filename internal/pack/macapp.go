// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package pack builds the release packages that are more than a binary: the
// macOS application bundle (onefold.app, zipped) with its icon. The Linux
// packages (.deb, .rpm, .apk) are described in packaging/nfpm.yaml.
//
// Everything is deterministic: the same binary and version give the same
// bytes, so a release can be rebuilt and compared.
package pack

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"text/template"
	"time"

	"github.com/mauriziofonte/onefold/internal/winres"
)

// BundleID is the bundle identifier of onefold.app (the launchd jobs of
// `onefold schedule` share its prefix).
const BundleID = "io.github.mauriziofonte.onefold"

// MinMacOS is the oldest macOS the Go toolchain of the release supports.
const MinMacOS = "12.0"

// AppName is the folder of the bundle inside the zip.
const AppName = "onefold.app"

// modTime is the time of every zip entry: a fixed instant, so the zip
// depends only on its content.
var modTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

var plist = template.Must(template.New("plist").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleDevelopmentRegion</key>
	<string>en</string>
	<key>CFBundleDisplayName</key>
	<string>onefold</string>
	<key>CFBundleExecutable</key>
	<string>onefold</string>
	<key>CFBundleIconFile</key>
	<string>onefold</string>
	<key>CFBundleIdentifier</key>
	<string>{{.ID}}</string>
	<key>CFBundleInfoDictionaryVersion</key>
	<string>6.0</string>
	<key>CFBundleName</key>
	<string>onefold</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>{{.Short}}</string>
	<key>CFBundleVersion</key>
	<string>{{.Short}}</string>
	<key>CFBundleGetInfoString</key>
	<string>onefold {{.Version}}</string>
	<key>LSApplicationCategoryType</key>
	<string>public.app-category.utilities</string>
	<key>LSMinimumSystemVersion</key>
	<string>{{.Min}}</string>
	<key>LSUIElement</key>
	<true/>
	<key>NSHighResolutionCapable</key>
	<true/>
	<key>NSHumanReadableCopyright</key>
	<string>Copyright (c) 2026 Maurizio Fonte. GNU AGPL-3.0-or-later.</string>
</dict>
</plist>
`))

// InfoPlist is Contents/Info.plist for a build of version ("v0.0.2-alpha"):
// macOS shows the numbers (0.0.2) and keeps the full version in the info
// string. LSUIElement: no Dock icon and no menu bar, since the GUI lives in
// the browser and the process ends by itself when no page is open.
func InfoPlist(version string) []byte {
	n := winres.VersionNumbers(version)
	var b bytes.Buffer
	esc := func(s string) string {
		var w strings.Builder
		_ = xml.EscapeText(&w, []byte(s))
		return w.String()
	}
	_ = plist.Execute(&b, map[string]string{
		"ID":      BundleID,
		"Short":   fmt.Sprintf("%d.%d.%d", n[0], n[1], n[2]),
		"Version": esc(version),
		"Min":     MinMacOS,
	})
	return b.Bytes()
}

// MacApp writes the zip of onefold.app around exe, a darwin binary of
// version: Contents/Info.plist, Contents/PkgInfo, Contents/MacOS/onefold
// (mode 0755) and Contents/Resources/onefold.icns. Started from Finder with
// no arguments, the binary sees that it runs inside a bundle and opens the
// GUI (cmd/onefold).
func MacApp(w io.Writer, exe []byte, version string) error {
	icns, err := Icns()
	if err != nil {
		return err
	}
	z := zip.NewWriter(w)
	dir := func(name string) error {
		h := &zip.FileHeader{Name: AppName + "/" + name, Modified: modTime}
		h.SetMode(fs.ModeDir | 0o755)
		_, err := z.CreateHeader(h)
		return err
	}
	file := func(name string, mode fs.FileMode, body []byte) error {
		h := &zip.FileHeader{Name: AppName + "/" + name, Method: zip.Deflate, Modified: modTime}
		h.SetMode(mode)
		f, err := z.CreateHeader(h)
		if err != nil {
			return err
		}
		_, err = f.Write(body)
		return err
	}
	steps := []func() error{
		func() error { return dir("") },
		func() error { return dir("Contents/") },
		func() error { return file("Contents/Info.plist", 0o644, InfoPlist(version)) },
		func() error { return file("Contents/PkgInfo", 0o644, []byte("APPL????")) },
		func() error { return dir("Contents/MacOS/") },
		func() error { return file("Contents/MacOS/onefold", 0o755, exe) },
		func() error { return dir("Contents/Resources/") },
		func() error { return file("Contents/Resources/onefold.icns", 0o644, icns) },
	}
	for _, s := range steps {
		if err := s(); err != nil {
			return err
		}
	}
	return z.Close()
}

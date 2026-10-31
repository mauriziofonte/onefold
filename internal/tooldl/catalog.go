// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package tooldl downloads the optional external tools (internal/tools) from
// their authoritative sources into the managed tools directory. Every package
// is pinned to one version, one URL and its SHA-256: a download that does not
// match is discarded, so a moved or tampered file can never be installed.
// Updating a tool means a new onefold release with a new pin.
package tooldl

import "runtime"

// Package is one downloadable archive and the files taken from it.
type Package struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	Provides []string `json:"provides"` // tool names (internal/tools)
	URL      string   `json:"url"`
	Size     int64    `json:"size"`
	SHA256   string   `json:"-"`
	Format   string   `json:"-"` // "zip" or "7z-sfx" (7-Zip installer: an exe with a 7z archive appended)
	Files    []File   `json:"-"`
	Home     string   `json:"home"`    // page of the source, for humans
	License  string   `json:"license"` // as published by the source
}

// File maps an archive member to its path in the managed dir. A From ending
// in "/" copies that whole subtree under To.
type File struct{ From, To string }

// Pins verified 2026-10-06: exiftool against exiftool.org checksums-13.59.txt,
// ffmpeg and 7-Zip against the GitHub release asset digests, xpdf against its
// GPG signature (key A560 06CA 75CF 8B13 FA2F 120D F482 5F53 9727 1342 from
// xpdfreader.com). BtbN keeps month-end builds for about two years: pin one of
// those, never a daily build.
var windowsAMD64 = []Package{
	{
		ID: "exiftool", Name: "ExifTool", Version: "13.59", Provides: []string{"exiftool"},
		URL:    "https://sourceforge.net/projects/exiftool/files/exiftool-13.59_64.zip/download",
		Size:   11183675,
		SHA256: "44b512b25af500724ba579d0a53c8fc5851628b692dd5e5d94ae4a15c2cba9ec",
		Format: "zip",
		Files: []File{
			{From: "exiftool-13.59_64/exiftool(-k).exe", To: "exiftool.exe"},
			{From: "exiftool-13.59_64/exiftool_files/", To: "exiftool_files/"},
		},
		Home: "https://exiftool.org/", License: "Perl (Artistic / GPL)",
	},
	{
		ID: "ffmpeg", Name: "FFmpeg (BtbN build, LGPL)", Version: "9.0.2", Provides: []string{"ffmpeg", "ffprobe"},
		URL:    "https://github.com/BtbN/FFmpeg-Builds/releases/download/autobuild-2026-09-30-13-08/ffmpeg-n9.0.2-17-g2a571b6068-win64-lgpl-9.0.zip",
		Size:   171535354,
		SHA256: "6b264b9e6019103f601d98c292bd332fd87acf1c5e941ddff4fb71760fe63432",
		Format: "zip",
		Files: []File{
			{From: "ffmpeg-n9.0.2-17-g2a571b6068-win64-lgpl-9.0/bin/ffmpeg.exe", To: "ffmpeg.exe"},
			{From: "ffmpeg-n9.0.2-17-g2a571b6068-win64-lgpl-9.0/bin/ffprobe.exe", To: "ffprobe.exe"},
		},
		Home: "https://github.com/BtbN/FFmpeg-Builds", License: "LGPL",
	},
	{
		ID: "xpdf", Name: "Xpdf command line tools", Version: "4.06", Provides: []string{"pdfinfo", "pdftotext"},
		URL:    "https://dl.xpdfreader.com/xpdf-tools-win-4.06.zip",
		Size:   12826385,
		SHA256: "2b6ca45da794e7854a6468fd6c8063fde62701f001ce03fa4f603eab7e15a0b6",
		Format: "zip",
		Files: []File{
			{From: "xpdf-tools-win-4.06/bin64/pdfinfo.exe", To: "pdfinfo.exe"},
			{From: "xpdf-tools-win-4.06/bin64/pdftotext.exe", To: "pdftotext.exe"},
		},
		Home: "https://www.xpdfreader.com/", License: "GPL",
	},
	{
		ID: "7zip", Name: "7-Zip", Version: "26.04", Provides: []string{"7z"},
		URL:    "https://github.com/ip7z/7zip/releases/download/26.04/7z2604-x64.exe",
		Size:   1664082,
		SHA256: "d54bf805f9f3704d1e8db2fa3498ae7ef2df0312b40b558e7c71c734430a665d",
		Format: "7z-sfx",
		Files: []File{
			{From: "7z.exe", To: "7z.exe"},
			{From: "7z.dll", To: "7z.dll"},
		},
		Home: "https://www.7-zip.org/", License: "LGPL + unRAR restriction",
	},
}

// Catalog lists the packages downloadable on this platform (Windows x64
// only: elsewhere the system package manager provides the tools).
func Catalog() []Package {
	if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
		return windowsAMD64
	}
	return nil
}

// Find returns the catalog package with that id.
func Find(id string) (Package, bool) {
	for _, p := range Catalog() {
		if p.ID == id {
			return p, true
		}
	}
	return Package{}, false
}

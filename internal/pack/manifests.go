// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package pack

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"text/template"
)

// Repo is where the releases are published.
const Repo = "mauriziofonte/onefold"

// Summary is the one-line description in every package manager.
const Summary = "One copy of every file you own, under a name you can predict"

// The release assets the package managers download.
var managerAssets = []string{
	"onefold-darwin-arm64", "onefold-darwin-amd64",
	"onefold-linux-arm64", "onefold-linux-amd64",
	"onefold-windows-amd64.exe",
}

// ReadSums parses a SHA256SUMS file (`sha256sum` output: hash, two spaces
// or a space and a star, name).
func ReadSums(r io.Reader) (map[string]string, error) {
	sums := map[string]string{}
	hex := regexp.MustCompile(`^[0-9a-f]{64}$`)
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		h, name, ok := strings.Cut(line, " ")
		name = strings.TrimPrefix(strings.TrimPrefix(name, " "), "*")
		if !ok || !hex.MatchString(h) || name == "" {
			return nil, fmt.Errorf("SHA256SUMS: bad line %q", line)
		}
		sums[name] = h
	}
	return sums, sc.Err()
}

var (
	tagRe = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

	formula = template.Must(template.New("formula").Parse(`# Homebrew formula of onefold {{.Tag}}, written by internal/pack from the
# release's SHA256SUMS. It installs the release binary (a static Go binary,
# no build), so Gatekeeper has no download mark to stop.
class Onefold < Formula
  desc "{{.Summary}}"
  homepage "https://github.com/{{.Repo}}"
  version "{{.Version}}"
  license "AGPL-3.0-or-later"

  on_macos do
    on_arm do
      url "{{.Base}}/onefold-darwin-arm64"
      sha256 "{{index .Sums "onefold-darwin-arm64"}}"
    end
    on_intel do
      url "{{.Base}}/onefold-darwin-amd64"
      sha256 "{{index .Sums "onefold-darwin-amd64"}}"
    end
  end

  on_linux do
    on_arm do
      url "{{.Base}}/onefold-linux-arm64"
      sha256 "{{index .Sums "onefold-linux-arm64"}}"
    end
    on_intel do
      url "{{.Base}}/onefold-linux-amd64"
      sha256 "{{index .Sums "onefold-linux-amd64"}}"
    end
  end

  def install
    bin.install Dir["onefold-*"].first => "onefold"
  end

  def caveats
    <<~EOS
      Optional tools (dates of more video formats, metadata written into copies, PDF text):
        brew install exiftool ffmpeg poppler
      The GUI: onefold gui
    EOS
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/onefold version")
  end
end
`))

	wingetVersion = template.Must(template.New("version").Parse(`# yaml-language-server: $schema=https://aka.ms/winget-manifest.version.1.9.0.schema.json
PackageIdentifier: {{.WingetID}}
PackageVersion: {{.Version}}
DefaultLocale: en-US
ManifestType: version
ManifestVersion: 1.9.0
`))

	wingetInstaller = template.Must(template.New("installer").Parse(`# yaml-language-server: $schema=https://aka.ms/winget-manifest.installer.1.9.0.schema.json
PackageIdentifier: {{.WingetID}}
PackageVersion: {{.Version}}
InstallerType: portable
Commands:
- onefold
ReleaseDate: {{.Date}}
Installers:
- Architecture: x64
  InstallerUrl: {{.Base}}/onefold-windows-amd64.exe
  InstallerSha256: {{.WinSum}}
ManifestType: installer
ManifestVersion: 1.9.0
`))

	wingetLocale = template.Must(template.New("locale").Parse(`# yaml-language-server: $schema=https://aka.ms/winget-manifest.defaultLocale.1.9.0.schema.json
PackageIdentifier: {{.WingetID}}
PackageVersion: {{.Version}}
PackageLocale: en-US
Publisher: Maurizio Fonte
PublisherUrl: https://github.com/mauriziofonte
PublisherSupportUrl: https://github.com/{{.Repo}}/issues
PackageName: onefold
PackageUrl: https://github.com/{{.Repo}}
License: AGPL-3.0-or-later
LicenseUrl: https://github.com/{{.Repo}}/blob/{{.Tag}}/LICENSE
ShortDescription: {{.Summary}}
Description: onefold merges folders and disks so that every distinct file is kept once, tidies a folder in place under a naming layout, measures what fills a disk and builds a photo and video archive from several machines. Every action starts as a preview. Started with no arguments from Explorer or the Start menu it opens its GUI.
Tags:
- deduplication
- duplicates
- photos
- backup
- archive
ReleaseNotesUrl: https://github.com/{{.Repo}}/releases/tag/{{.Tag}}
ManifestType: defaultLocale
ManifestVersion: 1.9.0
`))
)

// WingetID is the package identifier proposed to winget-pkgs.
const WingetID = "MaurizioFonte.onefold"

// Manifests are the package-manager files of the release tag ("v0.0.2-alpha")
// published on date (YYYY-MM-DD), from the hashes of its SHA256SUMS: the
// Homebrew formula (homebrew/onefold.rb, for a tap), the Scoop manifest
// (scoop/onefold.json, for a bucket) and the three winget manifests
// (winget/<id path>/<version>/...). Paths are slash-separated.
func Manifests(tag, date string, sums map[string]string) (map[string][]byte, error) {
	if !tagRe.MatchString(tag) {
		return nil, fmt.Errorf("version %q is not a release tag (vX.Y.Z[-pre])", tag)
	}
	if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(date) {
		return nil, fmt.Errorf("date %q is not YYYY-MM-DD", date)
	}
	for _, a := range managerAssets {
		if sums[a] == "" {
			return nil, fmt.Errorf("SHA256SUMS has no %s", a)
		}
	}
	d := map[string]any{
		"Tag":      tag,
		"Version":  strings.TrimPrefix(tag, "v"),
		"Repo":     Repo,
		"Summary":  Summary,
		"Base":     "https://github.com/" + Repo + "/releases/download/" + tag,
		"Sums":     sums,
		"WinSum":   strings.ToUpper(sums["onefold-windows-amd64.exe"]),
		"WingetID": WingetID,
		"Date":     date,
	}
	out := map[string][]byte{}
	render := func(name string, t *template.Template) error {
		var b bytes.Buffer
		if err := t.Execute(&b, d); err != nil {
			return err
		}
		out[name] = b.Bytes()
		return nil
	}
	ver := d["Version"].(string)
	wdir := "winget/manifests/m/MaurizioFonte/onefold/" + ver + "/"
	for name, t := range map[string]*template.Template{
		"homebrew/onefold.rb":                  formula,
		wdir + WingetID + ".yaml":              wingetVersion,
		wdir + WingetID + ".installer.yaml":    wingetInstaller,
		wdir + WingetID + ".locale.en-US.yaml": wingetLocale,
	} {
		if err := render(name, t); err != nil {
			return nil, err
		}
	}
	scoop, err := scoopManifest(tag, d["Base"].(string), sums["onefold-windows-amd64.exe"])
	if err != nil {
		return nil, err
	}
	out["scoop/onefold.json"] = scoop
	return out, nil
}

// scoopManifest is the Scoop manifest: the exe under the name onefold.exe,
// a Start menu shortcut (started from it with no arguments, onefold opens
// its GUI), and checkver/autoupdate so a bucket can follow new releases.
func scoopManifest(tag, base, sum string) ([]byte, error) {
	exe := func(b string) string { return b + "/onefold-windows-amd64.exe#/onefold.exe" }
	m := map[string]any{
		"version":     strings.TrimPrefix(tag, "v"),
		"description": Summary,
		"homepage":    "https://github.com/" + Repo,
		"license":     "AGPL-3.0-or-later",
		"architecture": map[string]any{
			"64bit": map[string]any{"url": exe(base), "hash": sum},
		},
		"bin":       "onefold.exe",
		"shortcuts": [][]string{{"onefold.exe", "onefold"}},
		"notes":     "Optional tools: onefold tools install all (or the Doctor page of the GUI).",
		"checkver":  map[string]any{"github": "https://github.com/" + Repo},
		"autoupdate": map[string]any{
			"architecture": map[string]any{
				"64bit": map[string]any{"url": exe("https://github.com/" + Repo + "/releases/download/v$version")},
			},
			"hash": map[string]any{"url": "$baseurl/SHA256SUMS"},
		},
	}
	b, err := json.MarshalIndent(m, "", "    ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// ManifestNames are the paths of Manifests, sorted.
func ManifestNames(m map[string][]byte) []string {
	var names []string
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

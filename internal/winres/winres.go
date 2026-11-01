// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package winres builds the Windows resources linked into onefold.exe: the
// application icon (the GUI logo, rasterized here so no image tool is needed)
// and the application manifest (per-monitor DPI awareness, long paths). The
// output is a COFF object (.syso) the Go linker picks up from the package
// directory of cmd/onefold; it is committed, so a build needs nothing but Go.
// Regenerate with `go generate ./cmd/onefold` (TestSysoInSync fails when the
// committed file is stale).
package winres

import (
	"bytes"
	"image/png"
)

// IconID is the resource ID of the application icon group: Explorer shows the
// first group, the GUI window loads it by this ID.
const IconID = 1

// SysoName is the file the generator writes in cmd/onefold (the suffix limits
// it to windows/amd64 builds, the only Windows target of the Makefile).
const SysoName = "rsrc_windows_amd64.syso"

// iconSizes are the icon images in the group, in pixels.
var iconSizes = []int{16, 20, 24, 32, 40, 48, 64, 256}

// Manifest is the application manifest: per-monitor (v2) DPI awareness, so
// the WebView2 window renders sharp at any scaling, long path support, run
// as the invoking user, Windows 10/11 behaviour. consoleAllocationPolicy
// "detached" (Windows 11 24H2 and later; older versions ignore it): started
// from Explorer, onefold gets no console, so no terminal window flashes
// before the GUI opens; started from a terminal it still shares that
// terminal's console and the shell waits for it as before.
const Manifest = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
    </application>
  </compatibility>
  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true/pm</dpiAware>
      <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">PerMonitorV2, PerMonitor</dpiAwareness>
      <longPathAware xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">true</longPathAware>
      <consoleAllocationPolicy xmlns="http://schemas.microsoft.com/SMI/2024/WindowsSettings">detached</consoleAllocationPolicy>
    </windowsSettings>
  </application>
  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security>
      <requestedPrivileges>
        <requestedExecutionLevel level="asInvoker" uiAccess="false"/>
      </requestedPrivileges>
    </security>
  </trustInfo>
</assembly>
`

// Build returns the .syso bytes of the committed file (no version resource:
// a development build has no version): deterministic, so the committed file
// can be compared with a fresh build.
func Build() ([]byte, error) { return BuildVersion("") }

// BuildVersion is Build with a VS_VERSIONINFO for version ("" = none): the
// release build writes it for the tag being built, then restores the
// committed file.
func BuildVersion(version string) ([]byte, error) {
	var icons [][]byte
	for _, s := range iconSizes {
		var b bytes.Buffer
		if err := png.Encode(&b, Logo(s)); err != nil {
			return nil, err
		}
		icons = append(icons, b.Bytes())
	}
	var vi []byte
	if version != "" {
		vi = versionInfo(version)
	}
	return syso(icons, iconSizes, []byte(Manifest), vi), nil
}

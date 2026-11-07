// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package kinds is onefold' single source of file-type knowledge: every fact
// about an extension (pipeline kind, category, layout group, parser format,
// canonical spelling, flags, description) comes from the embedded registry
// types.tsv, plus the enumeration skip rules (dirs, junk names).
//
// Extensions are always handled lowercase and WITHOUT the leading dot.
package kinds

import (
	_ "embed"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// Kinds = extraction pipelines. photo and video are the "media" kinds with the
// full canonical pipeline (tag-identity dedup, silvering); every other kind is
// "generic" (exact-hash dedup, byte copy).
const (
	Photo    = "photo"
	Video    = "video"
	Image    = "image"
	Audio    = "audio"
	Document = "document"
	Text     = "text"
	Archive  = "archive"
	Other    = "other"
)

// Type is one registry row.
type Type struct {
	Ext         string
	Kind        string
	Category    string // fine taxonomy (raster_image, word_processing, source_code, ...)
	Group       string // layout family (IMAGE, DOC, XLS, PDF, CODE, ARCHIVE, ...)
	Format      string // parser / container id: dispatch + magic family ("none" = no parser)
	Canonical   string // harmonized spelling (jpeg -> jpg)
	System      bool   // OS / application artifact
	Common      bool   // frequent file type
	Personal    bool   // typical personal file
	Transient   bool   // incomplete download, temp or lock file
	Shortcut    bool   // link to another file or site
	Sidecar     bool   // belongs to the photo or video of the same name (edits, metadata, thumbnail)
	Takeout     bool   // Google Takeout metadata: <name>.json next to the file
	Live        bool   // a half of a Live Photo: the still or its short video
	Description string
}

//go:embed types.tsv
var typesTSV string

var registry = func() map[string]Type {
	m := map[string]Type{}
	for n, line := range strings.Split(typesTSV, "\n") {
		if line == "" || strings.HasPrefix(line, "# ") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 8 {
			panic(fmt.Sprintf("kinds: types.tsv line %d: %d columns", n+1, len(f)))
		}
		t := Type{Ext: f[0], Kind: f[1], Category: f[2], Group: f[3], Format: f[4], Canonical: f[5], Description: f[7]}
		for _, fl := range strings.Split(f[6], ",") {
			switch fl {
			case "system":
				t.System = true
			case "common":
				t.Common = true
			case "personal":
				t.Personal = true
			case "transient":
				t.Transient = true
			case "shortcut":
				t.Shortcut = true
			case "sidecar":
				t.Sidecar = true
			case "takeout":
				t.Takeout = true
			case "live":
				t.Live = true
			}
		}
		if t.Description == "-" {
			t.Description = ""
		}
		m[t.Ext] = t
	}
	return m
}()

// Ext returns the lowercase extension of path without the dot ("" if none).
func Ext(path string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
}

// Lookup returns the registry row of ext (any case).
func Lookup(ext string) (Type, bool) {
	t, ok := registry[strings.ToLower(ext)]
	return t, ok
}

// KindOf maps an extension to its pipeline kind; unknown -> Other.
func KindOf(ext string) string {
	if t, ok := Lookup(ext); ok {
		return t.Kind
	}
	return Other
}

// IsMedia reports whether kind uses the media pipeline (photo/video).
func IsMedia(kind string) bool { return kind == Photo || kind == Video }

// IsRAW reports whether ext is a camera RAW format (group RAW).
func IsRAW(ext string) bool { return Group(ext) == "RAW" }

// Category returns the fine category of ext ("" if unknown).
func Category(ext string) string { t, _ := Lookup(ext); return t.Category }

// Group returns the layout family of ext ("" if unknown).
func Group(ext string) string { t, _ := Lookup(ext); return t.Group }

// Format returns the parser/container id of ext ("none" if unknown).
func Format(ext string) string {
	if t, ok := Lookup(ext); ok {
		return t.Format
	}
	return "none"
}

// NormalizeExt lowercases ext and folds spelling variants to the canonical one.
func NormalizeExt(ext string) string {
	e := strings.ToLower(ext)
	if t, ok := registry[e]; ok && t.Canonical != "" {
		return t.Canonical
	}
	return e
}

// Known reports whether ext is in the registry.
func Known(ext string) bool { _, ok := Lookup(ext); return ok }

// IsSystemExt reports whether ext is an OS/application artifact.
func IsSystemExt(ext string) bool { t, _ := Lookup(ext); return t.System }

// IsShortcut reports whether ext is a link to another file or site.
func IsShortcut(ext string) bool { t, _ := Lookup(ext); return t.Shortcut }

// IsTransient reports whether a file is an incomplete download, a temp or a
// lock file: by extension, or an Office owner file ("~$name.docx").
func IsTransient(base string) bool {
	if strings.HasPrefix(base, "~$") {
		return true
	}
	t, _ := Lookup(Ext(base))
	return t.Transient
}

// Categories lists every category, sorted.
func Categories() []string {
	set := map[string]bool{}
	for _, t := range registry {
		set[t.Category] = true
	}
	return sortedKeys(set)
}

// Kinds lists every kind (extraction pipeline), sorted.
func Kinds() []string {
	return sortedKeys(map[string]bool{Photo: true, Video: true, Image: true, Audio: true, Document: true, Text: true, Archive: true, Other: true})
}

// Groups lists every group (layout family, uppercase), sorted.
func Groups() []string {
	set := map[string]bool{}
	for _, t := range registry {
		set[t.Group] = true
	}
	return sortedKeys(set)
}

// CategoryExts returns every extension belonging to one of cats.
func CategoryExts(cats []string) (map[string]bool, error) {
	valid := map[string]bool{}
	for _, c := range Categories() {
		valid[c] = true
	}
	want := map[string]bool{}
	for _, c := range cats {
		c = strings.ToLower(strings.TrimSpace(c))
		if c == "" {
			continue
		}
		if !valid[c] {
			return nil, fmt.Errorf("unknown category %q (valid: %s)", c, strings.Join(Categories(), ", "))
		}
		want[c] = true
	}
	return selectExts(func(t Type) bool { return want[t.Category] }), nil
}

func selectExts(f func(Type) bool) map[string]bool {
	out := map[string]bool{}
	for e, t := range registry {
		if f(t) {
			out[e] = true
		}
	}
	return out
}

// GoldenDocumentExts are the (harmonized) extensions the golden tree files
// under DOCUMENTS/<group>/; pinned because golden names must never move.
var GoldenDocumentExts = []string{"pdf", "doc", "docx", "odt", "rtf", "xls", "xlsx", "ods", "csv", "ppt", "pptx", "odp", "txt", "md", "epub"}

// presets are catalog extension whitelists derived from the registry.
var presets = map[string]struct {
	doc string
	sel func(Type) bool
}{
	"media":    {"photos (jpg, heic, png, webp, tiff, camera RAW...) and videos: the golden scope (default)", func(t Type) bool { return IsMedia(t.Kind) }},
	"all":      {"every file (no whitelist)", nil},
	"common":   {"frequent file types", func(t Type) bool { return t.Common }},
	"personal": {"typical personal files (documents, media, archives, keys)", func(t Type) bool { return t.Personal }},
	"image":    {"photos and images (raster, vector, camera RAW)", func(t Type) bool { return t.Kind == Photo || t.Kind == Image }},
	"audio":    {"audio", func(t Type) bool { return t.Kind == Audio }},
	"video":    {"video", func(t Type) bool { return t.Kind == Video }},
	"document": {"documents, spreadsheets, presentations, e-books, e-mail, plain text", func(t Type) bool {
		return t.Kind == Document || (t.Kind == Text && (t.Group == "TXT" || t.Group == "XLS" || t.Group == "EMAIL"))
	}},
	"archive": {"archives and disc images", func(t Type) bool { return t.Kind == Archive || t.Group == "DISK" }},
	"code":    {"source code, scripts, web", func(t Type) bool { return t.Group == "CODE" || t.Group == "WEB" }},
	"visual": {"frequent photos, raster images and videos", func(t Type) bool {
		return t.Common && (t.Kind == Video || ((t.Kind == Photo || t.Kind == Image) && t.Group == "IMAGE"))
	}},
}

// PresetNames lists preset names, sorted.
func PresetNames() []string {
	set := map[string]bool{}
	for k := range presets {
		set[k] = true
	}
	return sortedKeys(set)
}

// PresetDoc describes a preset.
func PresetDoc(name string) string { return presets[name].doc }

// PresetExts resolves a union of presets. all=true means no whitelist.
func PresetExts(names []string) (exts map[string]bool, all bool, err error) {
	exts = map[string]bool{}
	for _, n := range names {
		n = strings.ToLower(strings.TrimSpace(n))
		if n == "" {
			continue
		}
		p, ok := presets[n]
		if !ok {
			return nil, false, fmt.Errorf("unknown preset %q (valid: %s)", n, strings.Join(PresetNames(), ", "))
		}
		switch n {
		case "all":
			all = true
		default:
			for e := range selectExts(p.sel) {
				exts[e] = true
			}
		}
	}
	return exts, all, nil
}

// Enumeration skip rules. Dir names match a whole path component,
// case-insensitively.
var (
	SystemDirs = []string{"$Recycle.Bin", "AppData", "htdocs", "Microsoft", "System Volume Information", "Package Cache",
		"Program Files (x86)", "Program Files", "ProgramData", "Windows", "Windows.old", "Windows10Upgrade", "WindowsApps",
		"WindowsPowerShell", "WindowsUpdate", "Steam", "SteamLibrary", "SteamApps"}
	CloudDirs = []string{"Dropbox", "Google Drive", "GDrive", "OneDrive", "iCloud Drive"}
	DevDirs   = []string{"vendor", "node_modules", "tmp", "tests"}
	// JunkNames are OS/tool droppings, never content (basename or dir
	// component). The macOS ones also cover what Finder and Spotlight leave on
	// external drives; AppleDouble "._name" files and Finder's "Icon\r" are
	// matched by IsJunk too.
	JunkNames = []string{"__MACOSX", ".DS_Store", ".localized", "Thumbs.db", "desktop.ini",
		".Spotlight-V100", ".Trashes", ".fseventsd", ".DocumentRevisions-V100", ".TemporaryItems", ".apdisk", ".VolumeIcon.icns"}
	// ArchiveJunkDirs are archive member dir components that never date an
	// archive (VCS metadata, caches, vendored dependencies).
	ArchiveJunkDirs = []string{".git", ".cache", "vendor", "node_modules", "composer", "tmp"}
)

// IsArchiveJunk reports whether an archive member path is junk (a JunkNames or
// ArchiveJunkDirs component).
func IsArchiveJunk(member string) bool {
	for _, c := range strings.Split(strings.ReplaceAll(member, "\\", "/"), "/") {
		if IsJunk(c) {
			return true
		}
		for _, j := range ArchiveJunkDirs {
			if strings.EqualFold(c, j) {
				return true
			}
		}
	}
	return false
}

// IsJunk reports whether a basename is a junk file/dir.
func IsJunk(base string) bool {
	if strings.HasPrefix(base, "._") || base == "Icon\r" {
		return true
	}
	for _, j := range JunkNames {
		if strings.EqualFold(base, j) {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Package markers of a file name: what makes a folder look like a
// driver package (an .inf with driver binaries) or an installer (a setup
// program, a Windows Installer package, a redistributable).
const (
	PkgDriverINF = 1 << iota
	PkgDriverBin
	PkgInstaller
)

var (
	pkgInstallerNames    = map[string]bool{"setup.exe": true, "install.exe": true, "installer.exe": true, "autorun.inf": true}
	pkgInstallerExts     = map[string]bool{"msi": true, "msp": true, "msu": true}
	pkgRedistPrefixes    = []string{"vcredist", "vc_redist", "dotnetfx", "ndp4", "directx", "dxsetup", "windowsdesktop-runtime"}
	pkgDriverBinaryExts  = map[string]bool{"sys": true, "cat": true, "dll": true}
	pkgDriverManifestExt = "inf"
)

// PackageMarkers returns the package markers of one file name.
func PackageMarkers(base string) int {
	low := strings.ToLower(base)
	ext := Ext(low)
	m := 0
	switch {
	case pkgInstallerNames[low]:
		m |= PkgInstaller
	case ext == pkgDriverManifestExt:
		m |= PkgDriverINF
	case pkgDriverBinaryExts[ext]:
		m |= PkgDriverBin
	case pkgInstallerExts[ext]:
		m |= PkgInstaller
	}
	if ext == "exe" {
		for _, p := range pkgRedistPrefixes {
			if strings.HasPrefix(low, p) {
				m |= PkgInstaller
			}
		}
	}
	return m
}

// PackageKind classifies a folder from the markers of its files and their
// number: "driver" (.inf + driver binaries), "installer" (a setup program
// or package next to other files), or "".
func PackageKind(markers, files int) string {
	switch {
	case markers&PkgDriverINF != 0 && markers&PkgDriverBin != 0:
		return "driver"
	case markers&PkgInstaller != 0 && files > 1:
		return "installer"
	}
	return ""
}

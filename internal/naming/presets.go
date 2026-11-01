// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package naming

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/kinds"
)

// Preset is a named naming spec ("selector[,selector]=template;...;*=template").
type Preset struct {
	Spec string
	Doc  string
}

// CleanSuffix appended to any preset name swaps {stem-norm} / {stem} for
// {stem-clean} (the "[origin] " name prefix is dropped too).
const CleanSuffix = "-clean"

// Shared fragments.
const (
	extYear  = `{EXT|"NOEXT"}/{year|"UNDATED"}/{stem-norm}<.{ext}>`
	normExt  = `{EXT-NORM|"NOEXT"}`
	normName = `{stem-norm}<.{ext-norm}>`
	normYear = normExt + `/{year|"UNDATED"}/` + normName
	// golden tree (library*): frozen like the media naming, raw stem, undated
	// year 0000, so `apply` stays idempotent across runs
	goldenYear = normExt + `/{year|"0000"}/{stem}<.{ext-norm}>`
	eraYear    = normExt + `/<{pre-2000}/>{year|"UNDATED"}`
	monthName  = `<{month} - {month-name}>`
	docGroups  = "PDF,DOC,XLS,PPT,TXT,EBOOK,EMAIL,CONTACT"
)

// goldenDocSel selects the pinned golden document extensions.
var goldenDocSel = "." + strings.Join(kinds.GoldenDocumentExts, ",.")

// layout is a family-specific template applied to some selectors; the other
// files fall back to <EXT>/<YEAR>/<name> (or FILES/... in the library tree).
type layout struct {
	sel, tpl, root, doc string
	libSel              string // selectors of the library variant ("" = sel)
}

var layouts = map[string]layout{
	"date-tree": {"*", `<{year}/{month} - {month-name}/{day}|undated>/<{type-prefix}_{time}_{stem-norm}|{stem-norm}><.{ext}>`,
		"TIMELINE", "<YEAR>/<MM - Month>/<DD>/<TYPE>_<HHMMSS>_<name>; undated -> undated/<name>", ""},
	"documents": {docGroups, `{group}/{year|"UNDATED"}/{author|"Unknown"}/{title|stem-norm}<.{ext}>`,
		"DOCUMENTS", "documents (" + docGroups + "; library: pinned golden extensions): <GROUP>/<YEAR>/<Author>/<Title>", goldenDocSel},
	"music": {"audio", `{artist|"Unknown Artist"}/<{album-year} - >{album|"Unknown Album"}/<{track-num}. {title|stem-norm}|{title|stem-norm}><.{ext}>`,
		"MUSIC", "audio: <Artist>/<Year - Album>/<NN. Title>", ""},
	"devices": {"photo,image", `{device-category|"Unknown"}/{device|"Unknown"}/{year|"UNDATED"}/{stem-norm}<.{ext}>`,
		"DEVICES", "photos and images: <Category>/<Device>/<YEAR>/<name>", ""},
}

// Presets are named by the layout they produce. Legacy collision markers are
// never reproduced: collisions always get the onefold _NN.
var Presets = func() map[string]Preset {
	p := map[string]Preset{
		"ext-year": {`*=` + extYear, "<EXT>/<YEAR>/<name> (merge and rearrange default)"},
		"ext-flat": {`*=` + normExt + `/` + normName, "<EXT>/<name>"},
		"ext-size": {`*=` + normExt + `/{size-bucket}/` + normName, "<EXT>/<size bucket>/<name>"},
		"ext-era-year": {`*=` + eraYear + `/` + normName,
			"<EXT>/[_FORMER]/<YEAR>/<name> (_FORMER before 2000)"},
		"ext-era-year-month": {`*=` + eraYear + `/` + monthName + `/` + normName,
			"<EXT>/[_FORMER]/<YEAR>/<MM - Mese>/<name>"},
		"ext-era-year-size-month": {`*=` + eraYear + `/{size-bucket}/` + monthName + `/` + normName,
			"<EXT>/[_FORMER]/<YEAR>/<size bucket>/<MM - Mese>/<name>"},
		"ext-era-year-origin": {`*=` + eraYear + `/<[{origin}] >{stem-clean}<.{ext-norm}>`,
			"<EXT>/[_FORMER]/<YEAR>/[<origin folder>] <name>"},
		"ext-era-year-origin-mtime": {`*=` + eraYear + `/<[{origin}] ><({mtime-datetime}) >{stem-clean}<.{ext-norm}>`,
			"<EXT>/[_FORMER]/<YEAR>/[<origin folder>] (<mtime>) <name>"},
		"photo-month": {`photo,image=<{EXT-NORM}/_PHOTOS/{year}/{month} - {month-name}/IMG_{datetime}.{ext-norm}|` + eraYear + `/` + normName + `>;*=` + eraYear + `/` + normName,
			"photos: <EXT>/_PHOTOS/<YEAR>/<MM - Mese>/IMG_<datetime>; rest <EXT>/[_FORMER]/<YEAR>/<name>"},
		"photo-timestamp": {`photo,image={EXT-NORM}/<{year}/{photo-prefix}_{datetime}.{ext-norm}|Uncategorized/` + normName + `>;*=` + extYear,
			"photos: <EXT>/<YEAR>/<PFX|PIX>_<datetime>, undated <EXT>/Uncategorized/<name>; rest <EXT>/<YEAR>/<name>"},
		"video-tier": {`video={year|"UNDATED"}/{tier|"SD"}/<{device-slug|"Unknown"}_{date}_{time}.{ext}|{stem-norm}<.{ext}>>;*=` + extYear,
			"videos: <YEAR>/<tier>/<Device>_<date>_<time>; rest <EXT>/<YEAR>/<name>"},
		"library": {`photo=PICTURES/{EXT-NORM}/{year|"0000"}/<{prefix}_{datetime}.{ext-norm}|{stem}<.{ext-norm}>>;` +
			`video=VIDEOS/{year|"0000"}/<VID_{datetime}.{ext-norm}|{stem}<.{ext-norm}>>;*=FILES/` + goldenYear,
			"PICTURES/<EXT>/<YEAR>/<PFX>_<datetime>, VIDEOS/<YEAR>/VID_<datetime>, FILES/<EXT>/<YEAR>/<name>"},
		"library-files": {`*=FILES/` + goldenYear, "FILES/<EXT>/<YEAR or 0000>/<name> (plan default)"},
	}
	for name, l := range layouts {
		fallback := ";*=" + extYear
		libFallback := ";*=FILES/" + goldenYear
		if l.sel == "*" {
			fallback, libFallback = "", ""
		}
		lib := strings.NewReplacer("<.{ext}>", "<.{ext-norm}>", "stem-norm", "stem", `"UNDATED"`, `"0000"`).Replace(l.tpl)
		p[name] = Preset{l.sel + "=" + l.tpl + fallback, l.doc + suffixDoc(l.sel, "<EXT>/<YEAR>/<name>")}
		libSel := l.sel
		if l.libSel != "" {
			libSel = l.libSel
		}
		p["library-"+name] = Preset{libSel + "=" + l.root + "/" + lib + libFallback, l.root + "/" + l.doc + suffixDoc(l.sel, "FILES/<EXT>/<YEAR>/<name>")}
	}
	return p
}()

func suffixDoc(sel, rest string) string {
	if sel == "*" {
		return ""
	}
	return "; rest " + rest
}

// LayoutAliases maps the historical -layout values to presets.
var LayoutAliases = map[string]string{
	"":         "ext-year",
	"dedupe":   "library-files",
	"timeline": "library-date-tree",
	"document": "library-documents",
	"music":    "library-music",
	"device":   "library-devices",
}

// LayoutName maps a -layout value through the historical aliases (dedupe,
// timeline, document, music, device -> library-*). Only -layout takes them:
// a -naming value is a preset name or a template (Resolve).
func LayoutName(l string) string {
	if a, ok := LayoutAliases[strings.TrimSpace(l)]; ok {
		return a
	}
	return l
}

// GoldenTree reports whether a resolved naming spec is a golden tree preset
// (library, library-*): the plan's naming of generic files.
func GoldenTree(spec string) bool {
	s := strings.TrimSuffix(strings.TrimSpace(spec), CleanSuffix)
	return s == "library" || strings.HasPrefix(s, "library-")
}

// CheckTemplate refuses the golden tree presets in merge and rearrange.
func CheckTemplate(spec string) error {
	if GoldenTree(spec) {
		return fmt.Errorf("%w: %s is the golden archive tree; merge and rearrange use the other presets (onefold naming)", catalog.ErrMixed, strings.TrimSpace(spec))
	}
	return nil
}

// PresetNames lists preset names, sorted.
func PresetNames() []string {
	out := make([]string, 0, len(Presets))
	for n := range Presets {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

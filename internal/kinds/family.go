// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package kinds

// Families group the categories of types.tsv into the few headings a person
// reads in a chart ("photos", "documents"...). They are derived from the
// category of each extension; types.tsv stays the only per-extension source.
const (
	FamPhotos    = "photos"
	FamVideos    = "videos"
	FamAudio     = "audio"
	FamDocuments = "documents"
	FamArchives  = "archives"
	FamCode      = "code"
	FamApps      = "apps"
	FamDesign    = "design"
	FamGames     = "games"
	FamData      = "data"
	FamOther     = "other"
)

// Families lists every family, in display order (other last).
var Families = []string{FamPhotos, FamVideos, FamAudio, FamDocuments, FamArchives, FamCode, FamApps, FamDesign,
	FamGames, FamData, FamOther}

// categoriesOf lists the categories of each family.
var categoriesOf = map[string][]string{
	FamPhotos:    {"raster_image", "camera_raw"},
	FamVideos:    {"video", "subtitle"},
	FamAudio:     {"audio", "audio_project", "playlist"},
	FamDocuments: {"word_processing", "spreadsheet", "presentation", "page_layout", "ebook", "text", "email", "personal_info"},
	FamArchives:  {"compressed", "disk_image", "backup", "encoded"},
	FamCode:      {"developer", "source_code", "web"},
	FamApps:      {"executable", "system", "settings", "plugin", "font", "certificate"},
	FamDesign:    {"vector_image", "3d_image", "cad", "gis", "image_editing"},
	FamGames:     {"game"},
	FamData:      {"data", "database"},
	FamOther:     {"misc"},
}

var familyOfCategory = func() map[string]string {
	m := map[string]string{}
	for f, cats := range categoriesOf {
		for _, c := range cats {
			m[c] = f
		}
	}
	return m
}()

// FamilyOfCategory maps a category to its family (unknown: other).
func FamilyOfCategory(cat string) string {
	if f, ok := familyOfCategory[cat]; ok {
		return f
	}
	return FamOther
}

// Family is the family of an extension (unknown or none: other).
func Family(ext string) string {
	t, ok := Lookup(ext)
	if !ok {
		return FamOther
	}
	return FamilyOfCategory(t.Category)
}

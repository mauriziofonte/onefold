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

var familyOfCategory = map[string]string{
	"raster_image": FamPhotos, "camera_raw": FamPhotos,
	"video": FamVideos, "subtitle": FamVideos,
	"audio": FamAudio, "audio_project": FamAudio, "playlist": FamAudio,
	"word_processing": FamDocuments, "spreadsheet": FamDocuments, "presentation": FamDocuments,
	"page_layout": FamDocuments, "ebook": FamDocuments, "text": FamDocuments, "email": FamDocuments,
	"personal_info": FamDocuments,
	"compressed": FamArchives, "disk_image": FamArchives, "backup": FamArchives, "encoded": FamArchives,
	"developer": FamCode, "source_code": FamCode, "web": FamCode,
	"executable": FamApps, "system": FamApps, "settings": FamApps, "plugin": FamApps, "font": FamApps,
	"certificate": FamApps,
	"vector_image": FamDesign, "3d_image": FamDesign, "cad": FamDesign, "gis": FamDesign, "image_editing": FamDesign,
	"game": FamGames,
	"data": FamData, "database": FamData,
	"misc": FamOther,
}

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

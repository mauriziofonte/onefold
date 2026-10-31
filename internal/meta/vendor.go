// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import (
	"regexp"
	"strings"
)

// VSignals are the raw ffprobe fields the vendor cascade reads (15-rule
// cascade + empty default). Field origins:
//   - format.tags: AndroidMfr/Model/Ver, CompatBrands, Comment, EncoderFmt, MajorBrand, Firmware
//   - video stream tags: StreamEncoder, HandlerName, VendorID
type VSignals struct {
	AndroidMfr    string
	AndroidModel  string
	AndroidVer    string
	CompatBrands  string
	Comment       string
	EncoderFmt    string
	MajorBrand    string
	Firmware      string
	StreamEncoder string
	HandlerName   string
	VendorID      string
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

// slugify: lowercase, non-alnum -> '-', collapse repeats, trim. (slugify_vendor)
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nonAlnum.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

func ci(hay, needle string) bool {
	return strings.Contains(strings.ToLower(hay), strings.ToLower(needle))
}

// VendorSlug returns the device slug, or "" if undetected. The rule ORDER is the
// contract (the toolkit's vendor contract test asserts it): do not reorder.
func VendorSlug(s VSignals) string {
	// 1. Android phones: manufacturer + model, stripping a redundant model prefix
	//    ("OnePlus" + "OnePlus A3003" -> "oneplus-a3003").
	if s.AndroidMfr != "" && s.AndroidModel != "" {
		mfr, mdl := strings.TrimSpace(s.AndroidMfr), strings.TrimSpace(s.AndroidModel)
		if strings.HasPrefix(strings.ToLower(mdl), strings.ToLower(mfr)) {
			return slugify(mdl)
		}
		return slugify(mfr + " " + mdl)
	}
	// 2. generic Android (version only)
	if s.AndroidVer != "" {
		return "android"
	}
	// 3-5. GoPro
	if ci(s.StreamEncoder, "GoPro") {
		return "gopro"
	}
	if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(s.Firmware)), "H2") {
		return "gopro"
	}
	if ci(s.HandlerName, "GoPro") {
		return "gopro"
	}
	// 6-8. Nikon
	if ci(s.Comment, "NIKON") {
		return "nikon"
	}
	if strings.EqualFold(strings.TrimSpace(s.VendorID), "niko") {
		return "nikon"
	}
	if ci(s.CompatBrands, "niko") {
		return "nikon"
	}
	// 9-10. Kodak
	if ci(s.Comment, "KODAK") {
		return "kodak"
	}
	if strings.EqualFold(strings.TrimSpace(s.VendorID), "kdak") {
		return "kodak"
	}
	// 11. Canon
	if ci(s.CompatBrands, "CAEP") {
		return "canon"
	}
	// 12. Panasonic
	if ci(s.CompatBrands, "pana") {
		return "panasonic"
	}
	// 13. Apple
	if ci(s.HandlerName, "Apple") || ci(s.HandlerName, "Core Media") {
		return "apple"
	}
	// 14-15. Google
	if ci(s.HandlerName, "produced by Google") {
		return "google"
	}
	if strings.EqualFold(strings.TrimSpace(s.EncoderFmt), "Google") {
		return "google"
	}
	return ""
}

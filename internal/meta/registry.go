// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import "strings"

// Device canonicalization — built from an exhaustive census of the real corpus
// (69k photos, 138 raw make/model, 243 software, 78 CreatorTool).
//
// The problem: one physical device appears under many raw strings (case, make
// duplicated in model, opaque model-codes, make/model swapped, EXIF-vs-XMP, and
// firmware build tags). This module folds them into ONE canonical device name.
//
// Two levels:
//   A. deviceRules — opaque model-codes / firmware build tags that need a lookup
//      to become a commercial name (LE2123 -> OnePlus 9 Pro, 00WW_* -> Nokia 7.2).
//   B. brandDevice — syntactic normalization (dedup brand, strip prefix) for
//      devices whose model is already legible (Nikon D3500, Canon EOS 1000D).
//
// A device NOT covered here is never "unrecognized": brandDevice normalizes it
// from make+model.
//
// Category/prefix precedence: enlargement & snapseed win over the device (strong derived artifacts);
// photoshop & editing lose to the device.

// DeviceRule maps distinctive signals (matched as lowercase substrings in the
// make+model+software+CreatorTool blob) to a canonical device. Ordered:
// distinctive codes first. Brand+Model empty => category-only (e.g. Google HDR+).
type DeviceRule struct {
	Any      []string
	Brand    string
	Model    string
	Category string // "smartphone" | "camera"
	Prefix   string // "IMG" | "DSC"
}

const (
	sm  = "smartphone"
	cam = "camera"
)

var deviceRules = []DeviceRule{
	// OnePlus (QCAM-AA/QCOM-AA = OnePlus camera app on the 9 Pro; A3003 = OnePlus 3, not 3T)
	{[]string{"le2123", "oneplus9pro", "qcam-aa", "qcom-aa", "oneplus 9 pro"}, "OnePlus", "9 Pro", sm, "IMG"},
	{[]string{"a3003", "oneplus3-user", "oneplus 3"}, "OnePlus", "3", sm, "IMG"},
	{[]string{"gm1913"}, "OnePlus", "7 Pro", sm, "IMG"},
	{[]string{"oneplus 15"}, "OnePlus", "15", sm, "IMG"},
	{[]string{"a0001", "oneplus one"}, "OnePlus", "One", sm, "IMG"},
	// Nokia / HMD Global (00WW_* = Android One build tag)
	{[]string{"nokia 7.2", "00ww_"}, "Nokia", "7.2", sm, "IMG"},
	// Samsung Galaxy (SM-/GT- codes + firmware prefixes)
	{[]string{"gt-i9000"}, "Samsung", "Galaxy S", sm, "IMG"},
	{[]string{"gt-i9100", "i9100xx"}, "Samsung", "Galaxy S II", sm, "IMG"},
	{[]string{"gt-i9300", "i9300xx"}, "Samsung", "Galaxy S III", sm, "IMG"},
	{[]string{"gt-i8160"}, "Samsung", "Galaxy Ace 2", sm, "IMG"},
	{[]string{"gt-s5570", "s5570mj", "s5570ai"}, "Samsung", "Galaxy Mini", sm, "IMG"},
	{[]string{"sm-a300fu", "a300fuxx"}, "Samsung", "Galaxy A3 (2015)", sm, "IMG"},
	{[]string{"sm-a336b", "a336bxx"}, "Samsung", "Galaxy A33 5G", sm, "IMG"},
	{[]string{"sm-g800f", "g800fxx"}, "Samsung", "Galaxy S5 Mini", sm, "IMG"},
	{[]string{"sm-g935f", "g935fxx"}, "Samsung", "Galaxy S7 Edge", sm, "IMG"},
	// Huawei
	{[]string{"was-lx1"}, "Huawei", "P10 Lite", sm, "IMG"},
	{[]string{"ale-l21"}, "Huawei", "P8 Lite (2015)", sm, "IMG"},
	{[]string{"lya-l29"}, "Huawei", "Mate 20 Pro", sm, "IMG"},
	{[]string{"fig-lx1"}, "Huawei", "P Smart (2018)", sm, "IMG"},
	{[]string{"eva-l09"}, "Huawei", "P9", sm, "IMG"},
	// Sony / Sony Ericsson smartphones
	{[]string{"c6903"}, "Sony", "Xperia Z1", sm, "IMG"},
	{[]string{"sk17i", "xperia mini pro"}, "Sony Ericsson", "Xperia Mini Pro", sm, "IMG"},
	{[]string{"k750i"}, "Sony Ericsson", "K750i", sm, "IMG"},
	// other smartphones / feature phones
	{[]string{"a510e", "wildfire s"}, "HTC", "Wildfire S", sm, "IMG"},
	{[]string{"gs290"}, "LG", "Cookie Fresh", sm, "IMG"},
	{[]string{"e770"}, "Motorola", "E770", sm, "IMG"},
	{[]string{"reno8 lite"}, "OPPO", "Reno8 Lite 5G", sm, "IMG"},
	// Sony Alpha / Cyber-shot (cameras)
	{[]string{"ilce-7m4"}, "Sony", "α7 IV", cam, "DSC"},
	{[]string{"ilce-7m3"}, "Sony", "α7 III", cam, "DSC"},
	{[]string{"dsc-w200"}, "Sony", "Cyber-shot W200", cam, "DSC"},
	{[]string{"dsc-w50"}, "Sony", "Cyber-shot W50", cam, "DSC"},
	{[]string{"dslr-a900"}, "Sony", "α900", cam, "DSC"},
	// Nikon Coolpix E-series + Z-series codes (cameras)
	{[]string{"e7600"}, "Nikon", "Coolpix 7600", cam, "DSC"},
	{[]string{"e4300"}, "Nikon", "Coolpix 4300", cam, "DSC"},
	{[]string{"e4100"}, "Nikon", "Coolpix 4100", cam, "DSC"},
	{[]string{"e775"}, "Nikon", "Coolpix 775", cam, "DSC"},
	{[]string{"coolpix p500"}, "Nikon", "Coolpix P500", cam, "DSC"},
	{[]string{"z 6_2", "z6_2"}, "Nikon", "Z6 II", cam, "DSC"},
	// Canon special naming (JP "Kiss Digital X" == EOS 400D)
	{[]string{"kiss digital x"}, "Canon", "EOS 400D", cam, "DSC"},
	// Google Camera (HDR+ build): category-only, device left indeterminate
	{[]string{"hdr+ 1.0"}, "", "", sm, "IMG"},
}

func (r DeviceRule) canon() string {
	if r.Brand == "" && r.Model == "" {
		return ""
	}
	return strings.TrimSpace(r.Brand + " " + r.Model)
}

// brandCanon maps a raw make to its canonical brand (folds case + corporate suffixes).
var brandCanon = map[string]string{
	"nikon corporation": "Nikon", "nikon": "Nikon",
	"canon":                 "Canon",
	"eastman kodak company": "Kodak", "kodak": "Kodak",
	"fujifilm":      "Fujifilm",
	"samsung":       "Samsung",
	"sony ericsson": "Sony Ericsson", "semc": "Sony Ericsson",
	"sony":       "Sony",
	"hmd global": "Nokia", "nokia": "Nokia",
	"huawei":             "Huawei",
	"oneplus":            "OnePlus",
	"google":             "Google",
	"apple":              "Apple",
	"panasonic":          "Panasonic",
	"pentax corporation": "Pentax", "pentax": "Pentax",
	"olympus imaging corp.": "Olympus", "olympus": "Olympus",
	"casio computer co.,ltd.": "Casio", "casio": "Casio",
	"motorola":       "Motorola",
	"htc":            "HTC",
	"lg electronics": "LG", "lge": "LG",
	"oppo":            "OPPO",
	"gopro":           "GoPro",
	"hewlett-packard": "HP", "hp": "HP",
	"vivitar": "Vivitar",
}

// cameraBrands classify a normalized brand as a camera (else smartphone/phone).
var cameraBrands = map[string]bool{
	"Nikon": true, "Canon": true, "Kodak": true, "Fujifilm": true, "Panasonic": true,
	"Pentax": true, "Olympus": true, "Casio": true, "GoPro": true, "HP": true, "Vivitar": true,
}

// normalizeDevice folds a raw (make, model) into "Brand Model", deduping the
// brand repeated inside the model and unifying case. "" if both empty.
func normalizeDevice(make, model string) string {
	mk := strings.TrimSpace(make)
	md := strings.TrimSpace(model)
	if mk == "" && md == "" {
		return ""
	}
	brand := brandCanon[strings.ToLower(mk)]
	if brand == "" {
		brand = titleFirst(mk)
	}
	// strip a redundant brand prefix from the model (e.g. "NIKON D3500" -> "D3500",
	// "Canon EOS 1000D" -> "EOS 1000D"), matching the RAW make word too.
	md = stripPrefixFold(md, "NIKON")
	for _, w := range strings.Fields(mk) {
		md = stripPrefixFold(md, w)
	}
	md = stripPrefixFold(md, brand)
	md = strings.TrimSpace(md)
	switch {
	case brand == "":
		return md
	case md == "":
		return brand
	case strings.HasPrefix(strings.ToLower(md), strings.ToLower(brand)):
		return md
	default:
		return brand + " " + md
	}
}

// brandDevice returns the device+category+prefix for a make/model whose brand is
// known (Nikon/Canon/… camera, Samsung/Apple/… phone). ok=false if unresolvable.
func brandDevice(make, model string) (dev, cat, prefix string, ok bool) {
	dev = normalizeDevice(make, model)
	if dev == "" {
		return "", "", "", false
	}
	brand := brandCanon[strings.ToLower(strings.TrimSpace(make))]
	if brand == "" {
		return "", "", "", false // unknown make -> let software/default decide
	}
	if cameraBrands[brand] {
		return dev, cam, "DSC", true
	}
	// Sony makes both: cameras carry ILCE/DSC-/DSLR/Cyber-shot in the model
	if brand == "Sony" {
		ml := strings.ToLower(model)
		if strings.Contains(ml, "ilce") || strings.Contains(ml, "dsc-") ||
			strings.Contains(ml, "dslr") || strings.Contains(ml, "cyber") {
			return dev, cam, "DSC", true
		}
	}
	return dev, sm, "IMG", true
}

// CanonicalDevice folds all device signals (EXIF make/model/software + XMP
// CreatorTool + firmware tags) into a canonical device, category and prefix.
//
// The device is resolved independently of the category: an edit (Snapseed on a
// Sony α7 IV) keeps the underlying commercial device, only the category/prefix
// switch to the edit. Category precedence: enlargement & snapseed
// beat the device; photoshop & editing lose to it. Never fails — an unknown
// device still returns a normalized name (or "") and IMG.
func CanonicalDevice(make, model, software, creator string) (device, category, prefix string) {
	blob := strings.ToLower(strings.Join([]string{make, model, software, creator}, " "))
	has := func(kws ...string) bool {
		for _, k := range kws {
			if strings.Contains(blob, k) {
				return true
			}
		}
		return false
	}
	dev, devCat, devPfx := resolveDevice(make, model, creator, blob, has)

	// strong derived artifacts override the category (device kept underneath)
	if has("gigapixel") {
		return dev, "enlargement", "ENL"
	}
	if has("snapseed") {
		return dev, "snapseed", "SNPSD"
	}
	// a recognized device wins over the weaker edit signals
	if devCat != "" {
		return dev, devCat, devPfx
	}
	// device-less: fall back to editing-software category
	if has("photoshop") {
		return dev, "photoshop", "PS"
	}
	if has("picasa", "photoscape", "paintshop", "gimp", "lightroom", "paint.net",
		"photoshelter", "canva", "viewnx", "topaz photo ai", "acd systems",
		"nero photosnap", "gthumb", "windows photo editor", "rawtherapee",
		"picsart", "instagram", "googlephotoscan") {
		return dev, "editing", "EDT"
	}
	return dev, "", "IMG"
}

// resolveDevice resolves the physical device name (+ its natural category/prefix)
// from all signals. devCat is "" only when the device is genuinely unidentifiable.
func resolveDevice(make, model, creator, blob string, has func(...string) bool) (dev, cat, prefix string) {
	// 1. opaque code / firmware -> commercial name
	for _, r := range deviceRules {
		if has(r.Any...) {
			d := r.canon()
			if d == "" { // category-only rule (HDR+): keep any normalized device
				d = normalizeDevice(make, model)
			}
			return d, r.Category, r.Prefix
		}
	}
	// 2. known brand from EXIF/XMP make+model
	if d, c, p, ok := brandDevice(make, model); ok {
		return d, c, p
	}
	// 3. device present ONLY in the CreatorTool (EXIF stripped), e.g. "NIKON D3500 Ver.1.00"
	if strings.TrimSpace(make) == "" && strings.TrimSpace(model) == "" {
		if d, c, p, ok := deviceFromCreator(creator); ok {
			return d, c, p
		}
	}
	// 4. device-less
	return normalizeDevice(make, model), "", ""
}

// deviceFromCreator recovers a device when make/model are empty but the XMP
// CreatorTool carries "<BRAND> <model> Ver.x" (e.g. "NIKON D3500 Ver.1.00").
// ok=false if the CreatorTool is software (Adobe…, HDR+…), not a device.
func deviceFromCreator(creator string) (dev, cat, prefix string, ok bool) {
	s := strings.TrimSpace(creator)
	if i := strings.Index(strings.ToLower(s), " ver"); i > 0 {
		s = s[:i]
	}
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return "", "", "", false
	}
	if brandCanon[strings.ToLower(fields[0])] == "" {
		return "", "", "", false // not "BRAND model" — leave to software/default
	}
	return brandDevice(fields[0], strings.TrimSpace(strings.TrimPrefix(s, fields[0])))
}

// titleFirst uppercases the first byte and leaves the rest untouched. Used for
// unknown makes.
func titleFirst(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// stripPrefixFold removes a leading word (case-insensitive) plus following spaces.
func stripPrefixFold(s, word string) string {
	if word == "" {
		return s
	}
	if len(s) >= len(word) && strings.EqualFold(s[:len(word)], word) {
		return strings.TrimLeft(s[len(word):], " ")
	}
	return s
}

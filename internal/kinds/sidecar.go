// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package kinds

import (
	"strings"
	"unicode/utf8"
)

// Sidecar name rules. A sidecar sits in the folder of its owner (a photo or a
// video) and its name extends the owner's:
//
//	IMG_1234.AAE, IMG_1234.xmp, MVI_1234.THM   <stem>.<ext>, flag sidecar
//	IMG_1234.CR2.xmp                           <name>.<ext>, flag sidecar
//	IMG_1234.HEIC.json                         <name>.json, flag takeout
//	IMG_1234.HEIC.supplemental-metadata.json   newer Google Takeout, also cut short
//	IMG_1234.jpg(1).json                       the Takeout file of IMG_1234(1).jpg
//	                                           (also .supplemental-metadata(1).json)
//	IMG_1234.MOV next to IMG_1234.HEIC         a Live Photo, flag live on both
//
// SidecarRest returns the part of other's name that the renamed sidecar keeps:
// after the owner's stem (byName false: newStem + rest) or after its whole
// name (byName true: newName + rest).
func SidecarRest(owner, other string) (rest string, byName bool, ok bool) {
	ot, _ := Lookup(Ext(other))
	if !ot.Sidecar && !ot.Takeout && !ot.Live {
		return "", false, false
	}
	own, _ := Lookup(Ext(owner))
	if !IsMedia(own.Kind) {
		return "", false, false
	}
	oStem := trimExt(owner)
	if ot.Live {
		// the video half of the still: same stem, a photo owner
		if own.Kind != Photo || !own.Live || KindOf(Ext(other)) != Video || !strings.EqualFold(trimExt(other), oStem) {
			return "", false, false
		}
		return other[len(trimExt(other)):], false, true
	}
	if hasPrefixFold(other, owner) && len(other) > len(owner) && other[len(owner)] == '.' {
		r := other[len(owner):] // ".xmp", ".json", ".supplemental-metadata.json"
		if ot.Sidecar && strings.Count(r, ".") == 1 {
			return r, true, true
		}
		if ot.Takeout && (strings.Count(r, ".") == 1 || takeoutCut(r[1:len(r)-len(".json")])) {
			return r, true, true
		}
		return "", false, false
	}
	if ot.Sidecar && strings.EqualFold(trimExt(other), oStem) {
		return other[len(trimExt(other)):], false, true
	}
	if !ot.Takeout {
		return "", false, false
	}
	base := trimExt(other) // without ".json"
	// IMG_1234(1).jpg -> IMG_1234.jpg(1).json
	if i := strings.LastIndexByte(oStem, '('); i > 0 && strings.HasSuffix(oStem, ")") {
		name, n := oStem[:i]+owner[len(oStem):], oStem[i:]
		if strings.EqualFold(base, name+n) || strings.EqualFold(base, name+".supplemental-metadata"+n) {
			return ".json", true, true
		}
	}
	// a name cut to 47 or 51 characters: the start of the owner's name and
	// its ".supplemental-metadata", at least the whole stem
	if n := utf8.RuneCountInString(other); (n == 47 || n == 51) && len(base) >= len(oStem) {
		if full := owner + ".supplemental-metadata"; hasPrefixFold(full, base) {
			return ".json", true, true
		}
	}
	return "", false, false
}

// takeoutCut: s is "supplemental-metadata" or the start of it.
func takeoutCut(s string) bool {
	return len(s) > 0 && strings.HasPrefix("supplemental-metadata", strings.ToLower(s))
}

// TakeoutEdited returns the name of the original of a Google Takeout edited
// copy ("IMG_1234-edited.jpg" -> "IMG_1234.jpg"), in the languages Takeout
// writes the suffix in; "" for any other name.
func TakeoutEdited(base string) string {
	stem := trimExt(base)
	for _, suf := range takeoutEditedSuffixes {
		if len(stem) > len(suf) && strings.EqualFold(stem[len(stem)-len(suf):], suf) {
			return stem[:len(stem)-len(suf)] + base[len(stem):]
		}
	}
	return ""
}

var takeoutEditedSuffixes = []string{"-edited", "-bearbeitet", "-modifié", "-editado", "-modificato", "-bewerkt", "-redigeret", "-muokattu", "-redigert", "-edytowane", "-upraveno"}

func trimExt(base string) string {
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		return base[:i]
	}
	return base
}

func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

// IsSidecarExt reports whether files of ext can belong to a photo or video
// of the same name (flags sidecar and takeout).
func IsSidecarExt(ext string) bool { t, _ := Lookup(ext); return t.Sidecar || t.Takeout }

// IsLive reports whether ext can be a half of a Live Photo.
func IsLive(ext string) bool { t, _ := Lookup(ext); return t.Live }

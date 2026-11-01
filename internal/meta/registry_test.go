// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import "testing"

func TestCanonicalDevice(t *testing.T) {
	cases := []struct {
		name                           string
		make, model, software, creator string
		dev, cat, prefix               string
	}{
		// OnePlus 9 Pro — the six permutations must all collapse to one device
		{"9pro exif", "OnePlus", "LE2123 (OnePlus9Pro)", "", "", "OnePlus 9 Pro", "smartphone", "IMG"},
		{"9pro bare", "OnePlus", "LE2123", "", "", "OnePlus 9 Pro", "smartphone", "IMG"},
		{"9pro swapped", "LE2123 (OnePlus9Pro)", "OnePlus", "", "", "OnePlus 9 Pro", "smartphone", "IMG"},
		{"9pro commercial", "OnePlus", "OnePlus 9 Pro", "", "", "OnePlus 9 Pro", "smartphone", "IMG"},
		{"9pro qcam app", "OnePlus", "QCAM-AA", "", "", "OnePlus 9 Pro", "smartphone", "IMG"},
		// OnePlus 3 (A3003, not 3T) — confirmed by firmware
		{"oneplus3", "OnePlus", "ONEPLUS A3003", "OnePlus3-user 8.0.0", "", "OnePlus 3", "smartphone", "IMG"},
		{"oneplus15", "OnePlus", "OnePlus 15", "", "", "OnePlus 15", "smartphone", "IMG"},
		// Nokia 7.2 — from make/model AND from the 00WW build tag alone
		{"nokia72 exif", "HMD Global", "Nokia 7.2", "", "", "Nokia 7.2", "smartphone", "IMG"},
		{"nokia72 firmware", "", "", "00WW_3_690_SP04", "", "Nokia 7.2", "smartphone", "IMG"},
		// Nikon D3500 — EXIF, and recovered from XMP CreatorTool only
		{"nikon exif", "NIKON CORPORATION", "NIKON D3500", "", "", "Nikon D3500", "camera", "DSC"},
		{"nikon xmp only", "", "", "", "NIKON D3500 Ver.1.00", "Nikon D3500", "camera", "DSC"},
		// Samsung codes
		{"galaxy a3", "SAMSUNG", "SM-A300FU", "", "", "Samsung Galaxy A3 (2015)", "smartphone", "IMG"},
		{"galaxy s3 lc", "samsung", "GT-I9300", "", "", "Samsung Galaxy S III", "smartphone", "IMG"},
		// Canon — syntactic normalization (make deduped)
		{"canon", "Canon", "Canon EOS 1000D", "", "", "Canon EOS 1000D", "camera", "DSC"},
		// Sony α7 IV edited in Snapseed: device kept, category becomes snapseed
		{"sony edit", "Sony", "ILCE-7M4", "Snapseed 2.0", "", "Sony α7 IV", "snapseed", "SNPSD"},
		// device-less edit: no device, editing category
		{"ps deviceless", "", "", "", "Adobe Photoshop CC 2017", "", "photoshop", "PS"},
		// genuinely unknown -> normalized name, IMG
		{"unknown", "", "", "", "", "", "", "IMG"},
	}
	for _, c := range cases {
		dev, cat, prefix := CanonicalDevice(c.make, c.model, c.software, c.creator)
		if dev != c.dev || cat != c.cat || prefix != c.prefix {
			t.Errorf("%s: got (%q,%q,%q), want (%q,%q,%q)",
				c.name, dev, cat, prefix, c.dev, c.cat, c.prefix)
		}
	}
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package winres

import (
	"bytes"
	"encoding/binary"
	"regexp"
	"strconv"
	"unicode/utf16"
)

const rtVersion = 16

// reSemver takes the numbers of a version tag ("v1.2.3", "v1.2.3-4-gabc-dirty").
var reSemver = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)`)

// VersionNumbers are the four numbers of the fixed file version: the tag's
// major.minor.patch and 0, or 0.0.0.0 for a build that is not a tag ("dev",
// a bare commit).
func VersionNumbers(version string) [4]uint16 {
	m := reSemver.FindStringSubmatch(version)
	if m == nil {
		return [4]uint16{}
	}
	var n [4]uint16
	for i := 0; i < 3; i++ {
		v, _ := strconv.Atoi(m[i+1])
		n[i] = uint16(min(v, 65535))
	}
	return n
}

// versionInfo is the VS_VERSIONINFO resource: the fixed file info (numbers
// shown by Explorer and Task Manager) and the English string table.
func versionInfo(version string) []byte {
	n := VersionNumbers(version)
	ms := uint32(n[0])<<16 | uint32(n[1])
	ls := uint32(n[2])<<16 | uint32(n[3])
	fixed := new(bytes.Buffer)
	binary.Write(fixed, binary.LittleEndian, [13]uint32{
		0xFEEF04BD, 0x00010000, // signature, structure version
		ms, ls, ms, ls, // file version, product version
		0x3F, 0, // flags mask, flags
		0x00040004, 1, 0, // VOS_NT_WINDOWS32, VFT_APP, subtype
		0, 0, // file date
	})
	strs := []([2]string){
		{"CompanyName", "Maurizio Fonte"},
		{"FileDescription", "onefold"},
		{"FileVersion", version},
		{"InternalName", "onefold"},
		{"LegalCopyright", "Copyright (c) 2026 Maurizio Fonte. AGPL-3.0-or-later"},
		{"OriginalFilename", "onefold.exe"},
		{"ProductName", "onefold"},
		{"ProductVersion", version},
	}
	var table [][]byte
	for _, s := range strs {
		table = append(table, block(s[0], wstr(s[1]), 1, len(utf16.Encode([]rune(s[1])))+1))
	}
	sfi := block("StringFileInfo", nil, 1, 0, block("040904B0", nil, 1, 0, table...))
	trans := []byte{0x09, 0x04, 0xB0, 0x04} // English (US), Unicode
	vfi := block("VarFileInfo", nil, 1, 0, block("Translation", trans, 0, len(trans)))
	return block("VS_VERSION_INFO", fixed.Bytes(), 0, fixed.Len(), sfi, vfi)
}

// block writes one version-info structure: wLength, wValueLength, wType, the
// UTF-16 key, padding to 32 bits, the value, padding, the children.
// valueLen is in bytes for binary values (type 0), in WORDs for text (type 1).
func block(key string, value []byte, typ uint16, valueLen int, children ...[]byte) []byte {
	b := new(bytes.Buffer)
	b.Write(make([]byte, 6)) // wLength, wValueLength, wType: filled below
	b.Write(wstr(key))
	pad4(b)
	b.Write(value)
	for _, c := range children {
		pad4(b)
		b.Write(c)
	}
	out := b.Bytes()
	binary.LittleEndian.PutUint16(out[0:], uint16(len(out)))
	binary.LittleEndian.PutUint16(out[2:], uint16(valueLen))
	binary.LittleEndian.PutUint16(out[4:], typ)
	return out
}

// wstr is s as NUL-terminated UTF-16LE.
func wstr(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 2*len(u)+2)
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[2*i:], c)
	}
	return b
}

func pad4(b *bytes.Buffer) {
	for b.Len()%4 != 0 {
		b.WriteByte(0)
	}
}

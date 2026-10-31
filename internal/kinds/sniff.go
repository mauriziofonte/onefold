// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package kinds

import (
	"io"
	"os"
)

// Magic-byte families. A registry format maps to the family of bytes it starts
// with; aliases share a family (docx IS a zip, heic IS ISO-BMFF), so an alias
// is never reported as a mismatch.
var formatFamily = map[string]string{
	"jpeg": "jpg", "png": "png", "gif": "gif", "bmp": "bmp", "webp": "webp", "tiff": "tiff", "heif": "isobmff",
	"pdf": "pdf", "isobmff": "isobmff", "ebml": "ebml", "riff-avi": "avi", "riff-wav": "wav", "asf": "asf",
	"mp3": "mp3", "flac": "flac", "ogg": "ogg", "zip": "zip", "rar": "rar", "7z": "7z", "gzip": "gz",
	"bzip2": "bz2", "xz": "xz", "ole-doc": "ole", "ole-sheet": "ole", "ole-slides": "ole", "ole-mail": "ole",
	"ole-other": "ole", "ooxml-doc": "zip", "ooxml-sheet": "zip", "ooxml-slides": "zip", "odf-doc": "zip",
	"odf-sheet": "zip", "odf-slides": "zip", "epub": "zip", "pe": "mz", "ico": "ico", "psd": "psd", "rtf": "rtf",
}

// familyCanon is the extension a family is renamed to by --fix-ext. Families
// absent here are ambiguous (isobmff: mp4 or heic? ole: doc or xls?).
var familyCanon = map[string]string{
	"jpg": "jpg", "png": "png", "gif": "gif", "bmp": "bmp", "pdf": "pdf", "webp": "webp", "avi": "avi", "wav": "wav",
	"zip": "zip", "rar": "rar", "7z": "7z", "gz": "gz", "bz2": "bz2", "xz": "xz", "flac": "flac", "ogg": "ogg",
	"mp3": "mp3", "psd": "psd", "rtf": "rtf",
}

// SniffFamily detects the container family from the first bytes ("" if unknown).
func SniffFamily(h []byte) string {
	has := func(off int, sig string) bool {
		return len(h) >= off+len(sig) && string(h[off:off+len(sig)]) == sig
	}
	switch {
	case has(0, "\xff\xd8\xff"):
		return "jpg"
	case has(0, "\x89PNG\r\n\x1a\n"):
		return "png"
	case has(0, "GIF87a"), has(0, "GIF89a"):
		return "gif"
	case has(0, "%PDF-"):
		return "pdf"
	case has(0, "RIFF") && has(8, "WEBP"):
		return "webp"
	case has(0, "RIFF") && has(8, "AVI "):
		return "avi"
	case has(0, "RIFF") && has(8, "WAVE"):
		return "wav"
	case has(0, "PK\x03\x04"), has(0, "PK\x05\x06"):
		return "zip"
	case has(0, "Rar!\x1a\x07"):
		return "rar"
	case has(0, "7z\xbc\xaf\x27\x1c"):
		return "7z"
	case has(0, "\x1f\x8b"):
		return "gz"
	case has(0, "BZh"):
		return "bz2"
	case has(0, "\xfd7zXZ\x00"):
		return "xz"
	case has(4, "ftyp"):
		return "isobmff"
	case has(0, "\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1"):
		return "ole"
	case has(0, "\x1a\x45\xdf\xa3"):
		return "ebml"
	case has(0, "\x30\x26\xb2\x75\x8e\x66\xcf\x11"):
		return "asf"
	case has(0, "fLaC"):
		return "flac"
	case has(0, "OggS"):
		return "ogg"
	case has(0, "ID3"):
		return "mp3"
	case has(0, "II*\x00"), has(0, "MM\x00*"):
		return "tiff"
	case has(0, "8BPS"):
		return "psd"
	case has(0, "{\\rtf"):
		return "rtf"
	case has(0, "MZ"):
		return "mz"
	case has(0, "\x00\x00\x01\x00"):
		return "ico"
	}
	return ""
}

// SniffFile reads the head of path and returns its family ("" if unknown or unreadable).
func SniffFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	buf := make([]byte, 16)
	n, _ := io.ReadFull(f, buf)
	return SniffFamily(buf[:n])
}

// Mismatch compares the declared extension with the sniffed family. mismatch
// only when BOTH are known and differ; fixExt = extension to rename to (""
// when the sniffed family is ambiguous).
func Mismatch(ext, family string) (mismatch bool, fixExt string) {
	declared := formatFamily[Format(ext)]
	if declared == "" || family == "" || declared == family {
		return false, ""
	}
	return true, familyCanon[family]
}

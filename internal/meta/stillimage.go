// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import (
	"bytes"
	"encoding/binary"
	"strings"

	"github.com/evanoberholster/imagemeta"
)

// Pure-Go EXIF readers for the photo formats imagemeta does not cover:
// WebP (RIFF EXIF chunk) and the 'Exif' item of HEIF/AVIF files whose EXIF
// imagemeta does not find. Without them a Windows host without exiftool would
// date these photos by mtime (parity).

// exifTIFF decodes a raw TIFF EXIF block (as stored by PNG eXIf, WebP EXIF,
// HEIF Exif items) into m: capture time with sub-second, device.
func exifTIFF(b []byte, m *Meta) error {
	ex, err := imagemeta.Decode(bytes.NewReader(b))
	if err != nil {
		return err
	}
	if dto := ex.DateTimeOriginal(); !dto.IsZero() {
		setCaptureTime(m, dto)
	} else if cd := ex.CreateDate(); !cd.IsZero() {
		setTime(m, cd, true)
	}
	mk, md := strings.TrimSpace(ex.Make), strings.TrimSpace(ex.Model)
	if mk != "" || md != "" {
		m.CanonDevice, m.Category, m.Prefix = CanonicalDevice(mk, md, ex.Software, "")
		m.Device = strings.TrimSpace(mk + " " + md)
	}
	setGeo(m, ex.GPS.Latitude(), ex.GPS.Longitude())
	return nil
}

// webpNative reads the canvas size (VP8X, else the VP8/VP8L frame header) and
// the EXIF chunk of a WebP file.
func webpNative(p string, m *Meta) error {
	b := head(p, 32<<20)
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return errUnsupported
	}
	for pos := 12; pos+8 <= len(b); {
		typ := string(b[pos : pos+4])
		n := int(binary.LittleEndian.Uint32(b[pos+4 : pos+8]))
		if n < 0 || n > len(b)-pos-8 {
			break
		}
		d := b[pos+8 : pos+8+n]
		switch {
		case typ == "VP8X" && n >= 10:
			m.Width = 1 + int(uint32(d[4])|uint32(d[5])<<8|uint32(d[6])<<16)
			m.Height = 1 + int(uint32(d[7])|uint32(d[8])<<8|uint32(d[9])<<16)
		case typ == "VP8 " && n >= 10 && m.Width == 0:
			m.Width = int(binary.LittleEndian.Uint16(d[6:8]) & 0x3fff)
			m.Height = int(binary.LittleEndian.Uint16(d[8:10]) & 0x3fff)
		case typ == "VP8L" && n >= 5 && d[0] == 0x2f && m.Width == 0:
			v := binary.LittleEndian.Uint32(d[1:5])
			m.Width, m.Height = int(v&0x3fff)+1, int(v>>14&0x3fff)+1
		case typ == "EXIF":
			_ = exifTIFF(bytes.TrimPrefix(d, []byte("Exif\x00\x00")), m)
		}
		pos += 8 + n + n&1 // chunks are padded to an even size
	}
	return nil
}

// heifExifNative reads the 'Exif' item of a HEIF/HEIC/AVIF file: meta box ->
// iinf (item of type Exif) -> iloc (its extents, in the file or in idat).
func heifExifNative(p string, m *Meta) error {
	f, size, err := openSrc(p)
	if err != nil {
		return err
	}
	defer f.Close()
	hb := make([]byte, min64(size, 4<<20))
	n, _ := f.ReadAt(hb, 0)
	hb = hb[:n]
	meta := isoChild(hb, "meta")
	if len(meta) < 4 {
		return errUnsupported
	}
	meta = meta[4:] // FullBox version and flags
	id, ok := exifItemID(isoChild(meta, "iinf"))
	if !ok {
		return errUnsupported
	}
	off, length, method, ok := ilocExtent(isoChild(meta, "iloc"), id)
	if !ok || length < 8 || length > 4<<20 {
		return errUnsupported
	}
	var item []byte
	switch method {
	case 0: // file offset
		item = make([]byte, length)
		if k, _ := f.ReadAt(item, int64(off)); uint64(k) != length {
			return errUnsupported
		}
	case 1: // offset inside the idat box of meta
		idat := isoChild(meta, "idat")
		if off+length > uint64(len(idat)) {
			return errUnsupported
		}
		item = idat[off : off+length]
	default:
		return errUnsupported
	}
	skip := uint64(binary.BigEndian.Uint32(item[:4])) // exif_tiff_header_offset
	if 4+skip >= uint64(len(item)) {
		return errUnsupported
	}
	return exifTIFF(item[4+skip:], m)
}

// isoChild returns the payload of the first child box of type typ in b.
func isoChild(b []byte, typ string) []byte {
	for pos := 0; pos+8 <= len(b); {
		size := uint64(binary.BigEndian.Uint32(b[pos : pos+4]))
		hdr := uint64(8)
		switch size {
		case 0:
			size = uint64(len(b) - pos)
		case 1:
			if pos+16 > len(b) {
				return nil
			}
			size, hdr = binary.BigEndian.Uint64(b[pos+8:pos+16]), 16
		}
		if size < hdr || size > uint64(len(b)-pos) {
			return nil
		}
		if string(b[pos+4:pos+8]) == typ {
			return b[pos+int(hdr) : pos+int(size)]
		}
		pos += int(size)
	}
	return nil
}

// exifItemID finds the item of type "Exif" in an iinf payload.
func exifItemID(b []byte) (uint32, bool) {
	if len(b) < 6 {
		return 0, false
	}
	pos := 6 // version, flags, entry_count (16 bit)
	if b[0] != 0 {
		pos = 8 // entry_count (32 bit)
	}
	for pos+12 <= len(b) {
		size := int(binary.BigEndian.Uint32(b[pos : pos+4]))
		if size < 8 || pos+size > len(b) {
			return 0, false
		}
		if string(b[pos+4:pos+8]) == "infe" && size >= 20 {
			e := b[pos+8 : pos+size]
			switch e[0] { // infe version
			case 2:
				if string(e[8:12]) == "Exif" {
					return uint32(binary.BigEndian.Uint16(e[4:6])), true
				}
			case 3:
				if len(e) >= 14 && string(e[10:14]) == "Exif" {
					return binary.BigEndian.Uint32(e[4:8]), true
				}
			}
		}
		pos += size
	}
	return 0, false
}

// ilocExtent returns the first extent of item id from an iloc payload, plus
// its construction method (0 file, 1 idat).
func ilocExtent(b []byte, id uint32) (off, length uint64, method int, ok bool) {
	if len(b) < 8 {
		return 0, 0, 0, false
	}
	v := b[0]
	offSize, lenSize := int(b[4]>>4), int(b[4]&15)
	baseSize, idxSize := int(b[5]>>4), int(b[5]&15)
	if v == 0 {
		idxSize = 0
	}
	pos := 6
	rd := func(n int) (uint64, bool) {
		if pos+n > len(b) {
			return 0, false
		}
		var x uint64
		for i := 0; i < n; i++ {
			x = x<<8 | uint64(b[pos+i])
		}
		pos += n
		return x, true
	}
	idSize := 2
	if v == 2 {
		idSize = 4
	}
	count, ok := rd(idSize)
	for i := uint64(0); ok && i < count; i++ {
		var item, base, extents uint64
		if item, ok = rd(idSize); !ok {
			break
		}
		cm := uint64(0)
		if v == 1 || v == 2 {
			if cm, ok = rd(2); !ok {
				break
			}
		}
		if _, ok = rd(2); !ok { // data_reference_index
			break
		}
		if base, ok = rd(baseSize); !ok {
			break
		}
		if extents, ok = rd(2); !ok {
			break
		}
		for e := uint64(0); ok && e < extents; e++ {
			var eo, el uint64
			if _, ok = rd(idxSize); !ok {
				break
			}
			if eo, ok = rd(offSize); !ok {
				break
			}
			if el, ok = rd(lenSize); !ok {
				break
			}
			if uint32(item) == id && e == 0 {
				return base + eo, el, int(cm & 15), true
			}
		}
	}
	return 0, 0, 0, false
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package testfix

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"sort"
)

// Exif is the metadata of a synthetic photo: any empty field is left out
// (Lat and Lon both 0 = no GPS).
type Exif struct {
	DateTimeOriginal string // "2021:05:06 07:08:09"
	Make, Model      string
	Lat, Lon         float64
}

// JPEG writes a w x h JPEG (colors from seed) carrying e in an EXIF APP1
// segment (little-endian TIFF: IFD0, the Exif IFD, the GPS IFD).
func JPEG(p string, w, h int, seed uint8, e Exif) error {
	if err := mkdir(p); err != nil {
		return err
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{seed, uint8(x * 3), uint8(y * 5), 255})
		}
	}
	var enc bytes.Buffer
	if err := jpeg.Encode(&enc, img, &jpeg.Options{Quality: 90}); err != nil {
		return err
	}
	tiff := exifTIFF(e)
	app1 := append([]byte("Exif\x00\x00"), tiff...)
	var out bytes.Buffer
	out.Write([]byte{0xFF, 0xD8, 0xFF, 0xE1})
	binary.Write(&out, binary.BigEndian, uint16(len(app1)+2))
	out.Write(app1)
	out.Write(enc.Bytes()[2:]) // after the encoder's own SOI
	return os.WriteFile(p, out.Bytes(), 0o644)
}

type tag struct {
	id, typ uint16
	count   uint32
	data    []byte // the value bytes (inline when 4 or fewer)
}

func ascii(id uint16, s string) tag {
	return tag{id, 2, uint32(len(s) + 1), append([]byte(s), 0)}
}

func long(id uint16, v uint32) tag {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return tag{id, 4, 1, b}
}

// dms is a coordinate as 3 rationals: degrees, minutes, seconds (1/10000).
func dms(id uint16, v float64) tag {
	v = math.Abs(v)
	d := math.Floor(v)
	m := math.Floor((v - d) * 60)
	s := ((v-d)*60 - m) * 60
	var b []byte
	for _, r := range [][2]uint32{{uint32(d), 1}, {uint32(m), 1}, {uint32(math.Round(s * 10000)), 10000}} {
		b = binary.LittleEndian.AppendUint32(b, r[0])
		b = binary.LittleEndian.AppendUint32(b, r[1])
	}
	return tag{id, 5, 3, b}
}

// ifd lays out one IFD at off; next is the offset of the IFD after it (0 =
// none). Values over 4 bytes follow the entries.
func ifd(tags []tag, off uint32) []byte {
	sort.Slice(tags, func(i, j int) bool { return tags[i].id < tags[j].id })
	head := 2 + 12*len(tags) + 4
	data := off + uint32(head)
	var b, ext []byte
	b = binary.LittleEndian.AppendUint16(b, uint16(len(tags)))
	for _, t := range tags {
		b = binary.LittleEndian.AppendUint16(b, t.id)
		b = binary.LittleEndian.AppendUint16(b, t.typ)
		b = binary.LittleEndian.AppendUint32(b, t.count)
		if len(t.data) <= 4 {
			v := make([]byte, 4)
			copy(v, t.data)
			b = append(b, v...)
			continue
		}
		b = binary.LittleEndian.AppendUint32(b, data+uint32(len(ext)))
		ext = append(ext, t.data...)
		if len(ext)%2 == 1 {
			ext = append(ext, 0)
		}
	}
	b = binary.LittleEndian.AppendUint32(b, 0)
	return append(b, ext...)
}

func exifTIFF(e Exif) []byte {
	var ifd0, exif, gps []tag
	if e.Make != "" {
		ifd0 = append(ifd0, ascii(0x010F, e.Make))
	}
	if e.Model != "" {
		ifd0 = append(ifd0, ascii(0x0110, e.Model))
	}
	if e.DateTimeOriginal != "" {
		exif = append(exif, ascii(0x9003, e.DateTimeOriginal))
	}
	if e.Lat != 0 || e.Lon != 0 {
		ns, ew := "N", "E"
		if e.Lat < 0 {
			ns = "S"
		}
		if e.Lon < 0 {
			ew = "W"
		}
		gps = append(gps, tag{0x0000, 1, 4, []byte{2, 3, 0, 0}}, ascii(0x0001, ns), dms(0x0002, e.Lat), ascii(0x0003, ew), dms(0x0004, e.Lon))
	}
	// sizes first: IFD0 holds the pointers to the two others
	size0 := len(ifd(append(append([]tag(nil), ifd0...), long(0x8769, 0), long(0x8825, 0)), 8))
	exifOff := uint32(8 + size0)
	sizeExif := len(ifd(exif, exifOff))
	gpsOff := exifOff + uint32(sizeExif)
	ifd0 = append(ifd0, long(0x8769, exifOff), long(0x8825, gpsOff))
	out := []byte("II*\x00\x08\x00\x00\x00")
	out = append(out, ifd(ifd0, 8)...)
	out = append(out, ifd(exif, exifOff)...)
	out = append(out, ifd(gps, gpsOff)...)
	return out
}

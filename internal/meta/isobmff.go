// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import (
	"encoding/binary"
	"io"
	"strings"
	"time"
)

// isobmffInfo is what the pure-Go MP4/MOV/3GP/M4A reader extracts: the subset
// of ffprobe fields onefold uses (creation_time, duration, dims, vendor signals).
type isobmffInfo struct {
	Creation     time.Time // mvhd creation_time (UTC, as ffprobe reports it)
	DurationSec  float64
	Width        int
	Height       int
	MajorBrand   string
	CompatBrands string
	HandlerName  string            // video track hdlr name
	Tags         map[string]string // keys/ilst + udta string atoms (lowercased keys)
	HasVideo     bool
	codedW       int // stsd visual sample entry (preferred over tkhd display size)
	codedH       int
}

var visualCodec = map[string]bool{"avc1": true, "avc3": true, "hvc1": true, "hev1": true, "mp4v": true, "s263": true, "h263": true,
	"jpeg": true, "mjpa": true, "mjpb": true, "av01": true, "vp08": true, "vp09": true, "apcn": true, "apch": true, "apcs": true,
	"apco": true, "ap4h": true, "dvh1": true, "dvhe": true, "SVQ3": true, "SVQ1": true, "rpza": true, "cvid": true, "2vuy": true}

var mp4Epoch = time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)

// readISOBMFF walks the box tree seeking over media data (mdat is never read).
func readISOBMFF(p string) (*isobmffInfo, error) {
	f, fsize, err := openSrc(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info := &isobmffInfo{Tags: map[string]string{}}
	r := &boxReader{f: f}
	if err := r.walk(0, fsize, 0, info, ""); err != nil && info.Creation.IsZero() && info.DurationSec == 0 {
		return nil, err
	}
	return info, nil
}

type boxReader struct {
	f     src
	keys  []string // meta/keys table (1-based index in ilst)
	boxes int
}

func (r *boxReader) readAt(off int64, n int) ([]byte, error) {
	if n < 0 || n > 1<<24 {
		return nil, io.ErrUnexpectedEOF
	}
	b := make([]byte, n)
	_, err := r.f.ReadAt(b, off)
	return b, err
}

// containers whose children are boxes (meta has a 4-byte version/flags prefix
// in ISO files but not in QuickTime: detected below).
var containerBox = map[string]bool{"moov": true, "trak": true, "mdia": true, "minf": true, "stbl": true, "udta": true, "meta": true, "ilst": true, "edts": true}

func (r *boxReader) walk(start, end int64, depth int, info *isobmffInfo, parent string) error {
	off := start
	trakVideo := false
	for off+8 <= end {
		r.boxes++
		if r.boxes > 20000 || depth > 12 {
			return nil
		}
		h, err := r.readAt(off, 16)
		if err != nil && len(h) < 8 {
			return err
		}
		size := int64(binary.BigEndian.Uint32(h[0:4]))
		typ := string(h[4:8])
		hdr := int64(8)
		switch size {
		case 1:
			if len(h) < 16 {
				return io.ErrUnexpectedEOF
			}
			size = int64(binary.BigEndian.Uint64(h[8:16]))
			hdr = 16
		case 0:
			size = end - off
		}
		if size < hdr || off+size > end {
			return nil // truncated/corrupt: keep what we have
		}
		body, bodyLen := off+hdr, size-hdr
		switch {
		case typ == "ftyp" && bodyLen >= 8:
			b, _ := r.readAt(body, int(min64(bodyLen, 256)))
			info.MajorBrand = strings.TrimRight(string(b[0:4]), "\x00 ")
			var cb strings.Builder
			for i := 8; i+4 <= len(b); i += 4 {
				cb.WriteString(strings.TrimRight(string(b[i:i+4]), "\x00"))
			}
			info.CompatBrands = cb.String()
		case typ == "mvhd" && bodyLen >= 20:
			b, _ := r.readAt(body, int(min64(bodyLen, 40)))
			if b[0] == 1 && len(b) >= 32 {
				info.Creation = mp4Epoch.Add(time.Duration(binary.BigEndian.Uint64(b[4:12])) * time.Second)
				ts := binary.BigEndian.Uint32(b[20:24])
				if ts > 0 {
					info.DurationSec = float64(binary.BigEndian.Uint64(b[24:32])) / float64(ts)
				}
			} else {
				c := binary.BigEndian.Uint32(b[4:8])
				if c != 0 {
					info.Creation = mp4Epoch.Add(time.Duration(c) * time.Second)
				}
				ts := binary.BigEndian.Uint32(b[12:16])
				if ts > 0 {
					info.DurationSec = float64(binary.BigEndian.Uint32(b[16:20])) / float64(ts)
				}
			}
		case typ == "trak":
			sub := &isobmffInfo{Tags: info.Tags}
			if err := r.walk(body, off+size, depth+1, sub, "trak"); err != nil {
				return err
			}
			if sub.HasVideo && !info.HasVideo {
				info.HasVideo = true
				info.Width, info.Height, info.HandlerName = sub.Width, sub.Height, sub.HandlerName
				if sub.codedW > 0 {
					info.Width, info.Height = sub.codedW, sub.codedH
				}
			}
		case typ == "tkhd" && bodyLen >= 84:
			b, _ := r.readAt(body, int(min64(bodyLen, 96)))
			wOff := 76 // version 0
			if b[0] == 1 {
				wOff = 88
			}
			if len(b) >= wOff+8 {
				info.Width = int(binary.BigEndian.Uint32(b[wOff:wOff+4]) >> 16)
				info.Height = int(binary.BigEndian.Uint32(b[wOff+4:wOff+8]) >> 16)
			}
		case typ == "hdlr" && bodyLen >= 24:
			b, _ := r.readAt(body, int(min64(bodyLen, 256)))
			if string(b[8:12]) == "vide" {
				trakVideo = true
				info.HasVideo = true
				name := strings.TrimRight(string(b[24:]), "\x00")
				if len(name) > 0 && int(name[0]) == len(name)-1 { // QuickTime pascal string
					name = name[1:]
				}
				info.HandlerName = strings.TrimSpace(name)
			}
		case typ == "stsd" && bodyLen >= 44:
			// first sample entry of a video track: coded width/height (what ffprobe reports)
			b, _ := r.readAt(body, int(min64(bodyLen, 64)))
			fmt4 := string(b[12:16])
			if len(b) >= 44 && visualCodec[fmt4] {
				if w, h := int(binary.BigEndian.Uint16(b[40:42])), int(binary.BigEndian.Uint16(b[42:44])); w > 0 && h > 0 {
					info.codedW, info.codedH = w, h
				}
			}
		case typ == "keys" && bodyLen >= 8:
			b, _ := r.readAt(body, int(min64(bodyLen, 1<<16)))
			n := int(binary.BigEndian.Uint32(b[4:8]))
			r.keys = r.keys[:0]
			for i, pos := 0, 8; i < n && pos+8 <= len(b); i++ {
				ks := int(binary.BigEndian.Uint32(b[pos : pos+4]))
				if ks < 8 || pos+ks > len(b) {
					break
				}
				r.keys = append(r.keys, string(b[pos+8:pos+ks]))
				pos += ks
			}
		case parent == "ilst" && bodyLen >= 16:
			// child of ilst: key = 4CC (udta-style) or 1-based index into keys
			key := typ
			if idx := binary.BigEndian.Uint32(h[4:8]); idx >= 1 && int(idx) <= len(r.keys) && !isPrintable4(h[4:8]) {
				key = r.keys[idx-1]
			}
			if v := r.dataValue(body, bodyLen); v != "" {
				info.Tags[strings.ToLower(key)] = v
			}
		case parent == "udta" && len(typ) == 4 && typ[0] == 0xa9 && bodyLen > 4:
			// QuickTime international text atom: size(2) lang(2) text
			b, _ := r.readAt(body, int(min64(bodyLen, 512)))
			if len(b) > 4 {
				n := int(binary.BigEndian.Uint16(b[0:2]))
				if 4+n <= len(b) {
					info.Tags[strings.ToLower(typ)] = strings.TrimSpace(string(b[4 : 4+n]))
				}
			}
		case containerBox[typ]:
			cs := body
			if typ == "meta" {
				// ISO meta is a FullBox (4 bytes version/flags); QuickTime meta is not.
				if b, _ := r.readAt(body, 8); len(b) == 8 && string(b[4:8]) != "hdlr" {
					cs += 4
				}
			}
			if err := r.walk(cs, off+size, depth+1, info, typ); err != nil {
				return err
			}
		}
		off += size
	}
	if trakVideo {
		info.HasVideo = true
	}
	return nil
}

// dataValue reads the "data" child of an ilst item as a string.
func (r *boxReader) dataValue(start, n int64) string {
	b, _ := r.readAt(start, int(min64(n, 4096)))
	if len(b) < 16 || string(b[4:8]) != "data" {
		return ""
	}
	sz := int(binary.BigEndian.Uint32(b[0:4]))
	if sz < 16 || sz > len(b) {
		sz = len(b)
	}
	typ := binary.BigEndian.Uint32(b[8:12]) & 0xffffff
	v := b[16:sz]
	switch typ {
	case 1: // UTF-8
		return strings.TrimSpace(string(v))
	case 21, 22: // signed/unsigned int
		var x uint64
		for _, c := range v {
			x = x<<8 | uint64(c)
		}
		return uitoa(x)
	case 0: // implicit (e.g. trkn: pad2 track2 total2)
		if len(v) >= 4 {
			return uitoa(uint64(binary.BigEndian.Uint16(v[2:4])))
		}
	}
	return ""
}

func isPrintable4(b []byte) bool {
	for _, c := range b {
		if c < 0x20 || (c > 0x7e && c != 0xa9) {
			return false
		}
	}
	return true
}

func uitoa(x uint64) string {
	if x == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for x > 0 {
		i--
		buf[i] = byte('0' + x%10)
		x /= 10
	}
	return string(buf[i:])
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// vendorSignals maps the parsed boxes onto the ffprobe-shaped vendor signals.
func (i *isobmffInfo) vendorSignals() VSignals {
	t := i.Tags
	return VSignals{
		AndroidMfr:   t["com.android.manufacturer"],
		AndroidModel: t["com.android.model"],
		AndroidVer:   t["com.android.version"],
		CompatBrands: i.CompatBrands,
		Comment:      firstNonEmpty(t["\xa9cmt"], t["comment"]),
		EncoderFmt:   firstNonEmpty(t["\xa9too"], t["encoder"]),
		MajorBrand:   i.MajorBrand,
		Firmware:     firstNonEmpty(t["firm"], t["com.apple.quicktime.software"]),
		HandlerName:  i.HandlerName,
	}
}

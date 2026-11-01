// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"time"
)

// Matroska / WebM (EBML) without ffprobe: the Segment's Info element gives
// the recording date (DateUTC) and the duration, the first video track its
// size. Only the headers before the first Cluster are read.

// EBML element IDs (with their length marker, as written in the file).
const (
	ebmlHeader     = 0x1A45DFA3
	ebmlSegment    = 0x18538067
	ebmlInfo       = 0x1549A966
	ebmlTimescale  = 0x2AD7B1
	ebmlDuration   = 0x4489
	ebmlDateUTC    = 0x4461
	ebmlTracks     = 0x1654AE6B
	ebmlTrackEntry = 0xAE
	ebmlTrackType  = 0x83
	ebmlVideo      = 0xE0
	ebmlPixelW     = 0xB0
	ebmlPixelH     = 0xBA
	ebmlCluster    = 0x1F43B675
)

// ebmlEpoch is the origin of DateUTC (nanoseconds).
var ebmlEpoch = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

var errNotEBML = errors.New("not a Matroska/WebM file")

type ebmlInfoT struct {
	Creation    time.Time // UTC; zero when absent
	DurationSec float64
	Width       int
	Height      int
}

// readEBML reads the headers of a Matroska/WebM file.
func readEBML(path string) (ebmlInfoT, error) {
	var info ebmlInfoT
	f, size, err := openSrc(path)
	if err != nil {
		return info, err
	}
	defer f.Close()
	r := &ebmlReader{f: f, size: size}
	id, n, _, err := r.element(0)
	if err != nil || id != ebmlHeader {
		return info, errNotEBML
	}
	off := n.end
	for off < size {
		id, el, unknown, err := r.element(off)
		if err != nil {
			return info, err
		}
		if id == ebmlSegment {
			end := el.end
			if unknown {
				end = size
			}
			return info, r.segment(el.data, end, &info)
		}
		off = el.end
	}
	return info, errNotEBML
}

type ebmlSpan struct{ data, end int64 }

type ebmlReader struct {
	f    src
	size int64
}

// element reads the ID and size at off; unknown = the "size unknown" value
// (live streams), then the span ends at the end of the file.
func (r *ebmlReader) element(off int64) (uint32, ebmlSpan, bool, error) {
	var b [12]byte
	n, _ := r.f.ReadAt(b[:], off)
	if n < 2 {
		return 0, ebmlSpan{}, false, io.ErrUnexpectedEOF
	}
	idLen := vintLen(b[0])
	if idLen == 0 || idLen > 4 || idLen >= n {
		return 0, ebmlSpan{}, false, errNotEBML
	}
	var id uint32
	for _, c := range b[:idLen] {
		id = id<<8 | uint32(c)
	}
	sLen := vintLen(b[idLen])
	if sLen == 0 || idLen+sLen > n {
		return 0, ebmlSpan{}, false, errNotEBML
	}
	v := uint64(b[idLen]) & (0xFF >> sLen)
	allOnes := v == 0xFF>>sLen
	for _, c := range b[idLen+1 : idLen+sLen] {
		v = v<<8 | uint64(c)
		allOnes = allOnes && c == 0xFF
	}
	data := off + int64(idLen+sLen)
	if allOnes {
		return id, ebmlSpan{data, r.size}, true, nil
	}
	if v > uint64(r.size-data) {
		v = uint64(r.size - data) // truncated file: read what is there
	}
	return id, ebmlSpan{data, data + int64(v)}, false, nil
}

// vintLen is the length of an EBML variable-size integer from its first byte.
func vintLen(c byte) int {
	for i := 0; i < 8; i++ {
		if c&(0x80>>i) != 0 {
			return i + 1
		}
	}
	return 0
}

func (r *ebmlReader) bytes(s ebmlSpan, max int64) ([]byte, error) {
	n := s.end - s.data
	if n < 0 || n > max {
		return nil, errNotEBML
	}
	b := make([]byte, n)
	_, err := r.f.ReadAt(b, s.data)
	return b, err
}

func (r *ebmlReader) uint(s ebmlSpan) uint64 {
	b, err := r.bytes(s, 8)
	if err != nil {
		return 0
	}
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return v
}

// segment walks the top-level elements of the Segment up to the first Cluster.
func (r *ebmlReader) segment(off, end int64, info *ebmlInfoT) error {
	scale := uint64(1000000) // TimecodeScale default: 1 ms
	var rawDur float64
	seenInfo := false
	for off < end {
		id, el, unknown, err := r.element(off)
		if err != nil {
			break
		}
		if id == ebmlCluster || unknown {
			break // media data: the headers are behind us
		}
		switch id {
		case ebmlInfo:
			seenInfo = true
			for o := el.data; o < el.end; {
				cid, c, _, err := r.element(o)
				if err != nil {
					break
				}
				switch cid {
				case ebmlTimescale:
					if v := r.uint(c); v > 0 {
						scale = v
					}
				case ebmlDuration:
					if b, err := r.bytes(c, 8); err == nil {
						switch len(b) {
						case 4:
							rawDur = float64(math.Float32frombits(binary.BigEndian.Uint32(b)))
						case 8:
							rawDur = math.Float64frombits(binary.BigEndian.Uint64(b))
						}
					}
				case ebmlDateUTC:
					if b, err := r.bytes(c, 8); err == nil && len(b) == 8 {
						info.Creation = ebmlEpoch.Add(time.Duration(int64(binary.BigEndian.Uint64(b))))
					}
				}
				o = c.end
			}
		case ebmlTracks:
			r.tracks(el, info)
		}
		off = el.end
	}
	if !seenInfo {
		return errNotEBML
	}
	info.DurationSec = rawDur * float64(scale) / 1e9
	return nil
}

// tracks takes the size of the first video track.
func (r *ebmlReader) tracks(el ebmlSpan, info *ebmlInfoT) {
	for o := el.data; o < el.end && info.Width == 0; {
		id, t, _, err := r.element(o)
		if err != nil {
			return
		}
		if id == ebmlTrackEntry {
			var typ uint64
			var w, h int
			for p := t.data; p < t.end; {
				cid, c, _, err := r.element(p)
				if err != nil {
					break
				}
				switch cid {
				case ebmlTrackType:
					typ = r.uint(c)
				case ebmlVideo:
					for q := c.data; q < c.end; {
						vid, v, _, err := r.element(q)
						if err != nil {
							break
						}
						switch vid {
						case ebmlPixelW:
							w = int(r.uint(v))
						case ebmlPixelH:
							h = int(r.uint(v))
						}
						q = v.end
					}
				}
				p = c.end
			}
			if typ == 1 {
				info.Width, info.Height = w, h
			}
		}
		o = t.end
	}
}

// FLV: the onMetaData script tag (AMF0) that opens the file carries the
// duration, the size and, from ffmpeg, a creation_time string: what ffprobe
// reports as format tags.
func readFLV(path string) (ebmlInfoT, error) {
	var info ebmlInfoT
	f, size, err := openSrc(path)
	if err != nil {
		return info, err
	}
	defer f.Close()
	head := make([]byte, min(size, 1<<20))
	if _, err := f.ReadAt(head, 0); err != nil && !errors.Is(err, io.EOF) {
		return info, err
	}
	if len(head) < 13 || string(head[:3]) != "FLV" {
		return info, errNotFLV
	}
	off := int(binary.BigEndian.Uint32(head[5:9])) + 4 // header, PreviousTagSize0
	if off+11 > len(head) || head[off]&0x1F != 18 {    // first tag must be script data
		return info, errNotFLV
	}
	dataSize := int(head[off+1])<<16 | int(head[off+2])<<8 | int(head[off+3])
	body := head[off+11:]
	if dataSize < len(body) {
		body = body[:dataSize]
	}
	a := &amf0{b: body}
	if name, ok := a.value().(string); !ok || name != "onMetaData" {
		return info, errNotFLV
	}
	m, ok := a.value().(map[string]any)
	if !ok {
		return info, errNotFLV
	}
	if v, ok := m["duration"].(float64); ok {
		info.DurationSec = v
	}
	if v, ok := m["width"].(float64); ok {
		info.Width = int(v)
	}
	if v, ok := m["height"].(float64); ok {
		info.Height = int(v)
	}
	if s, ok := m["creation_time"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			info.Creation = t.UTC()
		}
	}
	return info, nil
}

var errNotFLV = errors.New("not an FLV file with onMetaData")

// amf0 decodes the AMF0 values onMetaData uses (number, boolean, string,
// object, ECMA array, strict array, date, null); anything else stops it.
type amf0 struct {
	b   []byte
	pos int
	bad bool
}

func (a *amf0) need(n int) bool {
	if a.bad || a.pos+n > len(a.b) {
		a.bad = true
		return false
	}
	return true
}

func (a *amf0) str16() string {
	if !a.need(2) {
		return ""
	}
	n := int(binary.BigEndian.Uint16(a.b[a.pos:]))
	a.pos += 2
	if !a.need(n) {
		return ""
	}
	s := string(a.b[a.pos : a.pos+n])
	a.pos += n
	return s
}

func (a *amf0) props() map[string]any {
	m := map[string]any{}
	for !a.bad {
		k := a.str16()
		if a.need(1) && k == "" && a.b[a.pos] == 0x09 { // object end
			a.pos++
			return m
		}
		m[k] = a.value()
	}
	return m
}

func (a *amf0) value() any {
	if !a.need(1) {
		return nil
	}
	t := a.b[a.pos]
	a.pos++
	switch t {
	case 0x00: // number
		if !a.need(8) {
			return nil
		}
		v := math.Float64frombits(binary.BigEndian.Uint64(a.b[a.pos:]))
		a.pos += 8
		return v
	case 0x01: // boolean
		if !a.need(1) {
			return nil
		}
		a.pos++
		return a.b[a.pos-1] != 0
	case 0x02: // string
		return a.str16()
	case 0x03: // object
		return a.props()
	case 0x05, 0x06: // null, undefined
		return nil
	case 0x08: // ECMA array: count, then properties like an object
		if !a.need(4) {
			return nil
		}
		a.pos += 4
		return a.props()
	case 0x0A: // strict array
		if !a.need(4) {
			return nil
		}
		n := int(binary.BigEndian.Uint32(a.b[a.pos:]))
		a.pos += 4
		var out []any
		for i := 0; i < n && !a.bad; i++ {
			out = append(out, a.value())
		}
		return out
	case 0x0B: // date: ms since the epoch + zone
		if !a.need(10) {
			return nil
		}
		ms := math.Float64frombits(binary.BigEndian.Uint64(a.b[a.pos:]))
		a.pos += 10
		return time.UnixMilli(int64(ms)).UTC()
	}
	a.bad = true
	return nil
}

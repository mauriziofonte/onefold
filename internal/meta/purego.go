// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bodgit/sevenzip"
	"github.com/dhowden/tag"
	"github.com/evanoberholster/imagemeta"
	"github.com/richardlehane/mscfb"
	"github.com/richardlehane/msoleps"
	"github.com/richardlehane/msoleps/types"

	"github.com/mauriziofonte/onefold/internal/kinds"
)

// Pure-Go readers: the Windows build ships without external tools, so every
// format onefold dates has a native reader here. External tools remain an
// optional fallback when the native reader cannot parse a file.

var errUnsupported = errors.New("unsupported")

// ------------------------------------------------------------------ 7z

// list7zNative reads 7z headers (compressed headers included) without
// extracting data. Encrypted headers fail -> caller falls back.
func list7zNative(p string, acc *archiveAcc) error {
	r, err := sevenzip.OpenReader(p)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		fi := f.FileInfo()
		mt := f.Modified
		if !mt.IsZero() {
			mt = mt.In(Zone())
		}
		acc.add(f.Name, mt, fi.IsDir())
	}
	return nil
}

// ------------------------------------------------------------------ RAR

var (
	rar4Sig = []byte("Rar!\x1a\x07\x00")
	rar5Sig = []byte("Rar!\x1a\x07\x01\x00")
)

// listRarNative walks RAR4/RAR5 block headers, seeking over packed data (never
// decompresses). Encrypted headers -> errUnsupported (caller falls back).
func listRarNative(p string, acc *archiveAcc) error {
	f, fsize, err := openSrc(p)
	if err != nil {
		return err
	}
	defer f.Close()
	sig := make([]byte, 8)
	if _, err := io.ReadFull(f, sig); err != nil {
		return err
	}
	switch {
	case bytes.Equal(sig[:8], rar5Sig):
		return rar5Walk(f, 8, fsize, acc)
	case bytes.Equal(sig[:7], rar4Sig):
		return rar4Walk(f, 7, fsize, acc)
	}
	return errUnsupported
}

func rar4Walk(f src, off, size int64, acc *archiveAcc) error {
	for n := 0; off+7 <= size && n < 1_000_000; n++ {
		h := make([]byte, 7)
		if _, err := f.ReadAt(h, off); err != nil {
			return err
		}
		typ := h[2]
		flags := binary.LittleEndian.Uint16(h[3:5])
		hsize := int64(binary.LittleEndian.Uint16(h[5:7]))
		if hsize < 7 {
			return errUnsupported
		}
		switch typ {
		case 0x73: // main header
			if flags&0x0080 != 0 {
				return errUnsupported // encrypted block headers
			}
		case 0x7b: // end of archive
			return nil
		}
		add := int64(0)
		if flags&0x8000 != 0 || typ == 0x74 {
			b := make([]byte, 4)
			if _, err := f.ReadAt(b, off+7); err != nil {
				return err
			}
			add = int64(binary.LittleEndian.Uint32(b))
		}
		if typ == 0x74 && hsize >= 32 {
			b := make([]byte, hsize)
			if _, err := f.ReadAt(b, off); err != nil {
				return err
			}
			if flags&0x0100 != 0 && len(b) >= 40 { // 64-bit sizes
				add |= int64(binary.LittleEndian.Uint32(b[32:36])) << 32
			}
			dos := binary.LittleEndian.Uint32(b[20:24])
			nameLen := int(binary.LittleEndian.Uint16(b[26:28]))
			nameOff := 32
			if flags&0x0100 != 0 {
				nameOff = 40
			}
			name := ""
			if nameOff+nameLen <= len(b) {
				raw := b[nameOff : nameOff+nameLen]
				if i := bytes.IndexByte(raw, 0); i >= 0 { // unicode name follows the ASCII one
					raw = raw[:i]
				}
				name = strings.ReplaceAll(string(raw), "\\", "/")
			}
			acc.add(name, dosTime(dos), flags&0x00e0 == 0x00e0)
		}
		off += hsize + add
	}
	return nil
}

// dosTime converts an MS-DOS date/time (local wall clock) to a wall time.
func dosTime(v uint32) time.Time {
	d, t := v>>16, v&0xffff
	if d == 0 {
		return time.Time{}
	}
	return time.Date(int(d>>9)+1980, time.Month((d>>5)&0xf), int(d&0x1f), int(t>>11), int((t>>5)&0x3f), int(t&0x1f)*2, 0, Zone())
}

func rar5Walk(f src, off, size int64, acc *archiveAcc) error {
	for n := 0; off+7 <= size && n < 1_000_000; n++ {
		h := make([]byte, 4+10+64)
		m, _ := f.ReadAt(h, off)
		h = h[:m]
		if len(h) < 6 {
			return nil
		}
		pos := 4
		hsize, k := uvarint(h[pos:])
		if k <= 0 || hsize == 0 || hsize > 2<<20 {
			return errUnsupported
		}
		pos += k
		body := make([]byte, hsize)
		if _, err := f.ReadAt(body, off+int64(pos)); err != nil {
			return err
		}
		next := off + int64(pos) + int64(hsize)
		q := 0
		htype, k := uvarint(body[q:])
		q += k
		hflags, k := uvarint(body[q:])
		q += k
		var extraSize, dataSize uint64
		if hflags&0x1 != 0 {
			extraSize, k = uvarint(body[q:])
			q += k
		}
		if hflags&0x2 != 0 {
			dataSize, k = uvarint(body[q:])
			q += k
		}
		_ = extraSize
		switch htype {
		case 4: // archive encryption header: everything after is encrypted
			return errUnsupported
		case 5: // end of archive
			return nil
		case 2: // file header
			fflags, k := uvarint(body[q:])
			q += k
			_, k = uvarint(body[q:]) // unpacked size
			q += k
			_, k = uvarint(body[q:]) // attributes
			q += k
			var mt time.Time
			if fflags&0x2 != 0 && q+4 <= len(body) {
				mt = time.Unix(int64(binary.LittleEndian.Uint32(body[q:q+4])), 0).In(Zone())
				q += 4
			}
			if fflags&0x4 != 0 {
				q += 4 // data CRC32
			}
			_, k = uvarint(body[q:]) // compression info
			q += k
			_, k = uvarint(body[q:]) // host OS
			q += k
			nl, k := uvarint(body[q:])
			q += k
			name := ""
			if q+int(nl) <= len(body) {
				name = string(body[q : q+int(nl)])
			}
			acc.add(name, mt, fflags&0x1 != 0)
		}
		off = next + int64(dataSize)
	}
	return nil
}

func uvarint(b []byte) (uint64, int) {
	var x uint64
	for i := 0; i < len(b) && i < 10; i++ {
		x |= uint64(b[i]&0x7f) << (7 * i)
		if b[i]&0x80 == 0 {
			return x, i + 1
		}
	}
	return 0, -1
}

// ------------------------------------------------------------------ ISO 9660

// listISONative walks the ISO 9660 directory tree from the primary volume
// descriptor. Recording dates carry their own GMT offset.
func listISONative(p string, acc *archiveAcc) error {
	f, _, err := openSrc(p)
	if err != nil {
		return err
	}
	defer f.Close()
	const sector = 2048
	pvd := make([]byte, sector)
	found := false
	for s := int64(16); s < 64; s++ {
		if _, err := f.ReadAt(pvd, s*sector); err != nil {
			return err
		}
		if string(pvd[1:6]) != "CD001" {
			return errUnsupported
		}
		if pvd[0] == 1 {
			found = true
			break
		}
		if pvd[0] == 255 {
			break
		}
	}
	if !found {
		return errUnsupported
	}
	root := pvd[156 : 156+34]
	type dir struct {
		lba, size uint32
		prefix    string
	}
	queue := []dir{{binary.LittleEndian.Uint32(root[2:6]), binary.LittleEndian.Uint32(root[10:14]), ""}}
	seen := map[uint32]bool{}
	for len(queue) > 0 && len(seen) < 200000 {
		d := queue[0]
		queue = queue[1:]
		if seen[d.lba] || d.size > 64<<20 {
			continue
		}
		seen[d.lba] = true
		buf := make([]byte, d.size)
		if _, err := f.ReadAt(buf, int64(d.lba)*sector); err != nil && err != io.EOF {
			return err
		}
		for pos := 0; pos < len(buf); {
			l := int(buf[pos])
			if l == 0 { // records never cross sectors: jump to the next one
				pos = (pos/sector + 1) * sector
				continue
			}
			if pos+l > len(buf) || l < 34 {
				break
			}
			rec := buf[pos : pos+l]
			nl := int(rec[32])
			if 33+nl > len(rec) {
				break
			}
			name := string(rec[33 : 33+nl])
			pos += l
			if nl == 1 && (name == "\x00" || name == "\x01") {
				continue
			}
			if i := strings.IndexByte(name, ';'); i >= 0 {
				name = name[:i]
			}
			name = strings.TrimSuffix(name, ".")
			full := path.Join(d.prefix, name)
			isDir := rec[25]&0x02 != 0
			if rec[25]&0x80 != 0 { // multi-extent: count the file once (last extent)
				continue
			}
			if isDir {
				queue = append(queue, dir{binary.LittleEndian.Uint32(rec[2:6]), binary.LittleEndian.Uint32(rec[10:14]), full})
				continue
			}
			acc.add(full, isoTime(rec[18:25]), false)
		}
	}
	return nil
}

func isoTime(b []byte) time.Time {
	if b[0] == 0 && b[1] == 0 {
		return time.Time{}
	}
	z := time.FixedZone("iso", int(int8(b[6]))*15*60)
	t := time.Date(1900+int(b[0]), time.Month(b[1]), int(b[2]), int(b[3]), int(b[4]), int(b[5]), 0, z)
	return t.In(Zone())
}

// ------------------------------------------------------------------ OLE (doc/xls/ppt)

// oleMeta reads the SummaryInformation property set of an OLE2 compound file.
func oleMeta(p string, m *Meta) error {
	f, _, err := openSrc(p)
	if err != nil {
		return err
	}
	defer f.Close()
	doc, err := mscfb.New(f)
	if err != nil {
		return err
	}
	for e, err := doc.Next(); err == nil; e, err = doc.Next() {
		if e.Name != "\x05SummaryInformation" {
			continue
		}
		ps, err := msoleps.NewFrom(e)
		if err != nil {
			return err
		}
		for _, pr := range ps.Property {
			if pr == nil || pr.T == nil {
				continue
			}
			switch pr.Name {
			case "Title":
				m.Title = pr.T.String()
			case "Author":
				if m.Author == "" {
					m.Author = pr.T.String()
				}
			case "LastAuthor":
				if m.Author == "" {
					m.Author = pr.T.String()
				}
			case "PageCount":
				m.Pages, _ = strconv.Atoi(strings.TrimSpace(pr.T.String()))
			case "CreateTime":
				if ft, ok := pr.T.(types.FileTime); ok {
					setTime(m, ft.Time(), false)
				}
			}
		}
		return nil
	}
	return errUnsupported
}

// ------------------------------------------------------------------ audio

// audioNative reads ID3v1/v2, MP4, FLAC and Ogg tags; MP4-family audio also
// yields the container creation time and duration.
func audioNative(p, ext string, m *Meta) error {
	f, _, err := openSrc(p)
	if err != nil {
		return err
	}
	md, terr := tag.ReadFrom(f)
	f.Close()
	if terr == nil {
		m.Author, m.Album, m.Title = strings.TrimSpace(md.Artist()), strings.TrimSpace(md.Album()), strings.TrimSpace(md.Title())
		if m.Author == "" {
			m.Author = strings.TrimSpace(md.AlbumArtist())
		}
		if y := md.Year(); y >= 1000 {
			m.TagYear = strconv.Itoa(y)
		}
		m.Track, _ = md.Track()
		m.Disc, _ = md.Disc()
	}
	if f := kinds.Format(ext); f == "isobmff" || f == "aac" {
		if info, err := readISOBMFF(p); err == nil {
			if info.DurationSec > 0 {
				m.DurationSec = info.DurationSec
			}
			if !info.Creation.IsZero() && info.Creation.Year() > 1904 {
				setTime(m, info.Creation, false)
			}
			return nil
		}
	}
	return terr
}

// ------------------------------------------------------------------ images

// HEIFSize returns the size of the primary image of a HEIF, HEIC or AVIF
// file, read from its head (0,0 when it does not say).
func HEIFSize(path string) (w, h int) { return heifDims(head(path, 1<<20)) }

// heifDims returns the largest image spatial extent ('ispe' property) found in
// the head of a HEIF/HEIC/AVIF file: the primary image (a grid) is the largest,
// its tiles and the thumbnail are smaller. 0,0 when none is found.
func heifDims(b []byte) (w, h int) {
	for i := 4; i+16 <= len(b); i++ {
		if b[i] != 'i' || string(b[i:i+4]) != "ispe" || binary.BigEndian.Uint32(b[i-4:i]) != 20 {
			continue
		}
		ww, hh := int(binary.BigEndian.Uint32(b[i+8:i+12])), int(binary.BigEndian.Uint32(b[i+12:i+16]))
		if ww*hh > w*h {
			w, h = ww, hh
		}
	}
	return w, h
}

// setCaptureTime stores an EXIF DateTimeOriginal (wall clock) with its
// sub-second, the value that clusters same-capture photo copies.
func setCaptureTime(m *Meta, dto time.Time) {
	if setTime(m, dto, true) {
		m.SubSecNs = int64(dto.Nanosecond())
		m.SubSec = m.SubSecNs != 0
	}
}

// imageNative decodes EXIF from HEIC/TIFF/camera RAW/PNG/WebP via imagemeta.
func imageNative(p string, m *Meta) error {
	f, _, err := openSrc(p)
	if err != nil {
		return err
	}
	defer f.Close()
	ex, err := imagemeta.Decode(f)
	if err != nil {
		return err
	}
	if dto := ex.DateTimeOriginal(); !dto.IsZero() {
		setCaptureTime(m, dto)
	} else if cd := ex.CreateDate(); !cd.IsZero() {
		setTime(m, cd, true)
	}
	mk, md := strings.TrimSpace(ex.Make), strings.TrimSpace(ex.Model)
	if mk != "" || md != "" || ex.Software != "" {
		m.CanonDevice, m.Category, m.Prefix = CanonicalDevice(mk, md, ex.Software, "")
		m.Device = strings.TrimSpace(mk + " " + md)
	}
	if ex.ImageWidth > 0 && ex.ImageHeight > 0 {
		m.Width, m.Height = int(ex.ImageWidth), int(ex.ImageHeight)
	}
	setGeo(m, ex.GPS.Latitude(), ex.GPS.Longitude())
	return nil
}

var reDSCDate = regexp.MustCompile(`(?m)^%%CreationDate:\s*\(?([^)\r\n]+)\)?`)

// epsDate reads the DSC %%CreationDate comment of EPS/PS files.
func epsDate(p string, m *Meta) error {
	mm := reDSCDate.FindSubmatch(head(p, 64<<10))
	if mm == nil {
		return errUnsupported
	}
	s := strings.TrimSpace(string(mm[1]))
	if strings.HasPrefix(s, "D:") {
		if t, wall, ok := parsePDFDate(s); ok {
			setTime(m, t, wall)
			return nil
		}
	}
	if t, err := parseISOish(s); err == nil {
		setTime(m, t, false)
		return nil
	}
	for _, l := range []string{"Mon Jan _2 15:04:05 2006", "Mon Jan 2 15:04:05 2006", "01/02/06 15:04", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(l, s, Zone()); err == nil {
			setTime(m, t, true)
			return nil
		}
	}
	return errUnsupported
}

// ------------------------------------------------------------------ EPUB

type opfPackage struct {
	Metadata struct {
		Title   []string `xml:"title"`
		Creator []string `xml:"creator"`
		Date    []string `xml:"date"`
		Meta    []struct {
			Property string `xml:"property,attr"`
			Value    string `xml:",chardata"`
		} `xml:"meta"`
	} `xml:"metadata"`
}

// epubMeta reads the OPF package metadata (title, creator, dcterms:modified / dc:date).
func epubMeta(p string, m *Meta) error {
	zr, closeZip, err := openZip(p)
	if err != nil {
		return err
	}
	defer closeZip()
	var container struct {
		Rootfiles []struct {
			Path string `xml:"full-path,attr"`
		} `xml:"rootfiles>rootfile"`
	}
	if !readZipXML(zr, "META-INF/container.xml", &container) || len(container.Rootfiles) == 0 {
		return errUnsupported
	}
	var opf opfPackage
	if !readZipXML(zr, container.Rootfiles[0].Path, &opf) {
		return errUnsupported
	}
	md := opf.Metadata
	if len(md.Title) > 0 {
		m.Title = strings.TrimSpace(md.Title[0])
	}
	if len(md.Creator) > 0 {
		m.Author = strings.TrimSpace(md.Creator[0])
	}
	dates := append([]string(nil), md.Date...)
	for _, mt := range md.Meta {
		if mt.Property == "dcterms:modified" {
			dates = append(dates, mt.Value)
		}
	}
	for _, d := range dates {
		d = strings.TrimSpace(d)
		if t, err := parseISOish(d); err == nil {
			setTime(m, t, false)
			return nil
		}
		if t, err := time.ParseInLocation("2006-01-02", d, Zone()); err == nil {
			setTime(m, t, true)
			return nil
		}
	}
	return nil
}

// ------------------------------------------------------------------ PDF pages

var rePdfPage = regexp.MustCompile(`/Type\s*/Page[^s]`)
var rePdfCount = regexp.MustCompile(`/Type\s*/Pages\b[^>]*?/Count\s+(\d+)|/Count\s+(\d+)[^>]*?/Type\s*/Pages\b`)

// pdfPages estimates the page count without a PDF library: the root /Pages
// /Count (largest seen) or, failing that, the number of /Type /Page objects.
func pdfPages(buf []byte) int {
	best := 0
	for _, mm := range rePdfCount.FindAllSubmatch(buf, -1) {
		s := mm[1]
		if len(s) == 0 {
			s = mm[2]
		}
		if n, err := strconv.Atoi(string(s)); err == nil && n > best {
			best = n
		}
	}
	if best > 0 {
		return best
	}
	return len(rePdfPage.FindAllIndex(buf, -1))
}

// pngExifNative reads the PNG eXIf chunk (a raw TIFF/EXIF stream) via imagemeta.
func pngExifNative(p string, m *Meta) error {
	b := head(p, 32<<20)
	if len(b) < 8 || string(b[:8]) != "\x89PNG\r\n\x1a\n" {
		return errUnsupported
	}
	for pos := 8; pos+12 <= len(b); {
		n := int(binary.BigEndian.Uint32(b[pos : pos+4]))
		typ := string(b[pos+4 : pos+8])
		if n < 0 || pos+12+n > len(b) {
			break
		}
		if typ == "eXIf" {
			ex, err := imagemeta.Decode(bytes.NewReader(b[pos+8 : pos+8+n]))
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
		if typ == "IDAT" || typ == "IEND" {
			break // eXIf must precede image data
		}
		pos += 12 + n
	}
	return errUnsupported
}

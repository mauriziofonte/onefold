// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"encoding/xml"
	"image"
	_ "image/gif" // register GIF for DecodeConfig
	_ "image/png" // register PNG for DecodeConfig
	"io"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/mauriziofonte/onefold/internal/kinds"
	"github.com/mauriziofonte/onefold/internal/tools"
)

// extractGeneric fills metadata for non-media kinds. Time resolution per kind:
// embedded metadata -> filename -> (caller) mtime. Never fails: any parse error
// just degrades to the next source.
func extractGeneric(p, ext, kind string, o Options) Meta {
	m := Meta{Kind: kind, TimeSource: "none"}
	if !o.Quick {
		switch kind {
		case kinds.Document:
			extractDocument(p, ext, &m)
		case kinds.Audio:
			extractAudio(p, ext, &m)
		case kinds.Image:
			extractImage(p, ext, &m)
		case kinds.Archive:
			extractArchive(p, ext, &m)
		}
	}
	m.Title, m.Author, m.Album = cleanText(m.Title), cleanText(m.Author), cleanText(m.Album)
	if m.Time == "" {
		if t, ok := DeduceFromFilename(filepath.Base(p)); ok {
			m.Time, m.TimeSource = t, "filename"
		}
	}
	return m
}

// cleanText drops NULs/control chars (pdfinfo emits UTF-16 leftovers) and
// collapses whitespace.
func cleanText(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			if r == '\t' || r == '\n' || r == '\r' {
				return ' '
			}
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}

// setTime stores a canonical time if it passes the validity clamp.
func setTime(m *Meta, t time.Time, wallClock bool) bool {
	if t.IsZero() {
		return false
	}
	if !wallClock {
		t = t.In(Zone())
	}
	if !yearOK(t.Year()) {
		return false
	}
	m.Time, m.TimeSource = t.Format(canonFmt), "metadata"
	return true
}

// ---------------------------------------------------------------- documents

func extractDocument(p, ext string, m *Meta) {
	switch kinds.Format(ext) {
	case "pdf":
		if !pdfInfo(p, m) {
			pdfScan(p, m)
		}
	case "epub":
		if epubMeta(p, m) != nil || m.Time == "" {
			exifDoc(p, m)
		}
	case "ole-doc", "ole-sheet", "ole-slides":
		if oleMeta(p, m) != nil || m.Time == "" {
			exifDoc(p, m)
		}
	case "ooxml-doc", "ooxml-sheet", "ooxml-slides":
		ooxmlMeta(p, m)
	case "odf-doc", "odf-sheet", "odf-slides":
		odfMeta(p, m)
	case "rtf":
		rtfMeta(p, m)
	default:
		exifDoc(p, m)
	}
}

// pdfInfo uses pdfinfo, poppler's or xpdf's (the Windows download): both take
// -rawdates and -enc, so the raw "D:..." date goes through parsePDFDate, the
// parser of the pure-Go path (one reading of zoneless dates everywhere). false
// if the tool is missing or fails.
func pdfInfo(p string, m *Meta) bool {
	out, ok, err := tools.Output(nil, "pdfinfo", "-rawdates", "-enc", "UTF-8", p)
	if !ok || err != nil {
		return false
	}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		k, v, ok := strings.Cut(sc.Text(), ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch strings.TrimSpace(k) {
		case "Title":
			m.Title = v
		case "Author":
			m.Author = v
		case "Pages":
			m.Pages, _ = strconv.Atoi(v)
		case "CreationDate":
			if t, wall, ok := parsePDFDate(v); ok {
				setTime(m, t, wall)
			}
		}
	}
	return true
}

var (
	rePdfCreation = regexp.MustCompile(`/CreationDate\s*\(\s*(D:[0-9]{4,14}[^)]*)\)`)
	rePdfAuthor   = regexp.MustCompile(`/Author\s*(\((?:\\.|[^\\)]){0,400}\)|<[0-9A-Fa-f\s]{0,1000}>)`)
	rePdfTitle    = regexp.MustCompile(`/Title\s*(\((?:\\.|[^\\)]){0,600}\)|<[0-9A-Fa-f\s]{0,1000}>)`)
	reXmpCreate   = regexp.MustCompile(`xmp:CreateDate(?:="|>)([0-9]{4}-[0-9]{2}-[0-9]{2}T[^"<]+)`)
)

// pdfScan is the pure-Go fallback: regex over the head and tail of the file
// (the Info dictionary and XMP packet usually live there, uncompressed).
func pdfScan(p string, m *Meta) {
	// Progressive: the Info dictionary (trailer) and the XMP packet usually sit
	// in the last or first few KB; widen to 2 MB only when the date is missing.
	buf := headTail(p, 64<<10)
	if !rePdfCreation.Match(buf) && !reXmpCreate.Match(buf) {
		buf = headTail(p, 2<<20)
	}
	m.Pages = pdfPages(buf)
	if mm := rePdfCreation.FindSubmatch(buf); mm != nil {
		if t, wall, ok := parsePDFDate(string(mm[1])); ok {
			setTime(m, t, wall)
		}
	}
	if m.Time == "" {
		if mm := reXmpCreate.FindSubmatch(buf); mm != nil {
			if t, err := parseISOish(string(mm[1])); err == nil {
				setTime(m, t, false)
			}
		}
	}
	if mm := rePdfAuthor.FindSubmatch(buf); mm != nil {
		m.Author = pdfString(mm[1])
	}
	if mm := rePdfTitle.FindSubmatch(buf); mm != nil {
		m.Title = pdfString(mm[1])
	}
}

// parsePDFDate parses "D:YYYYMMDDHHmmSS+HH'mm'" (fields after the year optional).
// A date without offset is UTC (poppler parity), so wall is always false.
func parsePDFDate(s string) (time.Time, bool, bool) {
	s = strings.TrimPrefix(s, "D:")
	digits := 0
	for digits < len(s) && digits < 14 && s[digits] >= '0' && s[digits] <= '9' {
		digits++
	}
	num := s[:digits] + "00000000000000"[digits:]
	if digits < 4 {
		return time.Time{}, false, false
	}
	a := func(i, j, def int) int {
		n, err := strconv.Atoi(num[i:j])
		if err != nil || (n == 0 && def != 0) {
			return def
		}
		return n
	}
	y, mo, d := a(0, 4, 0), a(4, 6, 1), a(6, 8, 1)
	h, mi, se := a(8, 10, 0), a(10, 12, 0), a(12, 14, 0)
	rest := s[digits:]
	// No zone: the spec says "unknown"; poppler (pdfinfo) assumes UTC, and so do
	// we for parity between the Linux (pdfinfo) and Windows (pure-Go) paths.
	loc, wall := time.UTC, false
	if len(rest) > 0 && (rest[0] == '+' || rest[0] == '-' || rest[0] == 'Z') {
		if rest[0] != 'Z' {
			off := strings.NewReplacer("'", "").Replace(rest[1:])
			oh, _ := strconv.Atoi(pad2(off, 0))
			om, _ := strconv.Atoi(pad2(off, 2))
			sec := (oh*60 + om) * 60
			if rest[0] == '-' {
				sec = -sec
			}
			loc = time.FixedZone("pdf", sec)
		}
	}
	return time.Date(y, time.Month(mo), d, h, mi, se, 0, loc), wall, true
}

// pdfString decodes a PDF literal "(...)" or hex "<...>" string, including
// escapes, octal codes and UTF-16BE (BOM FE FF); otherwise PDFDocEncoding ~ Latin-1.
func pdfString(raw []byte) string {
	var b []byte
	switch {
	case len(raw) >= 2 && raw[0] == '<':
		hex := strings.Map(func(r rune) rune {
			if strings.ContainsRune("0123456789abcdefABCDEF", r) {
				return r
			}
			return -1
		}, string(raw[1:len(raw)-1]))
		if len(hex)%2 == 1 {
			hex += "0"
		}
		for i := 0; i+1 < len(hex); i += 2 {
			v, _ := strconv.ParseUint(hex[i:i+2], 16, 8)
			b = append(b, byte(v))
		}
	case len(raw) >= 2 && raw[0] == '(':
		in := raw[1 : len(raw)-1]
		for i := 0; i < len(in); i++ {
			c := in[i]
			if c != '\\' || i+1 >= len(in) {
				b = append(b, c)
				continue
			}
			i++
			switch e := in[i]; e {
			case 'n':
				b = append(b, '\n')
			case 'r':
				b = append(b, '\r')
			case 't':
				b = append(b, '\t')
			case 'b', 'f':
			case '0', '1', '2', '3', '4', '5', '6', '7':
				j := i
				for j < len(in) && j < i+3 && in[j] >= '0' && in[j] <= '7' {
					j++
				}
				v, _ := strconv.ParseUint(string(in[i:j]), 8, 8)
				b = append(b, byte(v))
				i = j - 1
			case '\r', '\n':
			default:
				b = append(b, e)
			}
		}
	default:
		return ""
	}
	if len(b) >= 2 && b[0] == 0xfe && b[1] == 0xff {
		u := make([]uint16, 0, len(b)/2)
		for i := 2; i+1 < len(b); i += 2 {
			u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
		}
		return cleanText(string(utf16.Decode(u)))
	}
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return cleanText(string(r))
}

func pad2(s string, i int) string {
	if len(s) >= i+2 {
		return s[i : i+2]
	}
	return "00"
}

// parseISOish accepts RFC3339 and the common short-offset / offset-less variants.
func parseISOish(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, l := range []string{time.RFC3339Nano, "2006-01-02T15:04:05-07", "2006-01-02T15:04:05-0700", "2006-01-02T15:04:05.999999999"} {
		if t, err := time.Parse(l, s); err == nil {
			return t, nil
		}
	}
	return time.ParseInLocation("2006-01-02T15:04:05", s, Zone())
}

type ooxmlCore struct {
	Title          string `xml:"title"`
	Creator        string `xml:"creator"`
	LastModifiedBy string `xml:"lastModifiedBy"`
	Created        string `xml:"created"`
}

type ooxmlApp struct {
	Pages  int `xml:"Pages"`
	Slides int `xml:"Slides"`
}

// ooxmlMeta reads docProps/core.xml (+app.xml) from a docx/xlsx/pptx. Pure-Go.
func ooxmlMeta(p string, m *Meta) {
	zr, closeZip, err := openZip(p)
	if err != nil {
		return
	}
	defer closeZip()
	var core ooxmlCore
	var app ooxmlApp
	readZipXML(zr, "docProps/core.xml", &core)
	readZipXML(zr, "docProps/app.xml", &app)
	m.Title = strings.TrimSpace(core.Title)
	m.Author = firstNonEmpty(strings.TrimSpace(core.Creator), strings.TrimSpace(core.LastModifiedBy))
	m.Pages = app.Pages
	if m.Pages == 0 {
		m.Pages = app.Slides
	}
	if t, err := parseISOish(core.Created); err == nil {
		setTime(m, t, false)
	}
}

type odfMetaDoc struct {
	Meta struct {
		Title          string `xml:"title"`
		InitialCreator string `xml:"initial-creator"`
		Creator        string `xml:"creator"`
		CreationDate   string `xml:"creation-date"`
		Stats          struct {
			Pages int `xml:"page-count,attr"`
		} `xml:"document-statistic"`
	} `xml:"meta"`
}

// odfMeta reads meta.xml from an odt/ods/odp. creation-date is wall clock.
func odfMeta(p string, m *Meta) {
	zr, closeZip, err := openZip(p)
	if err != nil {
		return
	}
	defer closeZip()
	var d odfMetaDoc
	readZipXML(zr, "meta.xml", &d)
	m.Title = strings.TrimSpace(d.Meta.Title)
	m.Author = firstNonEmpty(strings.TrimSpace(d.Meta.InitialCreator), strings.TrimSpace(d.Meta.Creator))
	m.Pages = d.Meta.Stats.Pages
	if s := strings.TrimSpace(d.Meta.CreationDate); s != "" {
		if len(s) > 19 && !strings.ContainsAny(s[19:], "Z+-") {
			s = s[:19] // drop fractional seconds of a wall-clock value
		}
		if t, err := time.ParseInLocation("2006-01-02T15:04:05", s, Zone()); err == nil {
			setTime(m, t, true)
		} else if t, err := parseISOish(s); err == nil {
			setTime(m, t, false)
		}
	}
}

func readZipXML(zr *zip.Reader, name string, v any) bool {
	for _, f := range zr.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return false
		}
		defer rc.Close()
		return xml.NewDecoder(io.LimitReader(rc, 4<<20)).Decode(v) == nil
	}
	return false
}

var (
	reRtfCreat  = regexp.MustCompile(`\\creatim\\yr(\d+)\\mo(\d+)\\dy(\d+)(?:\\hr(\d+))?(?:\\min(\d+))?(?:\\sec(\d+))?`)
	reRtfTitle  = regexp.MustCompile(`\{\\title ([^}]{1,300})\}`)
	reRtfAuthor = regexp.MustCompile(`\{\\author ([^}]{1,200})\}`)
)

// rtfMeta parses the RTF \info group (pure-Go). \creatim is wall clock.
func rtfMeta(p string, m *Meta) {
	buf := head(p, 256<<10)
	if mm := reRtfCreat.FindSubmatch(buf); mm != nil {
		n := func(b []byte) int { v, _ := strconv.Atoi(string(b)); return v }
		t := time.Date(n(mm[1]), time.Month(n(mm[2])), n(mm[3]), n(mm[4]), n(mm[5]), n(mm[6]), 0, Zone())
		setTime(m, t, true)
	}
	if mm := reRtfTitle.FindSubmatch(buf); mm != nil {
		m.Title = strings.TrimSpace(string(mm[1]))
	}
	if mm := reRtfAuthor.FindSubmatch(buf); mm != nil {
		m.Author = strings.TrimSpace(string(mm[1]))
	}
}

// ---------------------------------------------------------------- exiftool

// exifJSON runs exiftool with raw date strings (no -d: -d drops the zone of
// values like "2026:01:27 14:54:43+00:00"), QuickTimeUTC and TZ set to the canonical zone.
// nil if exiftool is absent or fails.
func exifJSON(p string, tags ...string) map[string]any {
	args := append([]string{"-m", "-q", "-fast2", "-j", "-api", "QuickTimeUTC"}, tags...)
	out, ok := exifOutput(append(args, p))
	if !ok {
		var err error
		if out, ok, err = tools.Output([]string{"TZ=" + Zone().String()}, "exiftool", append(args, p)...); !ok || err != nil {
			return nil
		}
	}
	var arr []map[string]any
	if json.Unmarshal(out, &arr) != nil || len(arr) == 0 {
		return nil
	}
	return arr[0]
}

func exifTime(t map[string]any, m *Meta, keys ...string) {
	for _, k := range keys {
		if tt, wall, ok := parseExifDate(asStr(t[k])); ok && setTime(m, tt, wall) {
			return
		}
	}
}

// parseExifDate parses exiftool date strings: "YYYY:MM:DD HH:MM:SS" (wall
// clock), optionally with ".frac" and a zone ("Z", "+hh:mm", "-hhmm"), in which
// case it is an instant (wall=false). Separators ':' / '-' / 'T' are accepted.
func parseExifDate(s string) (t time.Time, wall, ok bool) {
	s = strings.TrimSpace(s)
	if len(s) < 19 || strings.HasPrefix(s, "0000") {
		return time.Time{}, false, false
	}
	b := []byte(s[:19])
	b[4], b[7], b[10] = '-', '-', ' '
	base, err := time.ParseInLocation(canonFmt, string(b), Zone())
	if err != nil {
		return time.Time{}, false, false
	}
	rest := s[19:]
	if strings.HasPrefix(rest, ".") {
		i := 1
		for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
			i++
		}
		rest = rest[i:]
	}
	rest = strings.TrimSpace(rest)
	switch {
	case rest == "":
		return base, true, true
	case rest == "Z":
		return time.Date(base.Year(), base.Month(), base.Day(), base.Hour(), base.Minute(), base.Second(), 0, time.UTC), false, true
	case rest[0] == '+' || rest[0] == '-':
		off := strings.ReplaceAll(rest[1:], ":", "")
		if len(off) < 4 {
			return base, true, true
		}
		h, e1 := strconv.Atoi(off[:2])
		mi, e2 := strconv.Atoi(off[2:4])
		if e1 != nil || e2 != nil {
			return base, true, true
		}
		sec := (h*60 + mi) * 60
		if rest[0] == '-' {
			sec = -sec
		}
		z := time.FixedZone("exif", sec)
		return time.Date(base.Year(), base.Month(), base.Day(), base.Hour(), base.Minute(), base.Second(), 0, z), false, true
	}
	return base, true, true
}

func exifDoc(p string, m *Meta) {
	t := exifJSON(p, "-CreateDate", "-Author", "-Creator", "-Title", "-Pages", "-LastModifiedBy")
	if t == nil {
		return
	}
	exifTime(t, m, "CreateDate")
	m.Title = asStr(t["Title"])
	m.Author = firstNonEmpty(asStr(t["Author"]), firstNonEmpty(asStr(t["Creator"]), asStr(t["LastModifiedBy"])))
	m.Pages = asInt(t["Pages"])
}

func extractAudio(p, ext string, m *Meta) {
	if audioNative(p, ext, m) == nil {
		return
	}
	exifAudio(p, m)
}

func exifAudio(p string, m *Meta) {
	t := exifJSON(p, "-Artist", "-Album", "-Title", "-Year", "-RecordingTime", "-ContentCreateDate", "-Track", "-TrackNumber",
		"-PartOfSet", "-DiscNumber", "-Duration#", "-QuickTime:CreateDate")
	if t == nil {
		return
	}
	m.Author, m.Album, m.Title = asStr(t["Artist"]), asStr(t["Album"]), asStr(t["Title"])
	for _, k := range []string{"Year", "RecordingTime", "ContentCreateDate"} {
		if y := leadingInt(anyStr(t[k])); y >= 1000 {
			m.TagYear = strconv.Itoa(y)
			break
		}
	}
	m.Track = leadingInt(firstNonEmpty(anyStr(t["Track"]), anyStr(t["TrackNumber"])))
	m.Disc = leadingInt(firstNonEmpty(anyStr(t["PartOfSet"]), anyStr(t["DiscNumber"])))
	if d, ok := t["Duration"].(float64); ok {
		m.DurationSec = d
	}
	// Only a container creation time is a capture time (m4a voice memos). ID3
	// Year/RecordingTime is the RELEASE date of music: never used as file time.
	exifTime(t, m, "CreateDate")
}

func extractImage(p, ext string, m *Meta) {
	switch kinds.Format(ext) {
	case "eps":
		_ = epsDate(p, m)
	case "png":
		_ = pngExifNative(p, m)
	case "webp":
		_ = webpNative(p, m)
	case "heif":
		if imageNative(p, m) != nil || m.Time == "" {
			_ = heifExifNative(p, m) // AVIF: imagemeta finds no EXIF
		}
	case "gif", "bmp", "ico", "svg":
		// no embedded capture date in these formats worth a native reader
	default:
		_ = imageNative(p, m)
	}
	if m.Time != "" {
		imageDims(p, ext, m)
		return
	}
	t := exifJSON(p, "-DateTimeOriginal", "-SubSecTimeOriginal", "-CreateDate", "-Make", "-Model", "-Software", "-CreatorTool", "-ImageWidth", "-ImageHeight")
	if t != nil {
		exifTime(t, m, "DateTimeOriginal", "CreateDate")
		if dto, wall, ok := parseExifDate(asStr(t["DateTimeOriginal"])); ok && wall && m.Time == dto.Format(canonFmt) {
			m.SubSecNs = subSecNs(anyStr(t["SubSecTimeOriginal"]))
			m.SubSec = m.SubSecNs != 0
		}
		mk, md := asStr(t["Make"]), asStr(t["Model"])
		m.CanonDevice, m.Category, m.Prefix = CanonicalDevice(mk, md, asStr(t["Software"]), asStr(t["CreatorTool"]))
		m.Device = strings.TrimSpace(mk + " " + md)
		if w, h := asInt(t["ImageWidth"]), asInt(t["ImageHeight"]); w > 0 && h > 0 {
			m.Width, m.Height = w, h
		}
	}
	imageDims(p, ext, m)
}

// subSecNs converts an EXIF SubSecTime ("457" = .457 s) to nanoseconds.
func subSecNs(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 9 {
		return 0
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	for i := len(s); i < 9; i++ {
		n *= 10
	}
	return n
}

// imageDims fills missing dimensions from the image header (png/gif: stdlib;
// HEIF/HEIC/AVIF: the largest 'ispe' property, i.e. the primary image).
func imageDims(p, ext string, m *Meta) {
	if m.Width != 0 && m.Height != 0 {
		return
	}
	if kinds.Format(ext) == "heif" {
		m.Width, m.Height = heifDims(head(p, 1<<20))
		return
	}
	if f := kinds.Format(ext); f == "png" || f == "gif" {
		if f, _, err := openSrc(p); err == nil {
			if cfg, _, err := image.DecodeConfig(f); err == nil {
				m.Width, m.Height = cfg.Width, cfg.Height
			}
			f.Close()
		}
	}
}

// anyStr renders a JSON scalar (exiftool emits bare numbers for some tags).
func anyStr(v any) string {
	if f, ok := v.(float64); ok {
		return strconv.Itoa(int(f))
	}
	return asStr(v)
}

func leadingInt(s string) int {
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	n, _ := strconv.Atoi(s[:i])
	return n
}

// ---------------------------------------------------------------- archives

// archiveAcc collects the newest non-junk member time: an archive's effective
// date is when its newest content was last modified.
type archiveAcc struct {
	max     time.Time // wall clock (location irrelevant)
	entries int
}

func (a *archiveAcc) add(name string, wall time.Time, dir bool) {
	if dir || name == "" || kinds.IsArchiveJunk(name) {
		return
	}
	a.entries++
	if wall.After(a.max) {
		a.max = wall
	}
}

func (a *archiveAcc) apply(m *Meta) {
	m.Entries = a.entries
	if !a.max.IsZero() {
		w := a.max
		setTime(m, time.Date(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), w.Second(), 0, Zone()), true)
	}
}

func extractArchive(p, ext string, m *Meta) {
	var acc archiveAcc
	lower := strings.ToLower(p)
	switch kinds.Format(ext) {
	case "zip":
		zr, closeZip, err := openZip(p)
		if err != nil {
			return
		}
		for _, f := range zr.File {
			acc.add(f.Name, f.Modified, f.FileInfo().IsDir()) // Modified wall clock = stored local time
		}
		closeZip()
	case "tar":
		// Uncompressed: archive/tar seeks over member data (headers only).
		f, _, err := openSrc(p)
		if err != nil {
			return
		}
		defer f.Close()
		tr := tar.NewReader(f)
		for {
			h, err := tr.Next()
			if err != nil {
				break
			}
			acc.add(h.Name, h.ModTime.In(Zone()), h.Typeflag == tar.TypeDir)
		}
	case "gzip":
		if gzipArchive(p, kinds.NormalizeExt(ext) == "tgz" || strings.HasSuffix(lower, ".tar.gz"), &acc, m) {
			return // dated from the gzip header, member list incomplete
		}
	case "7z":
		if list7zNative(p, &acc) != nil {
			acc = archiveAcc{}
			list7z(p, &acc)
		}
	case "iso":
		if listISONative(p, &acc) != nil {
			acc = archiveAcc{}
			list7z(p, &acc)
		}
	case "rar":
		if listRarNative(p, &acc) != nil {
			acc = archiveAcc{}
			listRar(p, &acc)
		}
	default:
		return // bz2/xz: single stream, no member times
	}
	acc.apply(m)
}

// gzBudget caps the compressed bytes inflated to list a .tar.gz: a gzip stream
// has no index, so a full listing inflates the whole archive (measured: 395 s
// for one backup on a SATA QLC disk).
var gzBudget int64 = 64 << 20

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
	c.n += int64(n)
	return n, err
}

// gzipArchive lists a (tar.)gz. Returns true when it set the time itself from
// the gzip header because the member listing was cut by gzBudget (or the
// stream is not a tar). Otherwise acc holds a complete listing.
func gzipArchive(p string, isTar bool, acc *archiveAcc, m *Meta) bool {
	f, _, err := openSrc(p)
	if err != nil {
		return false
	}
	defer f.Close()
	cr := &countingReader{r: f}
	gz, err := gzip.NewReader(cr)
	if err != nil {
		return false
	}
	defer gz.Close()
	hdrTime := gz.Header.ModTime
	fromHeader := func() bool {
		*acc = archiveAcc{}
		if !hdrTime.IsZero() && hdrTime.Unix() > 0 {
			setTime(m, hdrTime, false)
		}
		return true
	}
	if !isTar {
		return fromHeader()
	}
	tr := tar.NewReader(gz)
	for {
		if cr.n > gzBudget {
			// incomplete: prefer the gzip header time, else the newest member seen
			if !hdrTime.IsZero() && hdrTime.Unix() > 0 {
				return fromHeader()
			}
			acc.entries = 0
			return false
		}
		h, err := tr.Next()
		if err != nil {
			return false
		}
		acc.add(h.Name, h.ModTime.In(Zone()), h.Typeflag == tar.TypeDir)
	}
}

// list7z parses `7z l -slt` (technical listing). A dummy password avoids the
// interactive prompt on encrypted archives.
func list7z(p string, acc *archiveAcc) {
	// 7z renders times with the CURRENT DST offset, not the one of the date.
	// p7zip / 7-Zip for Linux honour TZ=UTC; 7-Zip for Windows ignores it and
	// prints local time: undo today's offset (sevenZipUTC).
	args := []string{"l", "-slt", "-phello", "-y"}
	if runtime.GOOS == "windows" {
		args = append(args, "-sccUTF-8") // names in UTF-8, not the OEM code page
	}
	out, ok, err := tools.Output([]string{"TZ=UTC"}, "7z", append(args, p)...)
	if !ok || (err != nil && len(out) == 0) {
		return
	}
	var name, mod string
	var dir, inList bool
	flush := func() {
		if name != "" {
			if t, err := time.Parse("2006-01-02 15:04:05", mod); err == nil {
				acc.add(name, sevenZipUTC(t, runtime.GOOS, time.Now()).In(Zone()), dir)
			} else if mod == "" {
				acc.add(name, time.Time{}, dir)
			}
		}
		name, mod, dir = "", "", false
	}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "----------") {
			inList = true
			continue
		}
		if !inList {
			continue
		}
		k, v, _ := strings.Cut(line, " = ")
		switch k {
		case "Path":
			flush()
			name = v
		case "Modified":
			if len(v) >= 19 {
				mod = v[:19]
			}
		case "Folder":
			dir = v == "+"
		case "Attributes":
			dir = dir || strings.HasPrefix(v, "D")
		}
	}
	flush()
}

// sevenZipUTC turns a 7z listing time (parsed as UTC) into the real instant:
// on Windows 7z printed local time shifted by the offset in force now.
func sevenZipUTC(t time.Time, goos string, now time.Time) time.Time {
	if goos != "windows" {
		return t
	}
	_, off := now.Zone()
	return t.Add(-time.Duration(off) * time.Second)
}

// listRar parses `unrar lt` (technical listing).
func listRar(p string, acc *archiveAcc) {
	// RAR4 stores DOS local time (printed as-is); RAR5 stores UTC, rendered in TZ.
	out, ok, err := tools.Output([]string{"TZ=" + Zone().String()}, "unrar", "lt", "-phello", "-c-", p)
	if !ok || (err != nil && len(out) == 0) {
		return
	}
	var name, mod string
	var dir bool
	flush := func() {
		if name != "" {
			if t, err := time.Parse("2006-01-02 15:04:05", mod); err == nil {
				acc.add(name, t, dir)
			}
		}
		name, mod, dir = "", "", false
	}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), ": ")
		if !ok {
			continue
		}
		switch k {
		case "Name":
			flush()
			name = v
		case "Type":
			dir = v == "Directory"
		case "mtime":
			if len(v) >= 19 {
				mod = v[:19]
			}
		}
	}
	flush()
}

// ---------------------------------------------------------------- io helpers

func head(p string, n int64) []byte {
	if b, ok := preloadedHead(p, int(n)); ok {
		return b
	}
	f, _, err := openSrc(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	b, _ := io.ReadAll(io.LimitReader(f, n))
	return b
}

// headTail returns the first and last n bytes of p (whole file if small).
func headTail(p string, n int64) []byte {
	if v, ok := preloaded.Load(p); ok {
		b := v.([]byte)
		if int64(len(b)) <= 2*n {
			return b
		}
		return append(append([]byte(nil), b[:n]...), b[int64(len(b))-n:]...)
	}
	f, fsize, err := openSrc(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	if fsize <= 2*n {
		b, _ := io.ReadAll(f)
		return b
	}
	h := make([]byte, n)
	io.ReadFull(f, h)
	t := make([]byte, n)
	if _, err := f.ReadAt(t, fsize-n); err != nil && err != io.EOF {
		return h
	}
	return append(h, t...)
}

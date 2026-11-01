// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package textexport writes a plain-text sidecar (metadata header + extracted
// text) for document/text golden assets.
// Pure-Go for txt/md/csv/html/docx/xlsx/pptx/odt/ods/odp/epub/rtf; pdftotext
// and antiword are used when present for pdf/doc. READ-ONLY on sources.
package textexport

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mauriziofonte/onefold/internal/fsx"
	"github.com/mauriziofonte/onefold/internal/kinds"
	"github.com/mauriziofonte/onefold/internal/meta"
	"github.com/mauriziofonte/onefold/internal/plan"
	"github.com/mauriziofonte/onefold/internal/tools"
)

const maxRead = 32 << 20

// NoContent is written when nothing could be extracted.
const NoContent = "[No text content could be extracted]"

// Eligible reports whether an action gets a text sidecar.
func Eligible(a plan.Action) bool { return a.Kind == kinds.Document || a.Kind == kinds.Text }

// Result of exporting one action.
type Result struct {
	Dst     string
	Skipped bool // sidecar already exists
	Empty   bool // no text extracted
}

// Export writes target/<canonRel>.txt. Idempotent (skip if present), atomic.
func Export(a plan.Action, target string) (Result, error) {
	dst := filepath.Join(target, filepath.FromSlash(a.CanonRel)) + ".txt"
	if _, err := os.Stat(dst); err == nil {
		return Result{Dst: dst, Skipped: true}, nil
	}
	text := Extract(a.SrcPath)
	md, _ := meta.Extract(a.SrcPath)
	var b strings.Builder
	line := strings.Repeat("=", 80)
	fmt.Fprintf(&b, "%s\nTEXT EXPORT - onefold\n%s\n\nORIGINAL FILE:\n", line, line)
	fmt.Fprintf(&b, "  Filename: %s\n  Path: %s:%s\n  Size: %d bytes\n  Extension: %s\n  Golden: %s\n\n",
		filepath.Base(a.SrcPath), a.SrcHost, a.SrcPath, a.SrcSize, strings.ToUpper(kinds.Ext(a.SrcPath)), a.CanonRel)
	fmt.Fprintf(&b, "DOCUMENT METADATA:\n  Date: %s (%s)\n", a.Time, md.TimeSource)
	for _, kv := range [][2]string{{"Title", md.Title}, {"Author", md.Author}} {
		if kv[1] != "" {
			fmt.Fprintf(&b, "  %s: %s\n", kv[0], kv[1])
		}
	}
	if md.Pages > 0 {
		fmt.Fprintf(&b, "  Pages: %d\n", md.Pages)
	}
	dash := strings.Repeat("-", 80)
	fmt.Fprintf(&b, "\n%s\nCONTENT:\n%s\n\n", dash, dash)
	empty := strings.TrimSpace(text) == ""
	if empty {
		b.WriteString(NoContent)
	} else {
		b.WriteString(text)
	}
	fmt.Fprintf(&b, "\n\n%s\nEND OF DOCUMENT\n%s\n", line, line)

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return Result{Dst: dst}, err
	}
	tmp := fsx.TempFor(dst, false)
	if err := writeSynced(tmp, b.String()); err != nil {
		os.Remove(tmp)
		return Result{Dst: dst}, err
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", a.Time, meta.Zone()); err == nil {
		_ = os.Chtimes(tmp, t, t)
	}
	if err := fsx.RenameNoReplace(tmp, dst); err != nil {
		os.Remove(tmp)
		if errors.Is(err, fsx.ErrExists) {
			return Result{Dst: dst, Skipped: true}, nil
		}
		return Result{Dst: dst}, err
	}
	return Result{Dst: dst, Empty: empty}, nil
}

// writeSynced writes s to p (created or truncated) and syncs it to disk.
func writeSynced(p, s string) error {
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(s); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Extract returns the plain text of a document/text file ("" if not extractable).
func Extract(p string) string {
	ext := kinds.Ext(p)
	var s string
	switch kinds.Format(ext) {
	case "ooxml-doc":
		s = zipXMLText(p, func(n string) bool { return n == "word/document.xml" })
	case "ooxml-sheet":
		s = xlsxText(p)
	case "ooxml-slides":
		s = zipXMLText(p, func(n string) bool { return strings.HasPrefix(n, "ppt/slides/slide") && strings.HasSuffix(n, ".xml") })
	case "odf-doc", "odf-slides":
		s = zipXMLText(p, func(n string) bool { return n == "content.xml" })
	case "odf-sheet":
		s = odsText(p)
	case "epub":
		s = zipXMLText(p, func(n string) bool {
			l := strings.ToLower(n)
			return strings.HasSuffix(l, ".xhtml") || strings.HasSuffix(l, ".html") || strings.HasSuffix(l, ".htm")
		})
	case "pdf":
		s = run("pdftotext", "-q", "-enc", "UTF-8", "-nopgbrk", p, "-")
	case "ole-doc":
		// pure Go first (Windows has no antiword), antiword for what it cannot read
		if t, err := docText(p); err == nil && strings.TrimSpace(t) != "" {
			s = t
		} else {
			s = run("antiword", p)
		}
	case "ole-sheet":
		s, _ = xlsText(p)
	case "ole-slides":
		s, _ = pptText(p)
	case "rtf":
		s = rtfText(readAll(p))
	case "html":
		s = htmlText(readAll(p))
	default:
		if kinds.KindOf(ext) == kinds.Text {
			s = readAll(p)
		}
	}
	return clean(s)
}

func readAll(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	b, _ := io.ReadAll(io.LimitReader(f, maxRead))
	return string(b)
}

func run(name string, args ...string) string {
	out, ok, err := tools.Output(nil, name, args...)
	if !ok || err != nil {
		return ""
	}
	return string(out)
}

// clean forces valid UTF-8, normalizes newlines and collapses blank-line runs.
func clean(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\x00", "")
	s = regexp.MustCompile(`\n{3,}`).ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// blockEnds are XML elements (local names) after which a newline is emitted:
// paragraphs/headings (w:p, text:p, text:h, a:p), table rows, shared strings.
var blockEnds = map[string]bool{"p": true, "h": true, "tr": true, "si": true, "br": true, "div": true, "li": true}

// zipXMLText concatenates the character data of the matching zip members.
func zipXMLText(p string, match func(string) bool) string {
	zr, err := zip.OpenReader(p)
	if err != nil {
		return ""
	}
	defer zr.Close()
	var names []string
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		if match(f.Name) {
			names = append(names, f.Name)
			files[f.Name] = f
		}
	}
	sort.Slice(names, func(i, j int) bool { return naturalLess(names[i], names[j]) })
	var sb strings.Builder
	for _, n := range names {
		rc, err := files[n].Open()
		if err != nil {
			continue
		}
		dec := xml.NewDecoder(io.LimitReader(rc, maxRead))
		dec.Strict = false
		dec.AutoClose = xml.HTMLAutoClose
		dec.Entity = xml.HTMLEntity
		for {
			tok, err := dec.Token()
			if err != nil {
				break
			}
			switch t := tok.(type) {
			case xml.CharData:
				sb.Write(t)
			case xml.EndElement:
				if blockEnds[t.Name.Local] {
					sb.WriteByte('\n')
				}
			case xml.StartElement:
				if t.Name.Local == "tab" {
					sb.WriteByte('\t')
				}
			}
		}
		rc.Close()
		sb.WriteByte('\n')
	}
	return sb.String()
}

// odsText renders every sheet row-wise: cells joined by TAB, rows by newline.
func odsText(p string) string {
	zr, err := zip.OpenReader(p)
	if err != nil {
		return ""
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name != "content.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return ""
		}
		defer rc.Close()
		var sb, cell strings.Builder
		var row []string
		inCell := false
		dec := xml.NewDecoder(io.LimitReader(rc, maxRead))
		for {
			tok, err := dec.Token()
			if err != nil {
				break
			}
			switch t := tok.(type) {
			case xml.StartElement:
				if t.Name.Local == "table-cell" {
					inCell = true
					cell.Reset()
				}
			case xml.CharData:
				if inCell {
					cell.Write(t)
				}
			case xml.EndElement:
				switch t.Name.Local {
				case "p":
					if inCell {
						cell.WriteByte(' ')
					}
				case "table-cell":
					inCell = false
					row = append(row, strings.TrimSpace(cell.String()))
				case "table-row":
					sb.WriteString(strings.TrimRight(strings.Join(row, "\t"), "\t") + "\n")
					row = row[:0]
				case "table":
					sb.WriteByte('\n')
				}
			}
		}
		return sb.String()
	}
	return ""
}

type xlsxSST struct {
	SI []struct {
		T string   `xml:"t"`
		R []string `xml:"r>t"`
	} `xml:"si"`
}

type xlsxSheet struct {
	Rows []struct {
		Cells []struct {
			Type string `xml:"t,attr"`
			V    string `xml:"v"`
			IS   string `xml:"is>t"`
		} `xml:"c"`
	} `xml:"sheetData>row"`
}

// xlsxText renders every worksheet row-wise, resolving shared strings.
func xlsxText(p string) string {
	zr, err := zip.OpenReader(p)
	if err != nil {
		return ""
	}
	defer zr.Close()
	decode := func(f *zip.File, v any) bool {
		rc, err := f.Open()
		if err != nil {
			return false
		}
		defer rc.Close()
		return xml.NewDecoder(io.LimitReader(rc, maxRead)).Decode(v) == nil
	}
	var shared []string
	var sheets []*zip.File
	for _, f := range zr.File {
		switch {
		case f.Name == "xl/sharedStrings.xml":
			var sst xlsxSST
			if decode(f, &sst) {
				for _, si := range sst.SI {
					shared = append(shared, si.T+strings.Join(si.R, ""))
				}
			}
		case strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml"):
			sheets = append(sheets, f)
		}
	}
	sort.Slice(sheets, func(i, j int) bool { return naturalLess(sheets[i].Name, sheets[j].Name) })
	var sb strings.Builder
	for _, f := range sheets {
		var sh xlsxSheet
		if !decode(f, &sh) {
			continue
		}
		for _, r := range sh.Rows {
			var cells []string
			for _, c := range r.Cells {
				v := c.V
				switch c.Type {
				case "s":
					if i, err := strconv.Atoi(c.V); err == nil && i >= 0 && i < len(shared) {
						v = shared[i]
					}
				case "inlineStr":
					v = c.IS
				}
				cells = append(cells, v)
			}
			sb.WriteString(strings.TrimRight(strings.Join(cells, "\t"), "\t") + "\n")
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

var reDigits = regexp.MustCompile(`\d+`)

// naturalLess orders slide2.xml before slide10.xml.
func naturalLess(a, b string) bool {
	re := reDigits
	na, nb := re.FindString(a), re.FindString(b)
	if na != "" && nb != "" && re.ReplaceAllString(a, "#") == re.ReplaceAllString(b, "#") {
		x, _ := strconv.Atoi(na)
		y, _ := strconv.Atoi(nb)
		return x < y
	}
	return a < b
}

var (
	reTags    = regexp.MustCompile(`(?s)<(script|style)[^>]*>.*?</(script|style)>|<[^>]+>`)
	reBlockHT = regexp.MustCompile(`(?i)</(p|div|h[1-6]|li|tr)>|<br\s*/?>`)
)

func htmlText(s string) string {
	s = reBlockHT.ReplaceAllString(s, "\n")
	s = reTags.ReplaceAllString(s, "")
	return html.UnescapeString(s)
}

// rtfText is a small RTF-to-text converter: drops destination groups ({\*...},
// fonttbl, colortbl, stylesheet, info, pict), maps \par/\line/\tab, decodes \'hh
// (cp1252) and \uN, strips remaining control words and braces.
func rtfText(s string) string {
	var out bytes.Buffer
	skipDepth := -1
	depth := 0
	i := 0
	skipGroups := map[string]bool{"fonttbl": true, "colortbl": true, "stylesheet": true, "info": true, "pict": true, "header": true, "footer": true}
	for i < len(s) {
		c := s[i]
		switch {
		case c == '{':
			depth++
			// destination group?
			if skipDepth < 0 && i+1 < len(s) && s[i+1] == '\\' {
				j := i + 2
				if j < len(s) && s[j] == '*' {
					skipDepth = depth
				} else {
					k := j
					for k < len(s) && s[k] >= 'a' && s[k] <= 'z' {
						k++
					}
					if skipGroups[s[j:k]] {
						skipDepth = depth
					}
				}
			}
			i++
		case c == '}':
			if depth == skipDepth {
				skipDepth = -1
			}
			depth--
			i++
		case c == '\\':
			if i+1 >= len(s) {
				i++
				continue
			}
			n := s[i+1]
			switch {
			case n == '\'' && i+3 < len(s):
				if v, err := strconv.ParseUint(s[i+2:i+4], 16, 8); err == nil && skipDepth < 0 {
					out.WriteRune(cp1252(byte(v)))
				}
				i += 4
			case n == '\\' || n == '{' || n == '}':
				if skipDepth < 0 {
					out.WriteByte(n)
				}
				i += 2
			case n >= 'a' && n <= 'z':
				k := i + 1
				for k < len(s) && s[k] >= 'a' && s[k] <= 'z' {
					k++
				}
				word := s[i+1 : k]
				m := k
				if m < len(s) && s[m] == '-' {
					m++
				}
				for m < len(s) && s[m] >= '0' && s[m] <= '9' {
					m++
				}
				param := s[k:m]
				if m < len(s) && s[m] == ' ' {
					m++
				}
				if skipDepth < 0 {
					switch word {
					case "par", "line":
						out.WriteByte('\n')
					case "tab":
						out.WriteByte('\t')
					case "u":
						if v, err := strconv.Atoi(param); err == nil {
							if v < 0 {
								v += 65536
							}
							out.WriteRune(rune(v))
							if m < len(s) && s[m] == '?' {
								m++ // ANSI fallback char
							}
						}
					}
				}
				i = m
			default:
				i += 2
			}
		case c == '\r' || c == '\n':
			i++
		default:
			if skipDepth < 0 {
				out.WriteByte(c)
			}
			i++
		}
	}
	return out.String()
}

// cp1252 maps a Windows-1252 byte to its rune (Latin-1 range is identical).
func cp1252(b byte) rune {
	hi := []rune{'€', 0x81, '‚', 'ƒ', '„', '…', '†', '‡', 'ˆ', '‰', 'Š', '‹', 'Œ', 0x8d, 'Ž', 0x8f,
		0x90, '‘', '’', '“', '”', '•', '–', '—', '˜', '™', 'š', '›', 'œ', 0x9d, 'ž', 'Ÿ'}
	if b >= 0x80 && b < 0xa0 {
		return hi[b-0x80]
	}
	return rune(b)
}

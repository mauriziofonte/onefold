// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package testfix builds synthetic fixture files (archives, office documents,
// PDFs, images) for tests. Never used by the product binary.
package testfix

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"time"
)

// Member is one archive entry.
type Member struct {
	Name    string
	Body    string
	ModTime time.Time // wall clock
}

func mkdir(p string) error { return os.MkdirAll(filepath.Dir(p), 0o755) }

// Zip writes a zip archive with the given members.
func Zip(p string, ms []Member) error {
	if err := mkdir(p); err != nil {
		return err
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, m := range ms {
		h := &zip.FileHeader{Name: m.Name, Method: zip.Deflate, Modified: m.ModTime}
		w, err := zw.CreateHeader(h)
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte(m.Body)); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return os.WriteFile(p, buf.Bytes(), 0o644)
}

// TarGz writes a gzip-compressed tar (gz=false: plain tar).
func TarGz(p string, ms []Member, gz bool) error {
	if err := mkdir(p); err != nil {
		return err
	}
	var buf bytes.Buffer
	var tw *tar.Writer
	var gw *gzip.Writer
	if gz {
		gw = gzip.NewWriter(&buf)
		tw = tar.NewWriter(gw)
	} else {
		tw = tar.NewWriter(&buf)
	}
	for _, m := range ms {
		h := &tar.Header{Name: m.Name, Mode: 0o644, Size: int64(len(m.Body)), ModTime: m.ModTime, Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		if _, err := tw.Write([]byte(m.Body)); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if gw != nil {
		if err := gw.Close(); err != nil {
			return err
		}
	}
	return os.WriteFile(p, buf.Bytes(), 0o644)
}

// Docx writes a minimal OOXML word document with core/app properties and body text.
// created is written as UTC W3CDTF (as Office does).
func Docx(p, title, creator string, created time.Time, pages int, body string) error {
	core := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"><dc:title>%s</dc:title><dc:creator>%s</dc:creator><cp:lastModifiedBy>someone</cp:lastModifiedBy><dcterms:created xsi:type="dcterms:W3CDTF">%s</dcterms:created></cp:coreProperties>`,
		title, creator, created.UTC().Format("2006-01-02T15:04:05Z"))
	app := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties"><Pages>%d</Pages></Properties>`, pages)
	doc := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>%s</w:t></w:r></w:p></w:body></w:document>`, body)
	t := created
	return Zip(p, []Member{
		{Name: "[Content_Types].xml", Body: `<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"/>`, ModTime: t},
		{Name: "docProps/core.xml", Body: core, ModTime: t},
		{Name: "docProps/app.xml", Body: app, ModTime: t},
		{Name: "word/document.xml", Body: doc, ModTime: t},
	})
}

// ODT writes a minimal OpenDocument text with meta.xml (creation-date wall clock) and content.
func ODT(p, title, creator string, created time.Time, pages int, body string) error {
	meta := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<office:document-meta xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:meta="urn:oasis:names:tc:opendocument:xmlns:meta:1.0" xmlns:dc="http://purl.org/dc/elements/1.1/"><office:meta><dc:title>%s</dc:title><meta:initial-creator>%s</meta:initial-creator><meta:creation-date>%s.123000000</meta:creation-date><meta:document-statistic meta:page-count="%d"/></office:meta></office:document-meta>`,
		title, creator, created.Format("2006-01-02T15:04:05"), pages)
	content := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><office:document-content xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0"><office:body><office:text><text:p>%s</text:p></office:text></office:body></office:document-content>`, body)
	return Zip(p, []Member{
		{Name: "mimetype", Body: "application/vnd.oasis.opendocument.text", ModTime: created},
		{Name: "meta.xml", Body: meta, ModTime: created},
		{Name: "content.xml", Body: content, ModTime: created},
	})
}

// RTF writes an RTF with an \info group.
func RTF(p, title, author string, created time.Time, body string) error {
	if err := mkdir(p); err != nil {
		return err
	}
	s := fmt.Sprintf(`{\rtf1\ansi{\info{\title %s}{\author %s}{\creatim\yr%d\mo%d\dy%d\hr%d\min%d}}\pard %s\par}`,
		title, author, created.Year(), int(created.Month()), created.Day(), created.Hour(), created.Minute(), body)
	return os.WriteFile(p, []byte(s), 0o644)
}

// PDF writes a minimal valid single-page PDF with an Info dictionary and a text
// stream. date is written as "D:YYYYMMDDHHmmSS+01'00'".
func PDF(p, title, author string, date time.Time, text string) error {
	if err := mkdir(p); err != nil {
		return err
	}
	stream := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (%s) Tj ET", text)
	_, off := date.Zone()
	sign := '+'
	if off < 0 {
		sign, off = '-', -off
	}
	d := fmt.Sprintf("D:%s%c%02d'%02d'", date.Format("20060102150405"), sign, off/3600, (off%3600)/60)
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Title (%s) /Author (%s) /CreationDate (%s) >>", title, author, d),
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, len(objs), xref)
	return os.WriteFile(p, b.Bytes(), 0o644)
}

// PNG writes a w x h PNG filled with a color derived from seed (distinct seeds -> distinct bytes).
func PNG(p string, w, h int, seed uint8) error {
	if err := mkdir(p); err != nil {
		return err
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{seed, uint8(x), uint8(y), 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return os.WriteFile(p, buf.Bytes(), 0o644)
}

// File writes body to p and sets its mtime.
func File(p, body string, mtime time.Time) error {
	if err := mkdir(p); err != nil {
		return err
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		return err
	}
	return os.Chtimes(p, mtime, mtime)
}

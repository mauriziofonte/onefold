// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/evanoberholster/imagemeta/exif2"
	"github.com/evanoberholster/imagemeta/jpeg"
	imeta "github.com/evanoberholster/imagemeta/meta"
)

// Fuzz targets of the native readers: every one reads bytes an archive
// owner did not write and must return, without a panic and without reading
// past what it was given, whatever the bytes are. `go test` runs the seeds;
// `go test -fuzz FuzzX ./internal/meta` fuzzes one (ClusterFuzzLite runs
// them all, .clusterfuzzlite/build.sh). No external tool is called: each
// target calls the pure-Go reader directly.

// fuzzFile writes the input under name in a temp dir and calls read on it:
// the path readers open files themselves.
func fuzzFile(t *testing.T, name string, b []byte, read func(p string)) {
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	read(p)
}

// box is an ISO-BMFF box (here, not in gps_test.go: the ClusterFuzzLite
// build compiles this file alone, see .clusterfuzzlite/build.sh).
func box(typ string, body ...[]byte) []byte {
	n := 8
	for _, b := range body {
		n += len(b)
	}
	out := binary.BigEndian.AppendUint32(nil, uint32(n))
	out = append(out, typ...)
	for _, b := range body {
		out = append(out, b...)
	}
	return out
}

// seedFiles adds the fixtures of testdata that match the globs.
func seedFiles(f *testing.F, globs ...string) {
	for _, g := range globs {
		ms, _ := filepath.Glob(filepath.Join("testdata", g))
		for _, m := range ms {
			if b, err := os.ReadFile(m); err == nil {
				f.Add(b)
			}
		}
	}
}

func FuzzExifTIFF(f *testing.F) {
	le := []byte("II*\x00\x08\x00\x00\x00\x01\x00\x32\x01\x02\x00\x14\x00\x00\x00\x1a\x00\x00\x00\x00\x00\x00\x002020:01:02 03:04:05\x00")
	f.Add(le)
	f.Add([]byte("MM\x00*\x00\x00\x00\x08\x00\x00"))
	f.Add([]byte("II*\x00\xff\xff\xff\x7f"))
	f.Fuzz(func(t *testing.T, b []byte) {
		var m Meta
		_ = exifTIFF(b, &m)
	})
}

func FuzzISOBMFF(f *testing.F) {
	ftyp := box("ftyp", []byte("isom\x00\x00\x02\x00isomiso2mp41"))
	mvhd := box("mvhd", make([]byte, 100))
	f.Add(append(append([]byte{}, ftyp...), box("moov", mvhd)...))
	f.Add(append(append([]byte{}, box("ftyp", []byte("heic\x00\x00\x00\x00mif1heic"))...), box("meta", make([]byte, 4), box("hdlr", make([]byte, 20)))...))
	f.Add([]byte("\x00\x00\x00\x01ftyp\x00\x00\x00\x00\x00\x00\x00\x10"))
	seedFiles(f, "photo/*.heic", "photo/*.avif")
	f.Fuzz(func(t *testing.T, b []byte) {
		fuzzFile(t, "x.mp4", b, func(p string) {
			if info, err := readISOBMFF(p); err == nil {
				_ = info.vendorSignals()
			}
			var m Meta
			_ = heifExifNative(p, &m)
		})
		_, _ = heifDims(b)
	})
}

// ebmlElem is one Matroska element: its ID bytes, an 8-byte size, the body.
func ebmlElem(id []byte, body ...[]byte) []byte {
	var b bytes.Buffer
	b.Write(id)
	n := 0
	for _, x := range body {
		n += len(x)
	}
	var sz [8]byte
	binary.BigEndian.PutUint64(sz[:], uint64(n))
	sz[0] |= 0x01 // 8-byte vint marker
	b.Write(sz[:])
	for _, x := range body {
		b.Write(x)
	}
	return b.Bytes()
}

func FuzzMatroska(f *testing.F) {
	header := ebmlElem([]byte{0x1a, 0x45, 0xdf, 0xa3}, ebmlElem([]byte{0x42, 0x82}, []byte("matroska")))
	info := ebmlElem([]byte{0x15, 0x49, 0xa9, 0x66},
		ebmlElem([]byte{0x2a, 0xd7, 0xb1}, []byte{0x0f, 0x42, 0x40}),
		ebmlElem([]byte{0x44, 0x89}, []byte{0x40, 0x8f, 0x40, 0, 0, 0, 0, 0}),
		ebmlElem([]byte{0x44, 0x61}, make([]byte, 8)))
	video := ebmlElem([]byte{0xe0}, ebmlElem([]byte{0xb0}, []byte{0x01, 0x40}), ebmlElem([]byte{0xba}, []byte{0xf0}))
	tracks := ebmlElem([]byte{0x16, 0x54, 0xae, 0x6b}, ebmlElem([]byte{0xae}, ebmlElem([]byte{0x83}, []byte{1}), video))
	f.Add(append(header, ebmlElem([]byte{0x18, 0x53, 0x80, 0x67}, info, tracks)...))
	f.Add(append(append([]byte{}, header...), 0x18, 0x53, 0x80, 0x67, 0x01, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff))
	f.Fuzz(func(t *testing.T, b []byte) {
		fuzzFile(t, "x.mkv", b, func(p string) { _, _ = readEBML(p) })
	})
}

func FuzzFLV(f *testing.F) {
	meta := []byte("\x02\x00\x0aonMetaData\x08\x00\x00\x00\x02\x00\x08duration\x00\x40\x08\x00\x00\x00\x00\x00\x00" +
		"\x00\x0ccreationdate\x02\x00\x18Sat Jul 08 09:10:11 2023\x00\x00\x09")
	tag := []byte{0x12, 0, 0, byte(len(meta)), 0, 0, 0, 0, 0, 0, 0}
	flv := append([]byte("FLV\x01\x05\x00\x00\x00\x09\x00\x00\x00\x00"), tag...)
	f.Add(append(flv, meta...))
	f.Add([]byte("FLV\x01\x05\x00\x00\x00\x09"))
	f.Fuzz(func(t *testing.T, b []byte) {
		fuzzFile(t, "x.flv", b, func(p string) { _, _ = readFLV(p) })
	})
}

// RIFF: the WebP container and its EXIF chunk.
func FuzzRIFF(f *testing.F) {
	exif := []byte("II*\x00\x08\x00\x00\x00\x00\x00")
	chunk := append([]byte("EXIF"), byte(len(exif)), 0, 0, 0)
	body := append([]byte("WEBP"), append(chunk, exif...)...)
	f.Add(append(append([]byte("RIFF"), byte(len(body)), 0, 0, 0), body...))
	f.Add([]byte("RIFF\xff\xff\xff\xffWEBPVP8X\x0a\x00\x00\x00"))
	seedFiles(f, "photo/*.webp")
	f.Fuzz(func(t *testing.T, b []byte) {
		fuzzFile(t, "x.webp", b, func(p string) {
			var m Meta
			_ = webpNative(p, &m)
		})
	})
}

func FuzzRAR(f *testing.F) {
	f.Add(append(append([]byte{}, rar5Sig...), 0, 0, 0, 0, 0x0a, 0x02, 0x00, 0x00, 0x00))
	f.Add(append(append([]byte{}, rar4Sig...), 0x00, 0x00, 0x73, 0x00, 0x00, 0x0d, 0x00, 0, 0, 0, 0, 0, 0))
	seedFiles(f, "archive/*.rar")
	f.Fuzz(func(t *testing.T, b []byte) {
		fuzzFile(t, "x.rar", b, func(p string) {
			var acc archiveAcc
			_ = listRarNative(p, &acc)
		})
	})
}

func Fuzz7z(f *testing.F) {
	// signature header pointing at an empty end header
	sig := []byte{'7', 'z', 0xbc, 0xaf, 0x27, 0x1c, 0, 4}
	f.Add(append(sig, make([]byte, 24)...))
	f.Add(append(append([]byte{}, sig...), 0x8d, 0x9b, 0xd5, 0x0f, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0x01, 0x00, 0x00, 0x00, 0, 0, 0, 0, 0x00))
	f.Fuzz(func(t *testing.T, b []byte) {
		fuzzFile(t, "x.7z", b, func(p string) {
			var acc archiveAcc
			_ = list7zNative(p, &acc)
		})
	})
}

// zipOf is a zip with the named members.
func zipOf(members map[string]string) []byte {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for n, body := range members {
		w, _ := z.Create(n)
		_, _ = w.Write([]byte(body))
	}
	_ = z.Close()
	return b.Bytes()
}

// Zip containers: Office Open XML, OpenDocument and EPUB metadata.
func FuzzZip(f *testing.F) {
	f.Add(zipOf(map[string]string{"docProps/core.xml": `<cp:coreProperties xmlns:cp="x" xmlns:dcterms="y" xmlns:dc="z"><dc:creator>A</dc:creator><dcterms:created>2020-01-02T03:04:05Z</dcterms:created></cp:coreProperties>`}))
	f.Add(zipOf(map[string]string{"meta.xml": `<office:document-meta xmlns:office="o" xmlns:meta="m"><office:meta><meta:creation-date>2020-01-02T03:04:05</meta:creation-date></office:meta></office:document-meta>`}))
	f.Add(zipOf(map[string]string{"META-INF/container.xml": `<container><rootfiles><rootfile full-path="c.opf"/></rootfiles></container>`, "c.opf": `<package><metadata><date>2020-01-02</date></metadata></package>`}))
	f.Fuzz(func(t *testing.T, b []byte) {
		for _, name := range []string{"x.docx", "x.odt", "x.epub"} {
			fuzzFile(t, name, b, func(p string) {
				var m Meta
				switch name {
				case "x.docx":
					ooxmlMeta(p, &m)
				case "x.odt":
					odfMeta(p, &m)
				default:
					_ = epubMeta(p, &m)
				}
			})
		}
	})
}

func FuzzISO9660(f *testing.F) {
	iso := make([]byte, 20*2048)
	pvd := iso[16*2048:]
	pvd[0] = 1
	copy(pvd[1:], "CD001\x01")
	copy(pvd[813:], "2020010203040500\x00")
	// root directory record: extent 18, one sector
	root := pvd[156:]
	root[0] = 34
	binary.LittleEndian.PutUint32(root[2:], 18)
	binary.LittleEndian.PutUint32(root[10:], 2048)
	root[25] = 2
	term := iso[17*2048:]
	term[0] = 255
	copy(term[1:], "CD001\x01")
	f.Add(iso)
	f.Add(iso[:17*2048])
	f.Fuzz(func(t *testing.T, b []byte) {
		// the reader looks for the descriptors from sector 16: pad short inputs
		if len(b) < 16*2048 {
			b = append(make([]byte, 16*2048), b...)
		}
		fuzzFile(t, "x.iso", b, func(p string) {
			var acc archiveAcc
			_ = listISONative(p, &acc)
		})
	})
}

// OLE2 compound files: the summary information of .doc, .xls, .ppt.
func FuzzOLE2(f *testing.F) {
	hdr := make([]byte, 512)
	copy(hdr, "\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1")
	binary.LittleEndian.PutUint16(hdr[0x1a:], 3)
	binary.LittleEndian.PutUint16(hdr[0x1c:], 0xfffe)
	binary.LittleEndian.PutUint16(hdr[0x1e:], 9)
	binary.LittleEndian.PutUint16(hdr[0x20:], 6)
	f.Add(append(hdr, make([]byte, 1024)...))
	for _, n := range []string{"memo.doc", "sheet.xls", "slides.ppt"} {
		if b, err := os.ReadFile(filepath.Join("..", "textexport", "testdata", n)); err == nil {
			f.Add(b)
		}
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		fuzzFile(t, "x.doc", b, func(p string) {
			var m Meta
			_ = oleMeta(p, &m)
		})
	})
}

func FuzzPDF(f *testing.F) {
	f.Add([]byte("%PDF-1.4\n1 0 obj\n<< /CreationDate (D:20200102030405+01'00') /Author (A\\(b\\)) /Type /Page >>\nendobj\ntrailer\n<< /Info 1 0 R >>\n%%EOF\n"))
	f.Add([]byte("%PDF-1.7\n<< /CreationDate <FEFF0044003A0032003000320030> >>\n"))
	f.Fuzz(func(t *testing.T, b []byte) {
		fuzzFile(t, "x.pdf", b, func(p string) {
			var m Meta
			pdfScan(p, &m)
		})
		_ = pdfPages(b)
		_ = pdfString(b)
		_, _, _ = parsePDFDate(string(b))
	})
}

func FuzzRTF(f *testing.F) {
	f.Add([]byte(`{\rtf1\ansi{\info{\author A}{\creatim\yr2020\mo1\dy2\hr3\min4}{\revtim\yr2021\mo2\dy3}}Hello}`))
	f.Add([]byte(`{\rtf1{\info{\creatim\yr99999999999\mo13\dy40}}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		fuzzFile(t, "x.rtf", b, func(p string) {
			var m Meta
			rtfMeta(p, &m)
		})
	})
}

type jpegScanner func(io.Reader, func(io.Reader, imeta.ExifHeader) error, func(io.Reader) error) error

// jpegRun walks b with scan (scanJPEG or imagemeta's jpeg.ScanJPEG) and
// returns what it returned and what the readers were handed: each EXIF
// header with the bytes it starts at, each XMP packet. The EXIF decoded is
// not compared: on some damaged IFDs imagemeta v0.3.1 returns bytes left in
// its pooled buffer by an earlier file, so two runs of the same walk differ.
func jpegRun(scan jpegScanner, b []byte) string {
	ir := exif2.NewIfdReader(exif2.Logger)
	defer ir.Close()
	var calls []string
	exifFn := func(r io.Reader, h imeta.ExifHeader) error {
		var at []byte
		if br, ok := r.(*bufio.Reader); ok {
			at, _ = br.Peek(min(int(h.ExifLength), 4096))
		}
		calls = append(calls, fmt.Sprintf("exif %v %d %d %d %x", h.ByteOrder, h.FirstIfdOffset, h.TiffHeaderOffset, h.ExifLength, at))
		return ir.DecodeJPEGIfd(r, h)
	}
	xmpFn := func(r io.Reader) error {
		x, err := io.ReadAll(r)
		calls = append(calls, fmt.Sprintf("xmp %x", x))
		return err
	}
	err := scan(bufio.NewReader(bytes.NewReader(b)), exifFn, xmpFn)
	return fmt.Sprintf("%v %q", err, calls)
}

// jpegSame checks that scanJPEG does what imagemeta's walk does on b, and
// that it stops only where imagemeta would loop forever. It reports whether
// b was such an input.
func jpegSame(t *testing.T, b []byte) (stray bool) {
	got := jpegRun(scanJPEG, b)
	if strings.HasPrefix(got, errJPEGStray.Error()) {
		return true // imagemeta never returns on this input: not run
	}
	if want := jpegRun(jpeg.ScanJPEG, b); got != want {
		t.Fatalf("scanJPEG differs from imagemeta:\n got %s\nwant %s", got, want)
	}
	return false
}

// FuzzJPEG: scanJPEG matches imagemeta's JPEG walk on every input on which
// that walk ends, and ends on the others; the photo reader never panics.
func FuzzJPEG(f *testing.F) {
	seedFiles(f, "photo/*.jpg", "../../../testdata/fixtures/images/*/*.jpg", "../../../testdata/media-synth/*/*.jpg")
	f.Add([]byte("\xff\xd8\xff\xd9\xff\xe1\x00\x10" + strings.Repeat("\x00", 64))) // EOI, then a marker: imagemeta loops
	f.Add([]byte("\xff\xd8\x00\xff\xff" + strings.Repeat("\xdb", 70)))
	f.Fuzz(func(t *testing.T, b []byte) {
		jpegSame(t, b)
		decodePhoto(bytes.NewReader(b))
		decodeEXIF(bytes.NewReader(b))
	})
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package textexport

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/richardlehane/mscfb"
)

// Pure-Go text of the Office 97-2003 binary formats, read from their OLE2
// compound file: Word (.doc) through the piece table, Excel (.xls) through
// the BIFF8 cell records, PowerPoint (.ppt) through the text atoms of the
// slides. Nothing is decompressed or rendered: the text as stored.

var errNotOLE = errors.New("not an Office 97-2003 file")

// oleStreams reads the named streams of an OLE2 file (missing ones absent).
func oleStreams(p string, names ...string) (map[string][]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	doc, err := mscfb.New(f)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	out := map[string][]byte{}
	for e, err := doc.Next(); err == nil; e, err = doc.Next() {
		if !want[e.Name] || len(e.Path) > 0 {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(e, 256<<20))
		if err != nil {
			return nil, err
		}
		out[e.Name] = b
	}
	return out, nil
}

// ---------------------------------------------------------------- Word

// docText: the main document text of a Word 97-2003 file. The FIB names the
// table stream (0Table/1Table), the CLX in it holds the piece table: runs of
// characters stored as cp1252 bytes or UTF-16.
func docText(p string) (string, error) {
	st, err := oleStreams(p, "WordDocument", "0Table", "1Table")
	if err != nil {
		return "", err
	}
	return docStreams(st)
}

// docStreams is docText on the streams of the file.
func docStreams(st map[string][]byte) (string, error) {
	wd := st["WordDocument"]
	if len(wd) < 0x200 || binary.LittleEndian.Uint16(wd) != 0xA5EC {
		return "", errNotOLE
	}
	if binary.LittleEndian.Uint16(wd[0x0A:])&0x0100 != 0 {
		return "", errors.New("encrypted document")
	}
	table := st["0Table"]
	if binary.LittleEndian.Uint16(wd[0x0A:])&0x0200 != 0 {
		table = st["1Table"]
	}
	// FIB: base (32 bytes), csw + fibRgW, cslw + fibRgLw, cbRgFcLcb + fibRgFcLcb
	pos := 32
	csw := int(binary.LittleEndian.Uint16(wd[pos:]))
	pos += 2 + csw*2
	cslw := int(binary.LittleEndian.Uint16(wd[pos:]))
	lw := pos + 2
	pos = lw + cslw*4
	if cslw < 4 || pos+2 > len(wd) {
		return "", errNotOLE
	}
	ccpText := int(binary.LittleEndian.Uint32(wd[lw+3*4:])) // characters of the main document
	cbRgFcLcb := int(binary.LittleEndian.Uint16(wd[pos:]))
	rg := pos + 2
	const clxIndex = 33 // fcClx/lcbClx in FibRgFcLcb97
	if cbRgFcLcb <= clxIndex || rg+clxIndex*8+8 > len(wd) {
		return "", errNotOLE
	}
	fcClx := int(binary.LittleEndian.Uint32(wd[rg+clxIndex*8:]))
	lcbClx := int(binary.LittleEndian.Uint32(wd[rg+clxIndex*8+4:]))
	if fcClx+lcbClx > len(table) || lcbClx == 0 {
		return "", errNotOLE
	}
	clx := table[fcClx : fcClx+lcbClx]
	// skip the Prc entries (formatting), find the Pcdt
	for len(clx) > 0 && clx[0] == 0x01 {
		if len(clx) < 3 {
			return "", errNotOLE
		}
		clx = clx[3+int(binary.LittleEndian.Uint16(clx[1:])):]
	}
	if len(clx) < 5 || clx[0] != 0x02 {
		return "", errNotOLE
	}
	plc := clx[5:]
	if lcb := int(binary.LittleEndian.Uint32(clx[1:])); lcb < len(plc) {
		plc = plc[:lcb]
	}
	n := (len(plc) - 4) / 12 // n+1 CPs (4 bytes) + n PCDs (8 bytes)
	if n <= 0 {
		return "", errNotOLE
	}
	var b strings.Builder
	cpDone := 0
	for i := 0; i < n && cpDone < ccpText; i++ {
		cp0 := int(binary.LittleEndian.Uint32(plc[i*4:]))
		cp1 := int(binary.LittleEndian.Uint32(plc[(i+1)*4:]))
		pcd := plc[(n+1)*4+i*8:]
		fc := binary.LittleEndian.Uint32(pcd[2:])
		count := min(cp1-cp0, ccpText-cpDone)
		if count <= 0 {
			continue
		}
		if fc&0x40000000 != 0 { // compressed: one cp1252 byte per character
			off := int(fc&^0x40000000) / 2
			if off+count > len(wd) {
				break
			}
			for _, c := range wd[off : off+count] {
				b.WriteRune(cp1252(c))
			}
		} else {
			off := int(fc)
			if off+2*count > len(wd) {
				break
			}
			u := make([]uint16, count)
			for k := range u {
				u[k] = binary.LittleEndian.Uint16(wd[off+2*k:])
			}
			b.WriteString(string(utf16.Decode(u)))
		}
		cpDone += count
	}
	return wordPlain(b.String()), nil
}

// wordPlain turns Word's control characters into text: paragraph and line
// marks to newlines, cell marks to tabs, field instructions dropped (their
// shown result kept), object anchors removed.
func wordPlain(s string) string {
	var b strings.Builder
	depth, inCode := 0, []bool{}
	for _, r := range s {
		switch r {
		case 0x13: // field begin: instruction follows
			depth++
			inCode = append(inCode, true)
			continue
		case 0x14: // field separator: the result follows
			if depth > 0 {
				inCode[depth-1] = false
			}
			continue
		case 0x15: // field end
			if depth > 0 {
				depth--
				inCode = inCode[:depth]
			}
			continue
		}
		if depth > 0 && inCode[depth-1] {
			continue
		}
		switch r {
		case '\r', 0x0B, 0x0C, 0x0E:
			b.WriteByte('\n')
		case 0x07:
			b.WriteByte('\t')
		case 0x01, 0x08, 0x02, 0x05, 0x1E, 0x1F:
			// picture, drawn object, footnote/comment anchors, hyphens
		default:
			if r >= 0x20 || r == '\t' {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}

// ---------------------------------------------------------------- Excel

type xlsCell struct {
	row, col int
	text     string
}

// xlsText: every sheet of a BIFF8 workbook, "# <sheet>" then one row per
// line, cells separated by tabs.
func xlsText(p string) (string, error) {
	st, err := oleStreams(p, "Workbook", "Book")
	if err != nil {
		return "", err
	}
	return xlsStream(st["Workbook"])
}

// xlsStream is xlsText on the Workbook stream.
func xlsStream(wb []byte) (string, error) {
	if wb == nil {
		return "", errNotOLE // "Book" = BIFF5, not read
	}
	var sst []string
	var sheets []string
	var cells [][]xlsCell
	cur := -1
	var prevSST []byte // SST record body with its CONTINUE records
	var conts [][]byte
	flushSST := func() {
		if prevSST != nil {
			sst = parseSST(prevSST, conts)
			prevSST, conts = nil, nil
		}
	}
	for off := 0; off+4 <= len(wb); {
		typ := binary.LittleEndian.Uint16(wb[off:])
		size := int(binary.LittleEndian.Uint16(wb[off+2:]))
		if off+4+size > len(wb) {
			break
		}
		body := wb[off+4 : off+4+size]
		off += 4 + size
		if typ == 0x003C && prevSST != nil { // CONTINUE of the SST
			conts = append(conts, body)
			continue
		}
		flushSST()
		switch typ {
		case 0x0809: // BOF
			if len(body) >= 4 && binary.LittleEndian.Uint16(body[2:]) == 0x0010 { // worksheet
				cur++
				if cur >= len(cells) {
					cells = append(cells, nil)
				}
			}
		case 0x0085: // BOUNDSHEET: sheet names in order; only worksheets have cells here (chart, macro, VBA sheets do not)
			if len(body) >= 8 && body[5] == 0x00 {
				name, _ := xlsString(body[6:], int(body[6]), 1)
				sheets = append(sheets, name)
			}
		case 0x00FC: // SST
			prevSST = body
		case 0x00FD: // LABELSST
			if cur >= 0 && len(body) >= 10 {
				if i := int(binary.LittleEndian.Uint32(body[6:])); i < len(sst) {
					cells[cur] = append(cells[cur], xlsCell{int(binary.LittleEndian.Uint16(body)), int(binary.LittleEndian.Uint16(body[2:])), sst[i]})
				}
			}
		case 0x0204: // LABEL
			if cur >= 0 && len(body) >= 8 {
				s, _ := xlsString(body[6:], int(binary.LittleEndian.Uint16(body[6:])), 2)
				cells[cur] = append(cells[cur], xlsCell{int(binary.LittleEndian.Uint16(body)), int(binary.LittleEndian.Uint16(body[2:])), s})
			}
		case 0x0203: // NUMBER
			if cur >= 0 && len(body) >= 14 {
				v := math.Float64frombits(binary.LittleEndian.Uint64(body[6:]))
				cells[cur] = append(cells[cur], xlsCell{int(binary.LittleEndian.Uint16(body)), int(binary.LittleEndian.Uint16(body[2:])), num(v)})
			}
		case 0x027E: // RK
			if cur >= 0 && len(body) >= 10 {
				cells[cur] = append(cells[cur], xlsCell{int(binary.LittleEndian.Uint16(body)), int(binary.LittleEndian.Uint16(body[2:])), num(rk(binary.LittleEndian.Uint32(body[6:])))})
			}
		case 0x00BD: // MULRK
			if cur >= 0 && len(body) >= 6 {
				row, col := int(binary.LittleEndian.Uint16(body)), int(binary.LittleEndian.Uint16(body[2:]))
				for k := 4; k+6 <= len(body)-2; k += 6 {
					cells[cur] = append(cells[cur], xlsCell{row, col, num(rk(binary.LittleEndian.Uint32(body[k+2:])))})
					col++
				}
			}
		}
	}
	flushSST()
	var b strings.Builder
	for i, cs := range cells {
		if i < len(sheets) {
			b.WriteString("# " + sheets[i] + "\n")
		}
		sort.SliceStable(cs, func(x, y int) bool {
			if cs[x].row != cs[y].row {
				return cs[x].row < cs[y].row
			}
			return cs[x].col < cs[y].col
		})
		for k := 0; k < len(cs); {
			row := cs[k].row
			var line []string
			for ; k < len(cs) && cs[k].row == row; k++ {
				line = append(line, cs[k].text)
			}
			b.WriteString(strings.Join(line, "\t") + "\n")
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

// parseSST reads the shared strings; a string may cross into a CONTINUE
// record, where its characters restart with their own width flag byte.
func parseSST(body []byte, conts [][]byte) []string {
	if len(body) < 8 {
		return nil
	}
	unique := int(binary.LittleEndian.Uint32(body[4:]))
	segs := append([][]byte{body[8:]}, conts...)
	seg, pos := 0, 0
	next := func(n int) []byte { // n raw bytes, crossing segments when needed (not inside characters)
		var out []byte
		for n > 0 && seg < len(segs) {
			if pos >= len(segs[seg]) {
				seg, pos = seg+1, 0
				continue
			}
			k := min(n, len(segs[seg])-pos)
			out = append(out, segs[seg][pos:pos+k]...)
			pos += k
			n -= k
		}
		return out
	}
	out := make([]string, 0, min(unique, 1<<20))
	for i := 0; i < unique && seg < len(segs); i++ {
		hdr := next(3)
		if len(hdr) < 3 {
			break
		}
		cch := int(binary.LittleEndian.Uint16(hdr))
		flags := hdr[2]
		var runs, ext int
		if flags&0x08 != 0 {
			b := next(2)
			if len(b) < 2 {
				break
			}
			runs = int(binary.LittleEndian.Uint16(b))
		}
		if flags&0x04 != 0 {
			b := next(4)
			if len(b) < 4 {
				break
			}
			ext = int(binary.LittleEndian.Uint32(b))
		}
		wide := flags&0x01 != 0
		var rs []rune
		for cch > 0 && seg < len(segs) {
			if pos >= len(segs[seg]) { // characters continue in the next record: new width flag
				seg, pos = seg+1, 0
				if seg >= len(segs) {
					break
				}
				if len(segs[seg]) == 0 {
					continue
				}
				wide = segs[seg][0]&0x01 != 0
				pos = 1
				continue
			}
			avail := len(segs[seg]) - pos
			if wide {
				k := min(cch, avail/2)
				for j := 0; j < k; j++ {
					rs = append(rs, rune(binary.LittleEndian.Uint16(segs[seg][pos+2*j:])))
				}
				pos += 2 * k
				cch -= k
				if k == 0 {
					pos = len(segs[seg])
				}
			} else {
				k := min(cch, avail)
				for j := 0; j < k; j++ {
					rs = append(rs, rune(segs[seg][pos+j]))
				}
				pos += k
				cch -= k
			}
		}
		next(4*runs + ext)
		out = append(out, string(utf16.Decode(runesToU16(rs))))
	}
	return out
}

func runesToU16(rs []rune) []uint16 {
	u := make([]uint16, len(rs))
	for i, r := range rs {
		u[i] = uint16(r)
	}
	return u
}

// xlsString reads an unformatted BIFF8 string whose length (lenBytes wide)
// is at b[0:].
func xlsString(b []byte, n, lenBytes int) (string, int) {
	if len(b) < lenBytes+1 {
		return "", 0
	}
	flags := b[lenBytes]
	p := lenBytes + 1
	if flags&0x01 != 0 {
		if p+2*n > len(b) {
			n = (len(b) - p) / 2
		}
		u := make([]uint16, n)
		for i := range u {
			u[i] = binary.LittleEndian.Uint16(b[p+2*i:])
		}
		return string(utf16.Decode(u)), p + 2*n
	}
	if p+n > len(b) {
		n = len(b) - p
	}
	rs := make([]rune, n)
	for i := range rs {
		rs[i] = rune(b[p+i]) // BIFF8 "compressed" = the low byte of UTF-16
	}
	return string(rs), p + n
}

// rk decodes an RK number (30-bit integer or the high bits of a double, /100).
func rk(v uint32) float64 {
	var f float64
	if v&0x02 != 0 {
		f = float64(int32(v) >> 2)
	} else {
		f = math.Float64frombits(uint64(v&0xFFFFFFFC) << 32)
	}
	if v&0x01 != 0 {
		f /= 100
	}
	return f
}

func num(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

// ---------------------------------------------------------------- PowerPoint

// pptText: the text atoms of the slides (and notes), in stream order; the
// masters (layout placeholders, theme names) are left out.
func pptText(p string) (string, error) {
	st, err := oleStreams(p, "PowerPoint Document")
	if err != nil {
		return "", err
	}
	return pptStream(st["PowerPoint Document"])
}

// pptStream is pptText on the PowerPoint Document stream.
func pptStream(doc []byte) (string, error) {
	if doc == nil {
		return "", errNotOLE
	}
	var b strings.Builder
	var walk func(rec []byte, inSlide bool)
	walk = func(rec []byte, inSlide bool) {
		for off := 0; off+8 <= len(rec); {
			verInst := binary.LittleEndian.Uint16(rec[off:])
			typ := binary.LittleEndian.Uint16(rec[off+2:])
			size := int(binary.LittleEndian.Uint32(rec[off+4:]))
			if size < 0 || off+8+size > len(rec) {
				return
			}
			body := rec[off+8 : off+8+size]
			off += 8 + size
			switch {
			case verInst&0x000F == 0x000F: // container
				switch typ {
				case 0x03EE, 0x03F0: // Slide, Notes
					walk(body, true)
				case 0x03F8, 0x0FD9, 0x03EF: // MainMaster, HandoutMaster, SlideLayout atoms: placeholders and themes
				default:
					walk(body, inSlide)
				}
			case !inSlide:
			case typ == 0x0FA0: // TextCharsAtom: UTF-16
				u := make([]uint16, len(body)/2)
				for i := range u {
					u[i] = binary.LittleEndian.Uint16(body[2*i:])
				}
				b.WriteString(strings.ReplaceAll(string(utf16.Decode(u)), "\r", "\n") + "\n")
			case typ == 0x0FA8: // TextBytesAtom: low bytes of UTF-16
				rs := make([]rune, len(body))
				for i, c := range body {
					rs[i] = rune(c)
				}
				b.WriteString(strings.ReplaceAll(string(rs), "\r", "\n") + "\n")
			}
		}
	}
	walk(doc, false)
	return b.String(), nil
}

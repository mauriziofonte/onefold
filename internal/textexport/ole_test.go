// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package textexport

import (
	"encoding/binary"
	"testing"
)

// testdata/{memo.doc,sheet.xls,slides.ppt}: made-up content saved by
// Microsoft Word, Excel and PowerPoint in the 97-2003 formats
// (scripts/gen-office-fixtures.ps1). Read in pure Go, no external tool.
func TestOfficeLegacyText(t *testing.T) {
	t.Setenv("ONEFOLD_NO_EXTERNAL", "1")
	for _, c := range []struct{ file, want string }{
		{"memo.doc", "Quarterly memo\nThe città of Lucca, perché no: café, naïve, 25 °C, € 10.\nSecond paragraph with a long line of text that goes on and on to make the piece table a little longer than one line.\nCell A1\tCell B1\t\t\nLast line."},
		{"sheet.xls", "# Budget\nItem\tCosto €\nCaffè\t2.5\nRent\t1200\n\n# Notes\nsecond sheet text\t-3"},
		{"slides.ppt", "Roadmap slide\nPiù città, meno caffè\nSecond slide plain ASCII\nbullet one"},
	} {
		if got := Extract("testdata/" + c.file); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.file, got, c.want)
		}
	}
	if _, err := docText("testdata/sheet.xls"); err == nil {
		t.Error("a workbook read as a Word document")
	}
}

// A shared string crossing into a CONTINUE record restarts with its own
// width flag: an 8-bit start, a UTF-16 end.
func TestSSTContinue(t *testing.T) {
	body := []byte{0, 0, 0, 0, 2, 0, 0, 0} // total, unique = 2
	body = append(body, 3, 0, 0, 'a', 'b', 'c')
	body = append(body, 4, 0, 0, 'd', 'e') // 4 chars, 8-bit, only 2 here
	cont := []byte{0x01}                   // the rest is UTF-16
	cont = binary.LittleEndian.AppendUint16(cont, 0x00E8)
	cont = binary.LittleEndian.AppendUint16(cont, 'f')
	got := parseSST(body, [][]byte{cont})
	if len(got) != 2 || got[0] != "abc" || got[1] != "deèf" {
		t.Fatalf("%q", got)
	}
}

// Field instructions are dropped, their results kept; cell marks become tabs.
func TestWordPlain(t *testing.T) {
	in := "Go to \x13 HYPERLINK \"http://x\" \x14the site\x15 now\rA\x07B\x07\x07\x01end"
	if got := wordPlain(in); got != "Go to the site now\nA\tB\t\tend" {
		t.Fatalf("%q", got)
	}
}

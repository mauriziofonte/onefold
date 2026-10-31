// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package textexport

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mauriziofonte/onefold/internal/plan"
	"github.com/mauriziofonte/onefold/internal/testfix"
)

func TestExtractors(t *testing.T) {
	d := t.TempDir()
	ts := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(testfix.Docx(filepath.Join(d, "a.docx"), "T", "A", ts, 1, "Ciao mondo docx"))
	must(testfix.ODT(filepath.Join(d, "a.odt"), "T", "A", ts, 1, "Ciao mondo odt"))
	must(testfix.RTF(filepath.Join(d, "a.rtf"), "Titolo", "Autore", ts, `Citt\'e0 \par seconda riga`))
	must(testfix.Zip(filepath.Join(d, "a.xlsx"), []testfix.Member{{Name: "xl/sharedStrings.xml", ModTime: ts,
		Body: `<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><si><t>Cella uno</t></si><si><t>Cella due</t></si></sst>`},
		{Name: "xl/worksheets/sheet1.xml", ModTime: ts, Body: `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>` +
			`<row><c t="s"><v>0</v></c><c t="s"><v>1</v></c></row><row><c><v>42</v></c><c t="inlineStr"><is><t>inline</t></is></c></row></sheetData></worksheet>`}}))
	must(testfix.Zip(filepath.Join(d, "a.pptx"), []testfix.Member{
		{Name: "ppt/slides/slide10.xml", ModTime: ts, Body: `<p:sld xmlns:p="p" xmlns:a="a"><a:p><a:t>DIECI</a:t></a:p></p:sld>`},
		{Name: "ppt/slides/slide2.xml", ModTime: ts, Body: `<p:sld xmlns:p="p" xmlns:a="a"><a:p><a:t>DUE</a:t></a:p></p:sld>`},
	}))
	must(testfix.File(filepath.Join(d, "a.html"), "<html><style>x{}</style><p>Uno &amp; due</p><br>tre</html>", ts))
	must(testfix.File(filepath.Join(d, "a.md"), "# Titolo\r\n\r\n\r\n\r\ntesto", ts))

	cases := map[string][]string{
		"a.docx": {"Ciao mondo docx"},
		"a.odt":  {"Ciao mondo odt"},
		"a.rtf":  {"Città", "\nseconda riga"},
		"a.xlsx": {"Cella uno\tCella due\n42\tinline"},
		"a.pptx": {"DUE\n\nDIECI"},
		"a.html": {"Uno & due\n", "tre"},
		"a.md":   {"# Titolo\n\ntesto"},
	}
	for f, wants := range cases {
		got := Extract(filepath.Join(d, f))
		for _, w := range wants {
			if !strings.Contains(got, w) {
				t.Errorf("%s: %q missing in %q", f, w, got)
			}
		}
	}
	if strings.Contains(Extract(filepath.Join(d, "a.rtf")), "Titolo") {
		t.Error("rtf \\info group must be skipped")
	}
	if strings.Contains(Extract(filepath.Join(d, "a.html")), "x{}") {
		t.Error("html style must be dropped")
	}
	if _, err := exec.LookPath("pdftotext"); err == nil {
		must(testfix.PDF(filepath.Join(d, "a.pdf"), "T", "A", ts, "Totale fattura"))
		if got := Extract(filepath.Join(d, "a.pdf")); !strings.Contains(got, "Totale fattura") {
			t.Errorf("pdf: %q", got)
		}
	}
}

func TestExportSidecar(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "src", "r.docx")
	if err := testfix.Docx(src, "Relazione", "Mario", time.Date(2021, 6, 1, 9, 30, 0, 0, time.UTC), 4, "Contenuto importante"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(src)
	a := plan.Action{Kind: "document", SrcHost: "h", SrcPath: src, SrcSize: int64(len(before)), CanonRel: "DOCUMENTS/DOC/2021/Mario/Relazione.docx", Time: "2021-06-01 11:30:00"}
	if !Eligible(a) || Eligible(plan.Action{Kind: "photo"}) {
		t.Fatal("Eligible")
	}
	target := filepath.Join(d, "out")
	r, err := Export(a, target)
	if err != nil || r.Skipped || r.Empty {
		t.Fatalf("export: %v %+v", err, r)
	}
	if r.Dst != filepath.Join(target, "DOCUMENTS/DOC/2021/Mario/Relazione.docx.txt") {
		t.Fatalf("dst %s", r.Dst)
	}
	b, _ := os.ReadFile(r.Dst)
	for _, w := range []string{"TEXT EXPORT - onefold", "Title: Relazione", "Author: Mario", "Pages: 4", "Contenuto importante", "END OF DOCUMENT"} {
		if !strings.Contains(string(b), w) {
			t.Errorf("sidecar missing %q", w)
		}
	}
	r2, _ := Export(a, target)
	if !r2.Skipped {
		t.Fatal("second export must skip")
	}
	after, _ := os.ReadFile(src)
	if string(after) != string(before) {
		t.Fatal("source modified")
	}
	// nothing extractable -> placeholder
	bin := filepath.Join(d, "src", "x.xls")
	os.WriteFile(bin, []byte("\xd0\xcf\x11\xe0 not really"), 0o644)
	re, err := Export(plan.Action{Kind: "document", SrcPath: bin, CanonRel: "FILES/XLS/2020/x.xls", Time: "2020-01-01 00:00:00"}, target)
	if err != nil || !re.Empty {
		t.Fatalf("empty export: %v %+v", err, re)
	}
	eb, _ := os.ReadFile(re.Dst)
	if !strings.Contains(string(eb), NoContent) {
		t.Fatal("placeholder missing")
	}
}

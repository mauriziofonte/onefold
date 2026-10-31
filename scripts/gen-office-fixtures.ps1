# onefold
# Author: Maurizio Fonte <fonte.maurizio@gmail.com>
# Copyright (c) 2026 Maurizio Fonte
# SPDX-License-Identifier: AGPL-3.0-or-later

# Writes internal/textexport/testdata/{memo.doc,slides.ppt,sheet.xls}: made-up
# content saved in the 97-2003 formats by Microsoft Word, PowerPoint and Excel
# (COM automation, Windows with Office). Personal information is removed on
# save. Run: powershell -File gen-office-fixtures.ps1 (files land in %TEMP%\of-r12).
$ErrorActionPreference = 'Stop'
$out = Join-Path $env:TEMP 'of-r12'
New-Item -ItemType Directory -Force $out | Out-Null
# Word 97-2003 (.doc, wdFormatDocument = 0)
$w = New-Object -ComObject Word.Application
$w.Visible = $false
$w.DisplayAlerts = 0
try {
  $d = $w.Documents.Add()
  $s = $w.Selection
  $s.TypeText("Quarterly memo"); $s.TypeParagraph()
  $s.TypeText("The citt" + [char]0x00E0 + " of Lucca, perch" + [char]0x00E9 + " no: caf" + [char]0x00E9 + ", na" + [char]0x00EF + "ve, 25 " + [char]0x00B0 + "C, " + [char]0x20AC + " 10."); $s.TypeParagraph()
  $s.TypeText("Second paragraph with a long line of text that goes on and on to make the piece table a little longer than one line."); $s.TypeParagraph()
  $t = $d.Tables.Add($s.Range, 1, 2)
  $t.Cell(1,1).Range.Text = "Cell A1"; $t.Cell(1,2).Range.Text = "Cell B1"
  $s.EndKey(6) | Out-Null
  $s.TypeParagraph(); $s.TypeText("Last line.")
  $d.RemovePersonalInformation = $true
  $d.SaveAs([ref](Join-Path $out 'memo.doc'), [ref]0)
  $d.Close([ref]0)
} finally { $w.Quit() }
# PowerPoint 97-2003 (.ppt, ppSaveAsPresentation = 1)
$p = New-Object -ComObject PowerPoint.Application
try {
  $pr = $p.Presentations.Add(0)
  $sl = $pr.Slides.Add(1, 2)
  $sl.Shapes.Item(1).TextFrame.TextRange.Text = "Roadmap slide"
  $sl.Shapes.Item(2).TextFrame.TextRange.Text = "Pi" + [char]0x00F9 + " citt" + [char]0x00E0 + ", meno caff" + [char]0x00E8
  $sl2 = $pr.Slides.Add(2, 2)
  $sl2.Shapes.Item(1).TextFrame.TextRange.Text = "Second slide plain ASCII"
  $sl2.Shapes.Item(2).TextFrame.TextRange.Text = "bullet one"
  $pr.RemovePersonalInformation = -1
  $pr.SaveAs((Join-Path $out 'slides.ppt'), 1)
  $pr.Close()
} finally { $p.Quit() }
# Excel 97-2003 (.xls, xlExcel8 = 56)
$x = New-Object -ComObject Excel.Application
$x.Visible = $false; $x.DisplayAlerts = $false
try {
  $wb = $x.Workbooks.Add()
  $ws = $wb.Worksheets.Item(1); $ws.Name = "Budget"
  $ws.Cells.Item(1,1).Value2 = "Item"; $ws.Cells.Item(1,2).Value2 = "Costo " + [char]0x20AC
  $ws.Cells.Item(2,1).Value2 = "Caff" + [char]0x00E8; $ws.Cells.Item(2,2).Value2 = 2.5
  $ws.Cells.Item(3,1).Value2 = "Rent"; $ws.Cells.Item(3,2).Value2 = 1200
  $ws2 = $wb.Worksheets.Add([Type]::Missing, $ws); $ws2.Name = "Notes"
  $ws2.Cells.Item(1,1).Value2 = "second sheet text"; $ws2.Cells.Item(1,2).Value2 = -3
  $wb.RemovePersonalInformation = $true
  $wb.SaveAs((Join-Path $out 'sheet.xls'), 56)
  $wb.Close($false)
} finally { $x.Quit() }
"done"

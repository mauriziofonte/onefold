// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package htmlreport

import (
	"strings"
	"testing"
)

func TestDoc(t *testing.T) {
	d := New("Report <1>", "sub & line")
	d.Section("Numbers")
	d.Figures([]Figure{{Label: "Files", Value: "12", Note: "3 KB"}})
	d.Bars("By family", []string{"Family", "Files", "Size"}, []Bar{
		{Label: "photos", Value: 10, Color: "#4e79a7", Extra: []string{"2 MB"}},
		{Label: "<script>alert(1)</script>", Value: 0},
	})
	d.Bars("Empty", []string{"A", "B"}, nil)
	d.Table("Largest", []string{"Path", "Size"}, [][]string{{"a/b.jpg", "1 MB"}}, 1)
	var b strings.Builder
	if _, err := d.WriteTo(&b); err != nil {
		t.Fatal(err)
	}
	s := b.String()
	if err := Check(s); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<title>Report &lt;1&gt;</title>", "sub &amp; line", "role=\"img\"", "aria-label=\"By family: photos 10",
		"<summary>Table</summary>", "<td class=\"n\">10</td><td class=\"n\">2 MB</td>", "(none)", "a/b.jpg", "&lt;script&gt;"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in\n%s", want, s)
		}
	}
	if strings.Count(s, "<svg") != 1 {
		t.Fatalf("one chart expected, got %d", strings.Count(s, "<svg"))
	}
}

func TestCheck(t *testing.T) {
	ok := "<!doctype html><meta http-equiv=\"Content-Security-Policy\" content=\"default-src 'none'; style-src 'unsafe-inline'\"><svg></svg><details></details>"
	if err := Check(ok); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		ok + "<script>x</script>",
		ok + "<img src=\"https://example.org/x.png\">",
		ok + "<link rel=stylesheet href=\"//cdn.example.org/a.css\">",
		ok + "<svg></svg>", // a chart with no table
		"<!doctype html><svg></svg><details></details>",
	} {
		if Check(bad) == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mauriziofonte/onefold/internal/testfix"
	"github.com/mauriziofonte/onefold/internal/tools"
)

// testdata/archive/sample5.rar is a RAR 5.0 archive (stored members, Unix
// times) built from the format by testfix.RAR5 and accepted by unrar 7
// (`unrar t`: All OK) and 7-Zip: a directory and two files, newest member
// 2021-03-04 05:06:07 UTC. The native reader and the unrar fallback must list
// it alike, in the canonical zone.
func TestRAR5Listing(t *testing.T) {
	p := filepath.Join("testdata", "archive", "sample5.rar")
	// the committed bytes are what the builder makes (a regenerated fixture is the same file)
	fresh := filepath.Join(t.TempDir(), "sample5.rar")
	if err := testfix.RAR5(fresh, []testfix.Member{
		{Name: "docs/", ModTime: time.Date(2019, 1, 2, 3, 4, 5, 0, time.UTC)},
		{Name: "docs/report.txt", Body: "a report", ModTime: time.Date(2020, 5, 6, 7, 8, 9, 0, time.UTC)},
		{Name: "photo.jpg", Body: "not a photo", ModTime: time.Date(2021, 3, 4, 5, 6, 7, 0, time.UTC)},
	}); err != nil {
		t.Fatal(err)
	}
	a, _ := os.ReadFile(p)
	b, _ := os.ReadFile(fresh)
	if string(a) != string(b) {
		t.Fatal("testdata/archive/sample5.rar differs from testfix.RAR5")
	}
	prev := Zone()
	defer SetZone(prev)
	for _, zone := range []string{"UTC", "America/New_York"} {
		l, err := time.LoadLocation(zone)
		if err != nil {
			t.Fatal(err)
		}
		SetZone(l)
		want := time.Date(2021, 3, 4, 5, 6, 7, 0, time.UTC).In(l).Format(canonFmt)
		var native archiveAcc
		if err := listRarNative(p, &native); err != nil {
			t.Fatalf("%s: native: %v", zone, err)
		}
		if native.entries != 2 || native.max.Format(canonFmt) != want {
			t.Errorf("%s: native %d entries, newest %s, want 2, %s", zone, native.entries, native.max.Format(canonFmt), want)
		}
		if !tools.Has("unrar") {
			continue
		}
		var viaUnrar archiveAcc
		listRar(p, &viaUnrar)
		if viaUnrar.entries != native.entries || viaUnrar.max.Format(canonFmt) != native.max.Format(canonFmt) {
			t.Errorf("%s: unrar %d entries, newest %s; native %d, %s", zone, viaUnrar.entries, viaUnrar.max.Format(canonFmt),
				native.entries, native.max.Format(canonFmt))
		}
	}
}

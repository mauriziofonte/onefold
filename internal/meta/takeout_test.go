// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Takeout: the capture time of a photo whose metadata and name have none,
// the place, and the original's file for an edited copy; nothing without the
// option, and the file's own time and name win over Takeout's. Times are
// wall clocks of the package's zone (Europe/Rome, TestMain).
func TestTakeout(t *testing.T) {
	dir := t.TempDir()
	put := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	js := func(ts string, lat, lon string) string {
		return `{"title":"x","photoTakenTime":{"timestamp":"` + ts + `","formatted":"..."},"geoData":{"latitude":` + lat + `,"longitude":` + lon + `,"altitude":0}}`
	}
	long := "Screenshot_20200101-120000_A_rather_long_app.jpg" // cut to 51 characters by Takeout
	put("beach.jpg", "not a jpeg")
	put("beach.jpg.json", js("1620284889", "41.890251", "12.492373"))
	put("beach-edited.jpg", "not a jpeg either")
	put("park.jpg", "x")
	put("park.jpg.supplemental-metadata.json", js("1577880000", "0.0", "0.0"))
	put(long, "x")
	put((long + ".supplemental-metadata")[:46]+".json", js("1577880000", "48.8584", "2.2945"))
	put("IMG_20190203_040506.jpg", "x")
	put("IMG_20190203_040506.jpg.json", js("1620284889", "45.4642", "9.19"))
	cases := []struct {
		name, time, src, cat string
		lat, lon             float64
	}{
		{"beach.jpg", "2021-05-06 09:08:09", "takeout", "", 41.890251, 12.492373},
		{"beach-edited.jpg", "2021-05-06 09:08:09", "takeout", "editing", 41.890251, 12.492373},
		{"park.jpg", "2020-01-01 13:00:00", "takeout", "", 0, 0},
		{long, "2020-01-01 12:00:00", "filename", "", 48.8584, 2.2945},
		{"IMG_20190203_040506.jpg", "2019-02-03 04:05:06", "filename", "", 45.4642, 9.19},
	}
	for _, c := range cases {
		m, _ := ExtractWith(filepath.Join(dir, c.name), Options{Takeout: true})
		if m.Time != c.time || m.TimeSource != c.src || m.Category != c.cat || m.Lat != c.lat || m.Lon != c.lon {
			t.Errorf("%s: %s (%s) cat %q at %v,%v; want %s (%s) %q at %v,%v", c.name, m.Time, m.TimeSource, m.Category, m.Lat, m.Lon, c.time, c.src, c.cat, c.lat, c.lon)
		}
	}
	m, _ := ExtractWith(filepath.Join(dir, "beach-edited.jpg"), Options{})
	if m.TimeSource == "takeout" || m.Category == "editing" || m.Lat != 0 {
		t.Errorf("without the option: %+v", m)
	}
	if strings.Contains(m.Prefix, "EDT") {
		t.Errorf("prefix %q without the option", m.Prefix)
	}
}

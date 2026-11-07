// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import (
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mauriziofonte/onefold/internal/testfix"
	"github.com/mauriziofonte/onefold/internal/tools"
)

func TestParseISO6709(t *testing.T) {
	for _, c := range []struct {
		s        string
		lat, lon float64
		ok       bool
	}{
		{"+41.8919+012.5113/", 41.8919, 12.5113, true},
		{"+41.8919+012.5113+050.000/", 41.8919, 12.5113, true},
		{"-33.8568+151.2153/", -33.8568, 151.2153, true},
		{"+40.6892-074.0445+005.000/", 40.6892, -74.0445, true},
		{"+4153.514+01230.678/", 41.8919, 12.5113, true},
		{"+415330.6+0123040.7/", 41.8918, 12.5113, true},
		{"+00.0000+000.0000/", 0, 0, false},
		{"+95.0+010.0/", 0, 0, false},
		{"+4175.0+01230.0/", 0, 0, false},
		{"Rome", 0, 0, false},
		{"", 0, 0, false},
	} {
		lat, lon, ok := parseISO6709(c.s)
		if ok != c.ok || math.Abs(lat-c.lat) > 1e-4 || math.Abs(lon-c.lon) > 1e-4 {
			t.Errorf("%q: %v %v %v", c.s, lat, lon, ok)
		}
	}
}

// The place of a photo (EXIF GPS) and of a video (QuickTime ©xyz, read by
// the pure-Go reader) is kept only with Options.Places.
func TestPlaces(t *testing.T) {
	d := t.TempDir()
	jp := filepath.Join(d, "IMG_0001.jpg")
	if err := testfix.JPEG(jp, 32, 24, 1, testfix.Exif{DateTimeOriginal: "2021:05:06 07:08:09", Make: "Apple", Model: "iPhone 12", Lat: 41.8902, Lon: 12.4922}); err != nil {
		t.Fatal(err)
	}
	south := filepath.Join(d, "IMG_0002.jpg")
	if err := testfix.JPEG(south, 32, 24, 2, testfix.Exif{DateTimeOriginal: "2021:05:06 07:08:09", Lat: -33.8568, Lon: -70.6483}); err != nil {
		t.Fatal(err)
	}
	m, _ := ExtractWith(jp, Options{Places: true})
	if math.Abs(m.Lat-41.8902) > 1e-5 || math.Abs(m.Lon-12.4922) > 1e-5 || m.Time != "2021-05-06 07:08:09" {
		t.Fatalf("photo: %v %v %q", m.Lat, m.Lon, m.Time)
	}
	if m, _ := ExtractWith(south, Options{Places: true}); math.Abs(m.Lat+33.8568) > 1e-5 || math.Abs(m.Lon+70.6483) > 1e-5 {
		t.Fatalf("south and west: %v %v", m.Lat, m.Lon)
	}
	if m, _ := ExtractWith(jp, Options{}); m.Lat != 0 || m.Lon != 0 {
		t.Fatalf("a place kept without Places: %v %v", m.Lat, m.Lon)
	}
	// a QuickTime movie header and an udta ©xyz atom: size(2) language(2) text
	xyz := []byte("+41.8919+012.5113/")
	atom := append(binary.BigEndian.AppendUint16(nil, uint16(len(xyz))), 0x15, 0xc7)
	mvhd := make([]byte, 100)
	binary.BigEndian.PutUint32(mvhd[12:], 600)  // time scale
	binary.BigEndian.PutUint32(mvhd[16:], 1800) // 3 s
	mov := append(box("ftyp", []byte("qt  \x00\x00\x02\x00qt  ")), box("moov", box("mvhd", mvhd), box("udta", box("\xa9xyz", atom, xyz)))...)
	vp := filepath.Join(d, "VID_0001.mov")
	if err := os.WriteFile(vp, mov, 0o644); err != nil {
		t.Fatal(err)
	}
	vm, err := extractVideoNative(vp)
	if err != nil || math.Abs(vm.Lat-41.8919) > 1e-6 || math.Abs(vm.Lon-12.5113) > 1e-6 {
		t.Fatalf("video: %v %v %v", vm.Lat, vm.Lon, err)
	}
}

// ffprobe and the pure-Go reader give the same place for the videos ffmpeg
// writes: 3GPP loci in MP4, ©xyz in QuickTime.
func TestVideoPlaceParity(t *testing.T) {
	ff, ok := tools.Path("ffmpeg")
	if !ok || !tools.Has("ffprobe") {
		t.Skip("ffmpeg and ffprobe not installed")
	}
	d := t.TempDir()
	for _, name := range []string{"VID_0001.mp4", "VID_0002.mov"} {
		p := filepath.Join(d, name)
		if out, err := exec.Command(ff, "-nostdin", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=d=1:size=64x48",
			"-metadata", "location=+41.8919+012.5113/", p).CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		probe, _ := extractVideo(p)
		native, _ := extractVideoNative(p)
		if math.Abs(probe.Lat-41.8919) > 1e-4 || math.Abs(probe.Lon-12.5113) > 1e-4 || math.Abs(native.Lat-probe.Lat) > 1e-4 || math.Abs(native.Lon-probe.Lon) > 1e-4 {
			t.Errorf("%s: ffprobe %v,%v native %v,%v", name, probe.Lat, probe.Lon, native.Lat, native.Lon)
		}
	}
}

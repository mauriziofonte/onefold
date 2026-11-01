// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package winres

import (
	"bytes"
	"encoding/binary"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// The committed .syso must hold what the generator writes today. Compared by
// content, not bytes: the PNG bytes of the icons depend on the deflate of the
// Go toolchain (they changed between go1.22 and go1.27), their pixels do not.
func TestSysoInSync(t *testing.T) {
	want, err := Build()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join("..", "..", "cmd", "onefold", SysoName))
	if err != nil {
		t.Fatalf("%v: run go generate ./cmd/onefold", err)
	}
	stale := func(why string, args ...any) {
		t.Helper()
		t.Fatalf("cmd/onefold/"+SysoName+" is stale ("+why+"): run go generate ./cmd/onefold", args...)
	}
	w, _ := resources(t, want)
	g, _ := resources(t, got)
	if string(g[rtManifest][1]) != string(w[rtManifest][1]) {
		stale("manifest")
	}
	if len(g[rtIcon]) != len(w[rtIcon]) {
		stale("icons %d, want %d", len(g[rtIcon]), len(w[rtIcon]))
	}
	for id, wb := range w[rtIcon] {
		if !samePixels(t, g[rtIcon][id], wb) {
			stale("icon %d", id)
		}
	}
	// group entries: same image ids and sizes; the byte count follows the PNG
	gg, wg := g[rtGroupIcon][IconID], w[rtGroupIcon][IconID]
	if len(gg) != len(wg) || !bytes.Equal(gg[:6], wg[:6]) {
		stale("icon group")
	}
	for i := 6; i+14 <= len(wg); i += 14 {
		if !bytes.Equal(gg[i:i+8], wg[i:i+8]) || !bytes.Equal(gg[i+12:i+14], wg[i+12:i+14]) {
			stale("icon group entry %d", (i-6)/14)
		}
	}
}

func samePixels(t *testing.T, a, b []byte) bool {
	t.Helper()
	ia, err := png.Decode(bytes.NewReader(a))
	if err != nil {
		return false
	}
	ib, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	r := ib.Bounds()
	if ia.Bounds() != r {
		return false
	}
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if color.NRGBAModel.Convert(ia.At(x, y)) != color.NRGBAModel.Convert(ib.At(x, y)) {
				return false
			}
		}
	}
	return true
}

// resources walks the resource tree of a .syso: type -> id -> data, and the
// number of relocations.
func resources(t *testing.T, b []byte) (map[uint16]map[uint16][]byte, int) {
	t.Helper()
	le := binary.LittleEndian
	if len(b) < 60 || le.Uint16(b[0:]) != imageFileMachineAMD64 || le.Uint16(b[2:]) != 1 {
		t.Fatal("bad file header")
	}
	rawSize, rawAt := int(le.Uint32(b[20+16:])), int(le.Uint32(b[20+20:]))
	nReloc := int(le.Uint16(b[20+32:]))
	raw := b[rawAt : rawAt+rawSize]
	if n := int(le.Uint16(raw[14:])); n != 3 {
		t.Fatalf("types: %d", n)
	}
	data := map[uint16]map[uint16][]byte{}
	for k := 0; k < 3; k++ {
		typ := uint16(le.Uint32(raw[16+8*k:]))
		td := int(le.Uint32(raw[16+8*k+4:]) &^ subdirFlag)
		data[typ] = map[uint16][]byte{}
		for n := 0; n < int(le.Uint16(raw[td+14:])); n++ {
			id := uint16(le.Uint32(raw[td+16+8*n:]))
			ld := int(le.Uint32(raw[td+16+8*n+4:]) &^ subdirFlag)
			de := int(le.Uint32(raw[ld+16+4:]))
			off, size := int(le.Uint32(raw[de:])), int(le.Uint32(raw[de+4:]))
			data[typ][id] = raw[off : off+size]
		}
	}
	return data, nReloc
}

// Walk the resource tree back: three types, the icon group points at every
// icon image, the manifest is intact, every data entry has a relocation.
func TestSysoLayout(t *testing.T) {
	b, err := Build()
	if err != nil {
		t.Fatal(err)
	}
	le := binary.LittleEndian
	data, nReloc := resources(t, b)
	if len(data[rtIcon]) != len(iconSizes) || nReloc != len(iconSizes)+2 {
		t.Fatalf("icons %d relocations %d", len(data[rtIcon]), nReloc)
	}
	grp := data[rtGroupIcon][IconID]
	for i := range iconSizes {
		e := grp[6+14*i:]
		id, size := le.Uint16(e[12:]), le.Uint32(e[8:])
		if img := data[rtIcon][id]; len(img) != int(size) || !bytes.HasPrefix(img, []byte("\x89PNG")) {
			t.Fatalf("group entry %d: id %d size %d", i, id, size)
		}
	}
	if string(data[rtManifest][1]) != Manifest {
		t.Fatal("manifest differs")
	}
}

func TestLogo(t *testing.T) {
	img := Logo(48)
	for _, c := range []struct {
		x, y int
		want color.NRGBA
		what string
	}{
		{0, 0, color.NRGBA{}, "transparent corner"},
		{46, 46, color.NRGBA{}, "transparent corner"},
		{10, 30, backColor, "back square"},
		{36, 12, frontColor, "front square"},
		{24, 22, overColor, "overlap"},
	} {
		if got := img.NRGBAAt(c.x, c.y); got != c.want {
			t.Errorf("%s at %d,%d: %v, want %v", c.what, c.x, c.y, got, c.want)
		}
	}
}

func TestVersionNumbers(t *testing.T) {
	for in, want := range map[string][4]uint16{
		"v1.2.3": {1, 2, 3, 0}, "v1.2.3-4-gabcdef-dirty": {1, 2, 3, 0}, "1.20.300": {1, 20, 300, 0},
		"dev": {}, "9892bc8-dirty": {}, "v99999.1.1": {65535, 1, 1, 0},
	} {
		if got := VersionNumbers(in); got != want {
			t.Errorf("%q: %v, want %v", in, got, want)
		}
	}
}

// The version resource: VS_VERSIONINFO key, the fixed info signature on a
// 32-bit boundary with the numbers, the strings in UTF-16; the committed
// object (no version) and a versioned one differ only by it.
func TestVersionInfo(t *testing.T) {
	vi := versionInfo("v1.2.3")
	if int(binary.LittleEndian.Uint16(vi)) != len(vi) {
		t.Fatalf("wLength %d, len %d", binary.LittleEndian.Uint16(vi), len(vi))
	}
	if !bytes.Contains(vi, wstr("VS_VERSION_INFO")) || !bytes.Contains(vi, wstr("onefold.exe")) || !bytes.Contains(vi, wstr("v1.2.3")) {
		t.Fatal("keys or strings missing")
	}
	sig := bytes.Index(vi, []byte{0xBD, 0x04, 0xEF, 0xFE})
	if sig%4 != 0 || binary.LittleEndian.Uint32(vi[sig+8:]) != 1<<16|2 || binary.LittleEndian.Uint32(vi[sig+12:]) != 3<<16 {
		t.Fatalf("fixed info at %d: % x", sig, vi[sig:sig+16])
	}
	plain, _ := Build()
	versioned, _ := BuildVersion("v1.2.3")
	if bytes.Equal(plain, versioned) || len(versioned) <= len(plain) {
		t.Fatal("the version resource is not in the versioned object")
	}
}

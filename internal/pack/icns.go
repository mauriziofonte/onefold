// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package pack

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"sync"

	"github.com/mauriziofonte/onefold/internal/winres"
)

// icnsTypes are the entries of the macOS icon, each a PNG of the logo at
// that size: 16 to 512 points at 1x and 2x (macOS 10.7 and later read PNG
// in every one of them).
var icnsTypes = []struct {
	typ  string
	size int
}{
	{"icp4", 16}, {"ic11", 32},
	{"icp5", 32}, {"ic12", 64},
	{"ic07", 128}, {"ic13", 256},
	{"ic08", 256}, {"ic14", 512},
	{"ic09", 512}, {"ic10", 1024},
}

// Icns is onefold.icns: the logo of internal/winres (the same drawing as
// assets/brand/onefold-icon.svg) rendered at every size of icnsTypes. It is
// drawn once per process.
func Icns() ([]byte, error) { return icnsOnce() }

var icnsOnce = sync.OnceValues(drawIcns)

func drawIcns() ([]byte, error) {
	pngs := map[int][]byte{}
	var body bytes.Buffer
	for _, e := range icnsTypes {
		p, ok := pngs[e.size]
		if !ok {
			var b bytes.Buffer
			if err := png.Encode(&b, winres.Logo(e.size)); err != nil {
				return nil, err
			}
			p = b.Bytes()
			pngs[e.size] = p
		}
		body.WriteString(e.typ)
		_ = binary.Write(&body, binary.BigEndian, uint32(8+len(p)))
		body.Write(p)
	}
	var out bytes.Buffer
	out.WriteString("icns")
	_ = binary.Write(&out, binary.BigEndian, uint32(8+body.Len()))
	out.Write(body.Bytes())
	return out.Bytes(), nil
}

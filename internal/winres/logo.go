// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package winres

import (
	"image"
	"image/color"
	"math"
)

// The logo is assets/brand/onefold-icon.svg on its 24x24 view box: two
// overlapping rounded squares, a light one behind and a teal one in front,
// with their overlap in a darker teal (many folders, one copy), on a
// transparent background.
var (
	backColor  = color.NRGBA{0x9f, 0xd3, 0xcc, 0xff}
	frontColor = color.NRGBA{0x0f, 0x76, 0x6e, 0xff}
	overColor  = color.NRGBA{0x0b, 0x52, 0x4c, 0xff}
)

const viewBox = 24.0

type pt struct{ x, y float64 }

// roundRect is an axis-aligned rectangle with rounded corners (r = 0: square).
type roundRect struct{ x, y, w, h, r float64 }

// The shapes, painted in this order.
var (
	backRect  = roundRect{2, 7, 13, 13, 3}
	frontRect = roundRect{9, 2, 13, 13, 3}
	overlap   = roundRect{9, 7, 6, 8, 0}
)

// Logo rasterizes the logo at size x size pixels, 8x8 samples per pixel.
func Logo(size int) *image.NRGBA {
	const ss = 8
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	k := viewBox / float64(size)
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var in int
			var r, g, b int
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					p := pt{float64((float64(px) + (float64(sx)+0.5)/ss) * k), float64((float64(py) + (float64(sy)+0.5)/ss) * k)}
					c, ok := shade(p)
					if !ok {
						continue
					}
					in++
					r, g, b = r+int(c.R), g+int(c.G), b+int(c.B)
				}
			}
			if in == 0 {
				continue
			}
			img.SetNRGBA(px, py, color.NRGBA{uint8((r + in/2) / in), uint8((g + in/2) / in), uint8((b + in/2) / in),
				uint8((255*in + ss*ss/2) / (ss * ss))})
		}
	}
	return img
}

// shade is the color painted at p (ok = false: transparent).
func shade(p pt) (color.NRGBA, bool) {
	switch {
	case overlap.has(p):
		return overColor, true
	case frontRect.has(p):
		return frontColor, true
	case backRect.has(p):
		return backColor, true
	}
	return color.NRGBA{}, false
}

// has reports whether p is inside the rectangle. Explicit conversions round
// each product: no fused multiply-add (arm64 would fuse it), so the pixels,
// and the committed .syso, match on every architecture.
func (q roundRect) has(p pt) bool {
	if p.x < q.x || p.x > q.x+q.w || p.y < q.y || p.y > q.y+q.h {
		return false
	}
	if q.r == 0 {
		return true
	}
	cx := math.Max(q.x+q.r, math.Min(q.x+q.w-q.r, p.x))
	cy := math.Max(q.y+q.r, math.Min(q.y+q.h-q.r, p.y))
	return float64((p.x-cx)*(p.x-cx))+float64((p.y-cy)*(p.y-cy)) <= q.r*q.r
}

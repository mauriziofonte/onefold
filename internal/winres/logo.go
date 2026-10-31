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

// The logo is the GUI favicon (internal/gui/web/index.html) on a 24x24 view
// box: an indigo rounded square with three white stroked "layers".
var (
	indigo = color.NRGBA{0x4f, 0x46, 0xe5, 0xff}
	white  = color.NRGBA{0xff, 0xff, 0xff, 0xff}
)

const (
	viewBox     = 24.0
	cornerR     = 6.0
	strokeWidth = 1.6
)

type pt struct{ x, y float64 }

// polyline is one stroked path; closed ones join their last point to the first.
type polyline struct {
	pts    []pt
	closed bool
}

var layers = []polyline{
	{[]pt{{6, 8}, {12, 5}, {18, 8}, {12, 11}}, true},
	{[]pt{{6, 12}, {12, 15}, {18, 12}}, false},
	{[]pt{{6, 16}, {12, 19}, {18, 16}}, false},
}

// Logo rasterizes the logo at size x size pixels, 8x8 samples per pixel.
func Logo(size int) *image.NRGBA {
	const ss = 8
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	k := viewBox / float64(size)
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var in, stroke int
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					p := pt{(float64(px) + float64((float64(sx)+0.5)/ss)) * k, (float64(py) + float64((float64(sy)+0.5)/ss)) * k}
					if !inRoundedSquare(p) {
						continue
					}
					in++
					if onStroke(p) {
						stroke++
					}
				}
			}
			if in == 0 {
				continue
			}
			mix := func(a, b uint8) uint8 {
				return uint8((int(a)*(in-stroke) + int(b)*stroke + in/2) / in)
			}
			img.SetNRGBA(px, py, color.NRGBA{mix(indigo.R, white.R), mix(indigo.G, white.G), mix(indigo.B, white.B),
				uint8((255*in + ss*ss/2) / (ss * ss))})
		}
	}
	return img
}

func inRoundedSquare(p pt) bool {
	cx := math.Max(cornerR, math.Min(viewBox-cornerR, p.x))
	cy := math.Max(cornerR, math.Min(viewBox-cornerR, p.y))
	// explicit conversions round each product: no fused multiply-add (arm64
	// would fuse it), so the pixels, and the committed .syso, match on every
	// architecture
	return float64((p.x-cx)*(p.x-cx))+float64((p.y-cy)*(p.y-cy)) <= cornerR*cornerR
}

// onStroke: within half the stroke width of a segment; round joins, butt caps
// at the open ends (the SVG defaults with stroke-linejoin="round").
func onStroke(p pt) bool {
	for _, l := range layers {
		n := len(l.pts)
		segs := n - 1
		if l.closed {
			segs = n
		}
		for i := 0; i < segs; i++ {
			a, b := l.pts[i], l.pts[(i+1)%n]
			startCap := !l.closed && i == 0
			endCap := !l.closed && i == segs-1
			if segDist(p, a, b, startCap, endCap) <= strokeWidth/2 {
				return true
			}
		}
	}
	return false
}

// segDist is the distance from p to segment ab; a butt cap end does not
// extend past its endpoint.
func segDist(p, a, b pt, buttA, buttB bool) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	t := (float64((p.x-a.x)*dx) + float64((p.y-a.y)*dy)) / (float64(dx*dx) + float64(dy*dy))
	if (t < 0 && buttA) || (t > 1 && buttB) {
		return math.Inf(1)
	}
	t = math.Max(0, math.Min(1, t))
	qx, qy := a.x+float64(t*dx)-p.x, a.y+float64(t*dy)-p.y
	return math.Sqrt(float64(qx*qx) + float64(qy*qy))
}

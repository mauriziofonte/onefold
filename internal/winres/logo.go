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

// The logo is assets/brand/onefold-icon.svg on its 24x24 view box: a rounded
// square with a vertical indigo gradient and a white "1" whose flag is a
// folded flap of paper (a light face over a soft crease shadow).
var (
	gradTop    = color.NRGBA{0x63, 0x66, 0xf1, 0xff}
	gradBottom = color.NRGBA{0x43, 0x38, 0xca, 0xff}
	white      = color.NRGBA{0xff, 0xff, 0xff, 0xff}
	flapColor  = color.NRGBA{0xc7, 0xd2, 0xfe, 0xff}
	creaseInk  = color.NRGBA{0x1e, 0x1b, 0x4b, 0xff}
)

const (
	viewBox      = 24.0
	cornerR      = 5.5
	creaseOpaque = 0.3
)

type pt struct{ x, y float64 }

// The shapes, all convex, listed clockwise in SVG coordinates (y down).
var (
	stem   = []pt{{12.25, 5}, {16.25, 5}, {16.25, 19}, {12.25, 19}}
	flap   = []pt{{12.25, 5}, {12.25, 9.5}, {7.75, 9.5}}
	crease = []pt{{7.75, 9.5}, {12.25, 9.5}, {12.25, 11}}
)

// Logo rasterizes the logo at size x size pixels, 8x8 samples per pixel.
func Logo(size int) *image.NRGBA {
	const ss = 8
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	k := viewBox / float64(size)
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var in int
			var r, g, b float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					p := pt{float64((float64(px) + (float64(sx)+0.5)/ss) * k), float64((float64(py) + (float64(sy)+0.5)/ss) * k)}
					if !inRoundedSquare(p) {
						continue
					}
					in++
					c := shade(p)
					r, g, b = r+c[0], g+c[1], b+c[2]
				}
			}
			if in == 0 {
				continue
			}
			n := float64(in)
			img.SetNRGBA(px, py, color.NRGBA{to8(r / n), to8(g / n), to8(b / n), uint8((255*in + ss*ss/2) / (ss * ss))})
		}
	}
	return img
}

// shade is the color at p inside the rounded square.
func shade(p pt) [3]float64 {
	switch {
	case inside(p, stem):
		return rgb(white)
	case inside(p, flap):
		return rgb(flapColor)
	}
	bg := mix(rgb(gradTop), rgb(gradBottom), p.y/viewBox)
	if inside(p, crease) {
		return mix(bg, rgb(creaseInk), creaseOpaque)
	}
	return bg
}

func rgb(c color.NRGBA) [3]float64 { return [3]float64{float64(c.R), float64(c.G), float64(c.B)} }

// mix goes from a (t=0) to b (t=1). Explicit conversions round each product:
// no fused multiply-add (arm64 would fuse it), so the pixels, and the
// committed .syso, match on every architecture.
func mix(a, b [3]float64, t float64) [3]float64 {
	var o [3]float64
	for i := range o {
		o[i] = a[i] + float64((b[i]-a[i])*t)
	}
	return o
}

func to8(v float64) uint8 { return uint8(math.Max(0, math.Min(255, math.Round(v)))) }

// inside: p is on the inner side of every edge of the clockwise convex
// polygon (y down).
func inside(p pt, poly []pt) bool {
	for i, a := range poly {
		b := poly[(i+1)%len(poly)]
		if float64((b.x-a.x)*(p.y-a.y))-float64((b.y-a.y)*(p.x-a.x)) < 0 {
			return false
		}
	}
	return true
}

func inRoundedSquare(p pt) bool {
	cx := math.Max(cornerR, math.Min(viewBox-cornerR, p.x))
	cy := math.Max(cornerR, math.Min(viewBox-cornerR, p.y))
	return float64((p.x-cx)*(p.x-cx))+float64((p.y-cy)*(p.y-cy)) <= cornerR*cornerR
}

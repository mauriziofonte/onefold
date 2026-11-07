// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import (
	"regexp"
	"strconv"
)

// setGeo records the place a file's own metadata gives (EXIF GPS, a video's
// ISO 6709 location); the first valid one wins.
func setGeo(m *Meta, lat, lon float64) {
	if m.Lat != 0 || m.Lon != 0 || !validGeo(lat, lon) {
		return
	}
	m.Lat, m.Lon = round6(lat), round6(lon)
}

// reISO6709: the latitude and longitude at the start of an ISO 6709 point,
// each with its sign ("+41.8919+012.5113+050.000/").
var reISO6709 = regexp.MustCompile(`^([+-])(\d+)(\.\d+)?([+-])(\d+)(\.\d+)?`)

// parseISO6709 reads the point of an ISO 6709 string, the form videos carry
// their place in (QuickTime ©xyz, com.apple.quicktime.location.ISO6709):
// degrees ("+41.8919"), degrees and minutes ("+4153.51") or degrees,
// minutes and seconds ("+415330.6"), told apart by the digits before the
// decimal point (2, 4, 6 for a latitude; 3, 5, 7 for a longitude).
func parseISO6709(s string) (lat, lon float64, ok bool) {
	g := reISO6709.FindStringSubmatch(s)
	if g == nil {
		return 0, 0, false
	}
	lat, ok1 := iso6709Part(g[1], g[2], g[3], 2)
	lon, ok2 := iso6709Part(g[4], g[5], g[6], 3)
	if !ok1 || !ok2 || !validGeo(lat, lon) {
		return 0, 0, false
	}
	return lat, lon, true
}

func iso6709Part(sign, whole, frac string, degDigits int) (float64, bool) {
	var deg, mins, secs float64
	switch len(whole) {
	case degDigits:
		deg, _ = strconv.ParseFloat(whole+frac, 64)
	case degDigits + 2:
		deg, _ = strconv.ParseFloat(whole[:degDigits], 64)
		mins, _ = strconv.ParseFloat(whole[degDigits:]+frac, 64)
	case degDigits + 4:
		deg, _ = strconv.ParseFloat(whole[:degDigits], 64)
		mins, _ = strconv.ParseFloat(whole[degDigits:degDigits+2], 64)
		secs, _ = strconv.ParseFloat(whole[degDigits+2:]+frac, 64)
	default:
		return 0, false
	}
	if mins >= 60 || secs >= 60 {
		return 0, false
	}
	v := deg + mins/60 + secs/3600
	if sign == "-" {
		v = -v
	}
	return v, true
}

// videoGeo reads the place of a video from its tags (ffprobe format tags or
// the pure-Go ISO-BMFF reader's, whose keys are lower case).
func videoGeo(m *Meta, tags map[string]string) {
	for _, k := range []string{"com.apple.quicktime.location.ISO6709", "com.apple.quicktime.location.iso6709", "location", "location-eng"} {
		if lat, lon, ok := parseISO6709(tags[k]); ok {
			setGeo(m, lat, lon)
			return
		}
	}
}

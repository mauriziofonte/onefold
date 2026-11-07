// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package geo turns the place where a photo or a video was taken (latitude
// and longitude) into a city and a country, offline: the tables are in the
// binary and nothing leaves the computer.
//
// Cities come from GeoNames (https://www.geonames.org), CC BY 4.0: the
// populated places of at least 5000 people and every capital and seat of a
// first-order division, without the sections of a city (see DATA.md and
// gen/main.go). Country names come from the Unicode CLDR through
// golang.org/x/text, in English and Italian.
//
// The city of a point is the one with the smallest distance divided by a
// weight that grows slowly with its population ((pop/5000)^(1/4), at least
// 1), among the cities within CityMaxKm: a point in the middle of a large
// city is not given to a small town next to its centre, a point in a small
// town still is. The country is the city's; with no city in reach, the one
// of the nearest place within CountryMaxKm. Near a border the nearest place
// can be across it: the country is approximate there.
package geo

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"math"
	"strconv"
	"strings"
	"sync"
)

//go:embed cities.tsv.gz
var citiesGz []byte

//go:embed countries.tsv
var countriesTSV string

// The reach of a city and of a country.
const (
	CityMaxKm    = 30
	CountryMaxKm = 100
)

// Place is where a point is: a city name as GeoNames writes it ("" when no
// city is in reach) and an ISO 3166-1 alpha-2 country code ("" at sea or
// far from any place).
type Place struct {
	City        string
	CountryCode string
}

type city struct {
	name     string
	cc       string
	lat, lon float64
	w        float64
}

const cellDeg = 0.5

var (
	once      sync.Once
	cities    []city
	grid      map[int][]int32
	countries map[string][2]string // code -> English, Italian

	cacheMu sync.Mutex
	cache   = map[[2]int32]Place{}
)

func load() {
	r, err := gzip.NewReader(bytes.NewReader(citiesGz))
	if err != nil {
		panic("geo: cities table: " + err.Error())
	}
	sc := bufio.NewScanner(r)
	grid = map[int][]int32{}
	for sc.Scan() {
		f := strings.Split(sc.Text(), "\t")
		if len(f) != 5 {
			continue
		}
		lat, _ := strconv.ParseFloat(f[2], 64)
		lon, _ := strconv.ParseFloat(f[3], 64)
		pop, _ := strconv.ParseFloat(f[4], 64)
		c := city{name: f[0], cc: f[1], lat: lat, lon: lon, w: math.Pow(math.Max(1, pop/5000), 0.25)}
		k := cell(lat, lon)
		grid[k] = append(grid[k], int32(len(cities)))
		cities = append(cities, c)
	}
	countries = map[string][2]string{}
	for _, line := range strings.Split(countriesTSV, "\n") {
		if f := strings.Split(line, "\t"); len(f) == 3 {
			countries[f[0]] = [2]string{f[1], f[2]}
		}
	}
}

const lonCells = int(360 / cellDeg)

func cell(lat, lon float64) int {
	y := int(math.Floor((lat + 90) / cellDeg))
	x := int(math.Floor((lon+180)/cellDeg)) % lonCells
	if x < 0 {
		x += lonCells
	}
	return y*lonCells + x
}

// km is the great-circle distance (haversine, mean Earth radius).
func km(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371.0088
	p1, p2 := lat1*math.Pi/180, lat2*math.Pi/180
	dp, dl := p2-p1, (lon2-lon1)*math.Pi/180
	a := math.Sin(dp/2)*math.Sin(dp/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dl/2)*math.Sin(dl/2)
	return 2 * r * math.Asin(math.Min(1, math.Sqrt(a)))
}

// Valid reports whether lat, lon is a point (0,0 is "unknown").
func Valid(lat, lon float64) bool {
	return (lat != 0 || lon != 0) && lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}

// Lookup returns the place of a point; ok=false when the point is not valid
// or nothing is in reach. Safe for concurrent use; the tables are read on
// the first call.
func Lookup(lat, lon float64) (Place, bool) {
	if !Valid(lat, lon) {
		return Place{}, false
	}
	key := [2]int32{int32(math.Round(lat * 1e4)), int32(math.Round(lon * 1e4))}
	cacheMu.Lock()
	p, hit := cache[key]
	cacheMu.Unlock()
	if hit {
		return p, p.CountryCode != ""
	}
	once.Do(load)
	best, near, nearKm := scan(lat, lon, CityMaxKm)
	if best < 0 {
		_, near, nearKm = scan(lat, lon, CountryMaxKm)
	}
	switch {
	case best >= 0:
		p = Place{City: cities[best].name, CountryCode: cities[best].cc}
	case near >= 0 && nearKm <= CountryMaxKm:
		p = Place{CountryCode: cities[near].cc}
	}
	cacheMu.Lock()
	if len(cache) > 1<<16 {
		clear(cache)
	}
	cache[key] = p
	cacheMu.Unlock()
	return p, p.CountryCode != ""
}

// scan looks at the cells within reach km of a point: the best city (within
// CityMaxKm, by distance over weight) and the nearest place.
func scan(lat, lon, reach float64) (best, near int, nearKm float64) {
	dLat := reach / 111.0
	dLon := 180.0
	if c := math.Cos(lat * math.Pi / 180); c > dLat/90 {
		dLon = math.Min(180, dLat/c)
	}
	best, near, nearKm = -1, -1, math.Inf(1)
	bestScore := math.Inf(1)
	seen := map[int]bool{}
	for y := lat - dLat; y <= lat+dLat+cellDeg; y += cellDeg {
		if y < -90-cellDeg || y > 90+cellDeg {
			continue
		}
		for x := lon - dLon; x <= lon+dLon+cellDeg; x += cellDeg {
			k := cell(math.Max(-90, math.Min(89.999, y)), x)
			if seen[k] {
				continue
			}
			seen[k] = true
			for _, i := range grid[k] {
				c := &cities[i]
				d := km(lat, lon, c.lat, c.lon)
				if d < nearKm {
					near, nearKm = int(i), d
				}
				if d <= CityMaxKm {
					if s := d / c.w; s < bestScore {
						best, bestScore = int(i), s
					}
				}
			}
		}
	}
	return best, near, nearKm
}

// Country names in the languages of folder and file names.
const (
	English = "en"
	Italian = "it"
)

// CountryName returns the name of a country code in lang (English or
// Italian; anything else is English), or the code when it is unknown.
func CountryName(code, lang string) string {
	once.Do(load)
	n, ok := countries[code]
	if !ok {
		return code
	}
	if lang == Italian {
		return n[1]
	}
	return n[0]
}

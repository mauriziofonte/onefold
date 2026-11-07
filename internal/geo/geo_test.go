// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package geo

import (
	"sync"
	"testing"
)

// Points of public landmarks (no personal data): the city and the country
// they are given. A landmark inside a large city stays in it even when a
// smaller place is nearer to it than the city's centre; a point at sea or in
// the middle of an ocean has no city, far from land no country either.
func TestLookup(t *testing.T) {
	for _, c := range []struct {
		name     string
		lat, lon float64
		city, cc string
	}{
		{"Colosseum", 41.8902, 12.4922, "Rome", "IT"},
		{"Trastevere", 41.8897, 12.4700, "Rome", "IT"},
		{"St Peter's Square", 41.9022, 12.4568, "Vatican City", "VA"},
		{"Eiffel Tower", 48.8584, 2.2945, "Paris", "FR"},
		{"Louvre", 48.8606, 2.3376, "Paris", "FR"},
		{"Milan Cathedral", 45.4642, 9.1916, "Milan", "IT"},
		{"San Siro stadium", 45.4781, 9.1240, "Milan", "IT"},
		{"Shibuya crossing", 35.6595, 139.7005, "Tokyo", "JP"},
		{"Bolzano", 46.4983, 11.3548, "Bolzano", "IT"},
		{"Sydney Opera House", -33.8568, 151.2153, "Sydney", "AU"},
		{"Christ the Redeemer", -22.9519, -43.2105, "Rio de Janeiro", "BR"},
		{"Labasa", -16.4300, 179.3700, "Labasa", "FJ"},
		{"across the antimeridian from Labasa", -16.45, -179.95, "", "FJ"},
		{"Mid-Atlantic", 30.0, -40.0, "", ""},
	} {
		p, ok := Lookup(c.lat, c.lon)
		if p.City != c.city || p.CountryCode != c.cc || ok != (c.cc != "") {
			t.Errorf("%s: %+v %v, want %s %s", c.name, p, ok, c.city, c.cc)
		}
	}
	if _, ok := Lookup(0, 0); ok {
		t.Error("0,0 is unknown, not a place")
	}
	if _, ok := Lookup(91, 10); ok {
		t.Error("latitude 91 accepted")
	}
	if g, i := CountryName("IT", English), CountryName("IT", Italian); g != "Italy" || i != "Italia" {
		t.Errorf("country names %q %q", g, i)
	}
	if CountryName("QQ", English) != "QQ" {
		t.Error("an unknown code is not kept")
	}
}

// Concurrent lookups share the tables and the cache.
func TestLookupConcurrent(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				if p, _ := Lookup(41.89+float64(j%10)/1000, 12.49); p.City != "Rome" {
					t.Errorf("%+v", p)
					return
				}
			}
		}(i)
	}
	wg.Wait()
}

// BenchmarkLookup: a lookup of a new point in a dense area (no cache hit).
func BenchmarkLookup(b *testing.B) {
	Lookup(45, 9)
	for i := 0; i < b.N; i++ {
		cacheMu.Lock()
		clear(cache)
		cacheMu.Unlock()
		Lookup(45.0+float64(i%1000)/1e3, 9.1)
	}
}

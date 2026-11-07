// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build ignore

// gen writes the place tables of internal/geo from a GeoNames cities dump
// (cities1000.txt or cities500.txt, the tab-separated format of
// https://download.geonames.org/export/dump/) and the country names of the
// Unicode CLDR that golang.org/x/text carries:
//
//	go run internal/geo/gen/main.go -cities cities1000.txt -o internal/geo
//
// Kept: populated places (feature class P) of at least MinPop people, and
// every capital and seat of a first-order division whatever its size.
// Left out: sections of a city (PPLX, the neighbourhoods that would split a
// city into many folders) and historical, abandoned, destroyed, religious
// and farm places.
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// MinPop is the smallest population kept.
const MinPop = 5000

var dropped = map[string]bool{"PPLX": true, "PPLH": true, "PPLQ": true, "PPLW": true, "PPLCH": true, "PPLF": true, "PPLR": true}

type city struct {
	name, cc string
	lat, lon float64
	pop      int
}

func main() {
	in := flag.String("cities", "", "GeoNames cities dump (cities1000.txt format)")
	out := flag.String("o", "internal/geo", "folder of cities.tsv.gz and countries.tsv")
	flag.Parse()
	f, err := os.Open(*in)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	var cs []city
	ccs := map[string]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for sc.Scan() {
		r := strings.Split(sc.Text(), "\t")
		if len(r) < 19 || r[6] != "P" || dropped[r[7]] || len(r[8]) != 2 {
			continue
		}
		pop, _ := strconv.Atoi(r[14])
		if pop < MinPop && r[7] != "PPLC" && r[7] != "PPLA" {
			continue
		}
		lat, err1 := strconv.ParseFloat(r[4], 64)
		lon, err2 := strconv.ParseFloat(r[5], 64)
		name := strings.TrimSpace(r[1])
		if err1 != nil || err2 != nil || name == "" || strings.ContainsAny(name, "\t\n") {
			continue
		}
		cs = append(cs, city{name, r[8], lat, lon, pop})
		ccs[r[8]] = true
	}
	if err := sc.Err(); err != nil {
		log.Fatal(err)
	}
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].cc != cs[j].cc {
			return cs[i].cc < cs[j].cc
		}
		if cs[i].lat != cs[j].lat {
			return cs[i].lat < cs[j].lat
		}
		if cs[i].lon != cs[j].lon {
			return cs[i].lon < cs[j].lon
		}
		return cs[i].name < cs[j].name
	})
	var b bytes.Buffer
	for _, c := range cs {
		fmt.Fprintf(&b, "%s\t%s\t%.4f\t%.4f\t%d\n", c.name, c.cc, c.lat, c.lon, c.pop)
	}
	var gz bytes.Buffer
	w, _ := gzip.NewWriterLevel(&gz, gzip.BestCompression)
	w.Write(b.Bytes())
	w.Close()
	if err := os.WriteFile(filepath.Join(*out, "cities.tsv.gz"), gz.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
	var codes []string
	for c := range ccs {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	var t bytes.Buffer
	en, it := display.Regions(language.English), display.Regions(language.Italian)
	for _, c := range codes {
		name := func(n display.Namer) string {
			r, err := language.ParseRegion(c)
			if err != nil {
				return c
			}
			if s := n.Name(r); s != "" {
				return s
			}
			return c
		}
		fmt.Fprintf(&t, "%s\t%s\t%s\n", c, name(en), name(it))
	}
	if err := os.WriteFile(filepath.Join(*out, "countries.tsv"), t.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("%d places in %d countries; cities.tsv.gz %d bytes", len(cs), len(codes), gz.Len())
}

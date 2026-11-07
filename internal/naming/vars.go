// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package naming

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mauriziofonte/onefold/internal/catalog"
	"github.com/mauriziofonte/onefold/internal/geo"
	"github.com/mauriziofonte/onefold/internal/kinds"
	"github.com/mauriziofonte/onefold/internal/meta"
	"github.com/mauriziofonte/onefold/internal/names"
)

// Vars holds the placeholder values of one file.
type Vars map[string]string

// Get returns a value ("" if unknown).
func (v Vars) Get(k string) string { return v[k] }

// SetExt overrides the extension placeholders (e.g. with the extension implied
// by the content when the name lies).
func (v Vars) SetExt(ext string) {
	low := strings.ToLower(ext)
	v["ext"], v["ext-raw"], v["ext-norm"] = low, ext, kinds.NormalizeExt(low)
	v["EXT"], v["EXT-NORM"] = strings.ToUpper(low), strings.ToUpper(v["ext-norm"])
	v["group"], v["format"] = kinds.Group(low), kinds.Format(low)
	if low == "" {
		v["format"] = ""
	}
}

// Placeholder documentation, also the list of known names.
var Placeholders = [][2]string{
	{"ext", "extension, lowercase"},
	{"ext-norm", "extension lowercase, harmonized by the type registry (jpeg->jpg, tif->tiff, htm->html, yml->yaml, mpeg->mpg...)"},
	{"ext-raw", "extension exactly as in the source name"},
	{"EXT", "extension uppercase (folder style)"},
	{"EXT-NORM", "ext-norm uppercase"},
	{"stem", "file name without extension, untouched"},
	{"stem-norm", "stem without tool markers ('(yyyyMMdd_HHmmss) ' prefix, ' -- frev.N', ' (N)', collision '_NN' when the base file sits next to it); keeps the '[origin] ' prefix"},
	{"stem-clean", "stem-norm without the '[origin] ' prefix too"},
	{"origin", "origin folder: from an '[origin] ' name prefix, else the current parent folder"},
	{"origin-1", "folder prefix of level 1: the '[origin] ' name prefix, else the parent folder"},
	{"origin-2", "the '[origin] ' name prefix, else the folder 2 levels up (grandparent)"},
	{"origin-3", "the '[origin] ' name prefix, else the folder 3 levels up; a drive root counts as a folder (D:\\ -> D)"},
	{"origin-4", "as origin-3, 4 levels up"},
	{"origin-5", "as origin-3, 5 levels up"},
	{"origin-6", "as origin-3, 6 levels up"},
	{"origin-7", "as origin-3, 7 levels up"},
	{"origin-8", "as origin-3, 8 levels up"},
	{"origin-9", "as origin-3, 9 levels up"},
	{"parent", "current parent folder name"},
	{"year", "canonical year (metadata -> filename -> mtime); empty if no valid date"},
	{"month", "MM"},
	{"month-name", "January .. December, or Gennaio .. Dicembre (config names-language)"},
	{"month-name-en", "January .. December"},
	{"month-name-it", "Gennaio .. Dicembre"},
	{"day", "DD"},
	{"date", "yyyyMMdd"},
	{"time", "HHmmss"},
	{"datetime", "yyyyMMdd_HHmmss"},
	{"mtime-datetime", "file modification time as yyyyMMdd_HHmmss (canonical zone)"},
	{"name-date", "yyyyMMdd_HHmmss of a '(yyyyMMdd_HHmmss) ' name prefix"},
	{"pre-2000", "'_FORMER' when the year is before 2000, else empty"},
	{"kind", "photo, video, image, audio, document, text, archive, other"},
	{"group", "layout family from the type registry (PDF DOC XLS PPT TXT EBOOK EMAIL IMAGE RAW VIDEO AUDIO ARCHIVE CODE ...)"},
	{"category", "fine category from the type registry (raster_image, spreadsheet, source_code, ...)"},
	{"format", "container/parser family from the type registry (jpeg, isobmff, ooxml-doc, zip, ...)"},
	{"prefix", "canonical media prefix (IMG, DSC, VID, PS, SNPSD, EDT, ENL)"},
	{"photo-prefix", "prefix when the image has device/software metadata, else PIX"},
	{"type-prefix", "IMG VID AUD DOC ARC FILE by kind"},
	{"size-bucket", "0MB+ 5MB+ 10MB+ 20MB+ 50MB+ 100MB+ 200MB+ 500MB+ 1GB+"},
	{"device", "canonical device (Nikon D3500, OnePlus 9 Pro ...)"},
	{"device-slug", "device with spaces as underscores"},
	{"device-category", "Smartphone, Camera, Snapseed, Photoshop, Editing, Enlargement"},
	{"tier", "video tier by short side: 4K 1080p 720p 480p SD"},
	{"author", "document author / audio artist"},
	{"title", "document / audio title"},
	{"artist", "audio artist"},
	{"album", "audio album"},
	{"album-year", "audio release year tag"},
	{"track-num", "NN, or DNN when disc > 1"},
	{"country", "country where a photo or video was taken (GPS; catalog -places), in names-language: Italy or Italia"},
	{"country-en", "country in English"},
	{"country-it", "country in Italian"},
	{"country-code", "ISO country code (IT, FR, US...)"},
	{"city", "nearest city or town of 5000+ people within 30 km, as GeoNames writes it (Rome, Munich...)"},
}

var knownPH = func() map[string]bool {
	m := map[string]bool{}
	for _, p := range Placeholders {
		m[p[0]] = true
	}
	return m
}()

// Known reports whether name is a placeholder.
func Known(name string) bool { return knownPH[name] }

// value sanitation: a placeholder value is ONE path component and never a
// Windows device name, even when composed ("AUD_120000_CON_").
func clean(s string) string {
	return names.DeReserve(strings.TrimSpace(strings.NewReplacer("/", "_", "\\", "_").Replace(s)))
}

// VarsOf is VarsOfCtx without sibling knowledge (a trailing _NN is kept).
func VarsOf(r catalog.Record) Vars { return VarsOfCtx(r, nil) }

// VarsOfCtx builds the placeholder values of a cataloged file. exists reports
// whether a path is a file of the same tree (case-insensitive); with it, a
// trailing "_NN" is treated as a collision marker (dropped from {stem-norm} and
// {stem-clean}) only when the same name without it exists in the directory.
func VarsOfCtx(r catalog.Record, exists func(path string) bool) Vars {
	m := r.Meta
	base := filepath.Base(r.Path)
	stem, dotExt := names.SplitName(base)
	rawExt := strings.TrimPrefix(dotExt, ".")
	lg := names.ParseLegacy(stem)
	norm, cln := lg.Norm, lg.Clean
	if lg.CleanNoNN != "" && exists != nil && siblingExists(r.Path, rawExt, stem, lg, exists) {
		norm, cln = lg.NormNoNN, lg.CleanNoNN
	}
	parent := clean(filepath.Base(filepath.Dir(r.Path)))
	v := Vars{
		"stem":       clean(stem),
		"stem-norm":  clean(norm),
		"stem-clean": clean(cln),
		"parent":     parent,
		"origin":     parent,
		"kind":       m.Kind,
		"category":   m.FileCategory,
		"author":     clean(m.Author),
		"title":      clean(m.Title),
		"album":      clean(m.Album),
		"album-year": m.TagYear,
		"name-date":  lg.NameDate,
	}
	v.SetExt(rawExt)
	if v["category"] == "" {
		v["category"] = kinds.Category(rawExt)
	}
	if lg.Origin != "" {
		v["origin"] = clean(lg.Origin)
	}
	for n := 1; n <= MaxOriginLevel; n++ {
		o := clean(lg.Origin)
		if o == "" {
			o = clean(Ancestor(r.Path, n))
		}
		if o == "" {
			break
		}
		v["origin-"+strconv.Itoa(n)] = o
	}
	if m.Kind == kinds.Audio {
		v["artist"] = clean(m.Author)
	}
	if t, err := time.Parse("2006-01-02 15:04:05", m.Time); err == nil && m.TimeSource != "none" {
		v["year"] = t.Format("2006")
		v["month"] = t.Format("01")
		v["month-name"] = monthNames()[t.Month()-1]
		v["month-name-en"] = names.MonthEN[t.Month()-1]
		v["month-name-it"] = names.MonthIT[t.Month()-1]
		v["day"] = t.Format("02")
		v["date"] = t.Format("20060102")
		v["time"] = t.Format("150405")
		v["datetime"] = t.Format("20060102_150405")
		if t.Year() < 2000 {
			v["pre-2000"] = "_FORMER"
		}
	}
	if r.MTimeNs > 0 {
		v["mtime-datetime"] = time.Unix(0, r.MTimeNs).In(meta.Zone()).Format("20060102_150405")
	}
	switch m.Kind {
	case kinds.Photo, kinds.Image:
		v["prefix"] = orDefault(m.Prefix, "IMG")
		if m.CanonDevice != "" || m.Device != "" || m.Category != "" {
			v["photo-prefix"] = v["prefix"]
		} else {
			v["photo-prefix"] = "PIX"
		}
	case kinds.Video:
		v["prefix"] = "VID"
	}
	v["type-prefix"] = names.TypePrefix(m.Kind)
	v["size-bucket"] = names.SizeBucket(r.Size)
	dev := clean(firstNonEmpty(m.CanonDevice, m.Device))
	v["device"] = dev
	v["device-slug"] = strings.Join(strings.Fields(dev), "_")
	if m.Category != "" {
		v["device-category"] = strings.ToUpper(m.Category[:1]) + m.Category[1:]
	}
	if m.Kind == kinds.Video {
		v["tier"] = names.VideoTier(m.Width, m.Height)
	}
	if p, ok := geo.Lookup(m.Lat, m.Lon); ok {
		v["country-code"] = p.CountryCode
		v["country-en"] = clean(names.Sanitize(geo.CountryName(p.CountryCode, geo.English), 100))
		v["country-it"] = clean(names.Sanitize(geo.CountryName(p.CountryCode, geo.Italian), 100))
		v["country"] = v["country-en"]
		if l, _ := namesLanguage.Load().(string); l == NamesIT {
			v["country"] = v["country-it"]
		}
		v["city"] = clean(names.Sanitize(p.City, 100))
	}
	switch {
	case m.Track > 0 && m.Disc > 1:
		v["track-num"] = fmt.Sprintf("%d%02d", m.Disc, m.Track)
	case m.Track > 0:
		v["track-num"] = fmt.Sprintf("%02d", m.Track)
	}
	return v
}

// MaxOriginLevel is the highest {origin-N} level.
const MaxOriginLevel = 9

// Ancestor returns the name of the folder n levels above path (1 = parent).
// A drive root counts as a folder named after its letter (D:\ -> D), a UNC
// root after its share; "" above the top.
func Ancestor(path string, n int) string {
	d := filepath.Dir(path)
	for i := 1; i < n; i++ {
		up := filepath.Dir(d)
		if up == d {
			return ""
		}
		d = up
	}
	if vol := filepath.VolumeName(d); vol != "" && len(d) <= len(vol)+1 {
		vol = strings.TrimSuffix(vol, ":")
		if i := strings.LastIndexAny(vol, `\/`); i >= 0 {
			vol = vol[i+1:]
		}
		return vol
	}
	b := filepath.Base(d)
	if b == "." || b == string(filepath.Separator) || b == "/" {
		return ""
	}
	return b
}

// siblingExists reports whether the name without its trailing _NN exists next
// to p: as the raw name ("[x] a_01" -> "[x] a") or marker-free ("a_01 (1)" -> "a").
func siblingExists(p, ext, stem string, lg names.Legacy, exists func(string) bool) bool {
	dir := filepath.Dir(p)
	cands := []string{lg.CleanNoNN, lg.NormNoNN}
	if strings.HasSuffix(stem, lg.Clean) {
		cands = append(cands, strings.TrimSuffix(stem, lg.Clean)+lg.CleanNoNN)
	}
	for _, c := range cands {
		if c == "" {
			continue
		}
		if ext != "" {
			c += "." + ext
		}
		if exists(filepath.Join(dir, c)) {
			return true
		}
	}
	return false
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// sanitizeSegment makes one rendered path component portable.
func sanitizeSegment(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "." || s == ".." {
		return ""
	}
	return names.SanitizeName(s)
}

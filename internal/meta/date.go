// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import (
	"math"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Canonical timestamp logic. Output format is the wall-clock string
// "2006-01-02 15:04:05" (naive local capture time).

const canonFmt = "2006-01-02 15:04:05"

// yearOK clamps to the validity window [1985, now+1].
func yearOK(y int) bool { return y >= 1985 && y <= time.Now().Year()+1 }

// CanonicalVideo derives the capture time from a video's raw creation_time
// (RFC3339, typically UTC = end-of-mux) minus the clip duration, expressed in
// canonical-zone wall-clock (Zone).
func CanonicalVideo(creationRFC3339 string, durationSec float64) (string, bool) {
	if creationRFC3339 == "" {
		return "", false
	}
	t, err := time.Parse(time.RFC3339, creationRFC3339)
	if err != nil {
		// some muxers omit the 'Z'; try a plain layout in UTC
		if t, err = time.ParseInLocation("2006-01-02T15:04:05", creationRFC3339, time.UTC); err != nil {
			return "", false
		}
	}
	if durationSec > 0 {
		t = t.Add(-time.Duration(int64(math.Floor(durationSec))) * time.Second) // end -> start
	}
	local := t.In(Zone())
	if !yearOK(local.Year()) {
		return "", false
	}
	return local.Format(canonFmt), true
}

// CanonicalPhoto takes imagemeta's DateTimeOriginal (already a local wall-clock,
// with sub-second) and returns its clock string. No TZ shift: EXIF is local.
func CanonicalPhoto(t time.Time) (string, bool) {
	if t.IsZero() || !yearOK(t.Year()) {
		return "", false
	}
	return t.Format(canonFmt), true
}

// Filename patterns, in priority order (first match wins). sep is any of
// . - _ space, optional.
var (
	sep        = `[.\-_ ]?`
	reDateTime = regexp.MustCompile(`(\d{4})` + sep + `(\d{2})` + sep + `(\d{2})` + sep + `(\d{2})[.\-_: ]?(\d{2})[.\-_: ]?(\d{2})`)
	reWhatsApp = regexp.MustCompile(`(\d{4})` + sep + `(\d{2})` + sep + `(\d{2})` + sep + `WA\d+`)
	reDayOnly  = regexp.MustCompile(`(\d{4})` + sep + `(\d{2})` + sep + `(\d{2})`)
	reDayLast  = regexp.MustCompile(`(\d{2})` + sep + `(\d{2})` + sep + `(\d{4})`)
)

// DeduceFromFilename recovers a capture time from the basename when metadata is
// absent. WhatsApp defaults to 10:00:00, date-only to 12:00:00.
func DeduceFromFilename(base string) (string, bool) {
	if m := reDateTime.FindStringSubmatch(base); m != nil {
		return build(m[1], m[2], m[3], m[4], m[5], m[6])
	}
	if m := reWhatsApp.FindStringSubmatch(base); m != nil {
		return build(m[1], m[2], m[3], "10", "00", "00")
	}
	if m := reDayOnly.FindStringSubmatch(base); m != nil {
		return build(m[1], m[2], m[3], "12", "00", "00")
	}
	if m := reDayLast.FindStringSubmatch(base); m != nil {
		return dayFirstOrMonthFirst(m[1], m[2], m[3])
	}
	return "", false
}

var (
	reWACounter = regexp.MustCompile(`(?i)WA\d+`)
	reCounter   = regexp.MustCompile(`\d+`)
)

// FilenameCounter returns the counter a file name carries next to a date
// without a time of day ("WA0007" in IMG-20200101-WA0007.jpg, "0003" in
// 2020-01-01_0003.jpg), "" when the name has a time of day or no counter.
// Those names get a default time (10:00:00, 12:00:00), so the counter is
// what tells such files apart.
func FilenameCounter(base string) string {
	if reDateTime.MatchString(base) {
		return ""
	}
	if loc := reWhatsApp.FindStringIndex(base); loc != nil {
		return strings.ToUpper(reWACounter.FindString(base[loc[0]:loc[1]]))
	}
	if loc := reDayOnly.FindStringIndex(base); loc != nil {
		stem := strings.TrimSuffix(base[loc[1]:], filepath.Ext(base))
		return reCounter.FindString(stem)
	}
	return ""
}

// Date orders of an XX-XX-YYYY file name (config date-order).
const (
	DMY = "dmy"
	MDY = "mdy"
)

var dateOrder atomic.Value // string: DMY, MDY or ""

var photoDateFallback atomic.Bool

// SetPhotoDateFallback makes a JPEG without DateTimeOriginal take its EXIF
// CreateDate, else its ModifyDate, before the file name and the file time.
// Off by default: it changes the golden name of such photos, so an archive
// built without it would get them again under a new name.
func SetPhotoDateFallback(on bool) { photoDateFallback.Store(on) }

// SetDateOrder sets how ambiguous XX-XX-YYYY file-name dates are read: DMY,
// MDY, or "" (not used).
func SetDateOrder(o string) { dateOrder.Store(o) }

// dayFirstOrMonthFirst reads a XX-XX-YYYY date: when only one of day-month and
// month-day is a valid date (or both give the same one) that is the date;
// when both are valid and differ, the configured date order decides, and
// without one the name gives no date.
func dayFirstOrMonthFirst(a, b, y string) (string, bool) {
	dmy, okD := build(y, b, a, "12", "00", "00")
	mdy, okM := build(y, a, b, "12", "00", "00")
	switch {
	case okD && okM && dmy != mdy:
		switch o, _ := dateOrder.Load().(string); o {
		case DMY:
			return dmy, true
		case MDY:
			return mdy, true
		}
		return "", false
	case okD:
		return dmy, true
	case okM:
		return mdy, true
	}
	return "", false
}

func build(ys, mos, ds, hs, mis, ss string) (string, bool) {
	y, _ := strconv.Atoi(ys)
	mo, _ := strconv.Atoi(mos)
	d, _ := strconv.Atoi(ds)
	h, _ := strconv.Atoi(hs)
	mi, _ := strconv.Atoi(mis)
	s, _ := strconv.Atoi(ss)
	if !yearOK(y) || mo < 1 || mo > 12 || d < 1 || d > 31 || h > 24 || mi > 59 || s > 59 {
		return "", false
	}
	t := time.Date(y, time.Month(mo), d, h, mi, s, 0, Zone())
	// reject rollovers (e.g. day 31 in a 30-day month)
	if t.Day() != d || int(t.Month()) != mo {
		return "", false
	}
	return t.Format(canonFmt), true
}

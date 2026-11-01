// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Command bakeoff is a throwaway validation harness (NOT part of the tool). It
// answers, on REAL files, two questions before we commit to pure-Go extraction:
//
//  1. SPEED: is imagemeta (photos) / go-mp4 (video) faster than exiftool/ffprobe?
//  2. FIDELITY: do they agree with exiftool/ffprobe on the fields the vendor
//     cascade and the canonical timestamp depend on?
//
// PRIVACY: it reads ONLY metadata (never pixels) and prints ONLY aggregates —
// agreement rates, coverage gaps, difference categories, timings. No raw EXIF
// values (no GPS, no personal timestamps) are ever emitted.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/abema/go-mp4"
	"github.com/evanoberholster/imagemeta"

	"github.com/mauriziofonte/onefold/internal/kinds"
)

func main() {
	photos := flag.String("photos", "", "directory of photos (imagemeta vs exiftool)")
	videos := flag.String("videos", "", "directory of videos (go-mp4 vs ffprobe)")
	n := flag.Int("n", 300, "max sample files per side")
	flag.Parse()

	if *photos != "" {
		runPhotos(collect(*photos, *n, photoExt), *n)
	}
	if *videos != "" {
		runVideos(collect(*videos, *n, videoExt), *n)
	}
	if *photos == "" && *videos == "" {
		fmt.Fprintln(os.Stderr, "give -photos and/or -videos")
		os.Exit(2)
	}
}

// Container formats (type registry) each bake-off parses.
var photoExt = map[string]bool{"jpeg": true, "heif": true, "png": true, "tiff": true}
var videoExt = map[string]bool{"isobmff": true}

func collect(root string, n int, formats map[string]bool) []string {
	var out []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if formats[kinds.Format(kinds.Ext(p))] {
			out = append(out, p)
		}
		if len(out) >= n {
			return filepath.SkipAll
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// ---------- PHOTOS ----------

type pFields struct {
	dtoSec string // "2006-01-02 15:04:05" or ""
	subsec bool
	make_  string
	model  string
	w, h   int
}

func runPhotos(files []string, n int) {
	if len(files) == 0 {
		fmt.Println("[photos] no files")
		return
	}
	fmt.Printf("\n===== PHOTOS: %d files (imagemeta vs exiftool) =====\n", len(files))

	// imagemeta (in-process)
	goRes := make([]pFields, len(files))
	t0 := time.Now()
	var goErr int
	for i, f := range files {
		v, err := imagemetaExtract(f)
		if err != nil {
			goErr++
		}
		goRes[i] = v
	}
	goDur := time.Since(t0)

	// exiftool per-file (the naive way both old tools used)
	exRes := make([]pFields, len(files))
	t1 := time.Now()
	for i, f := range files {
		exRes[i] = exiftoolOne(f)
	}
	exPerFile := time.Since(t1)

	// exiftool one-shot over ALL files (the batch way)
	t2 := time.Now()
	exiftoolBatch(files)
	exBatch := time.Since(t2)

	// compare
	var dtoMatch, dtoTZ, dtoOther, dtoBothEmpty, dtoGoMissing int
	var makeMatch, makeGoMissing, modelMatch, modelGoMissing int
	var subGo, subEx int
	var dimMatch, dimGoMissing int
	for i := range files {
		g, e := goRes[i], exRes[i]
		switch {
		case g.dtoSec == e.dtoSec && g.dtoSec != "":
			dtoMatch++
		case g.dtoSec == "" && e.dtoSec == "":
			dtoBothEmpty++
		case g.dtoSec == "" && e.dtoSec != "":
			dtoGoMissing++
		case sameClock(g.dtoSec, e.dtoSec):
			dtoTZ++ // same wall-clock, differ only by hour offset => timezone handling
		default:
			dtoOther++
		}
		if g.subsec {
			subGo++
		}
		if e.subsec {
			subEx++
		}
		if eqCI(g.make_, e.make_) && e.make_ != "" {
			makeMatch++
		} else if g.make_ == "" && e.make_ != "" {
			makeGoMissing++
		}
		if eqCI(g.model, e.model) && e.model != "" {
			modelMatch++
		} else if g.model == "" && e.model != "" {
			modelGoMissing++
		}
		if g.w == e.w && g.h == e.h && e.w != 0 {
			dimMatch++
		} else if (g.w == 0 || g.h == 0) && e.w != 0 {
			dimGoMissing++
		}
	}

	fmt.Printf("SPEED   imagemeta(in-proc): %v  (%.1f files/s)\n", goDur.Round(time.Millisecond), fps(len(files), goDur))
	fmt.Printf("        exiftool per-file:  %v  (%.1f files/s)  -> imagemeta %.1fx faster\n", exPerFile.Round(time.Millisecond), fps(len(files), exPerFile), ratio(exPerFile, goDur))
	fmt.Printf("        exiftool batch(1x): %v  (%.1f files/s)  -> imagemeta %.1fx faster\n", exBatch.Round(time.Millisecond), fps(len(files), exBatch), ratio(exBatch, goDur))
	fmt.Printf("FIDELITY vs exiftool (of %d):\n", len(files))
	fmt.Printf("  DateTimeOriginal: match %d | tz-only diff %d | other diff %d | both-empty %d | GO MISSING %d\n", dtoMatch, dtoTZ, dtoOther, dtoBothEmpty, dtoGoMissing)
	fmt.Printf("  sub-second present: imagemeta %d, exiftool %d\n", subGo, subEx)
	fmt.Printf("  Make:  match %d | GO MISSING %d\n", makeMatch, makeGoMissing)
	fmt.Printf("  Model: match %d | GO MISSING %d\n", modelMatch, modelGoMissing)
	fmt.Printf("  Dims:  match %d | GO MISSING %d (imagemeta reads EXIF dims, often absent in plain JPEG)\n", dimMatch, dimGoMissing)
	if goErr > 0 {
		fmt.Printf("  imagemeta decode errors: %d\n", goErr)
	}
}

func imagemetaExtract(path string) (pFields, error) {
	f, err := os.Open(path)
	if err != nil {
		return pFields{}, err
	}
	defer f.Close()
	e, err := imagemeta.Decode(f)
	if err != nil {
		return pFields{}, err
	}
	var v pFields
	if dto := e.DateTimeOriginal(); !dto.IsZero() {
		v.dtoSec = dto.Format("2006-01-02 15:04:05")
		v.subsec = dto.Nanosecond() != 0
	}
	v.make_ = strings.TrimSpace(e.Make)
	v.model = strings.TrimSpace(e.Model)
	v.w, v.h = int(e.ImageWidth), int(e.ImageHeight)
	return v, nil
}

func exiftoolOne(path string) pFields {
	out, err := exec.Command("exiftool", "-m", "-q", "-fast2", "-j",
		"-d", "%Y-%m-%d %H:%M:%S",
		"-DateTimeOriginal", "-SubSecTimeOriginal", "-Make", "-Model",
		"-ImageWidth", "-ImageHeight", path).Output()
	if err != nil {
		return pFields{}
	}
	return parseExifJSON(out)
}

func exiftoolBatch(paths []string) {
	args := append([]string{"-m", "-q", "-fast2", "-j", "-d", "%Y-%m-%d %H:%M:%S",
		"-DateTimeOriginal", "-Make", "-Model"}, paths...)
	_ = exec.Command("exiftool", args...).Run()
}

func parseExifJSON(out []byte) pFields {
	var arr []map[string]any
	if json.Unmarshal(out, &arr) != nil || len(arr) == 0 {
		return pFields{}
	}
	m := arr[0]
	var v pFields
	if s, ok := m["DateTimeOriginal"].(string); ok {
		v.dtoSec = s
	}
	if _, ok := m["SubSecTimeOriginal"]; ok {
		v.subsec = true
	}
	v.make_ = strings.TrimSpace(str(m["Make"]))
	v.model = strings.TrimSpace(str(m["Model"]))
	v.w, v.h = toInt(m["ImageWidth"]), toInt(m["ImageHeight"])
	return v
}

// ---------- VIDEOS ----------

func runVideos(files []string, n int) {
	if len(files) == 0 {
		fmt.Println("[videos] no files")
		return
	}
	fmt.Printf("\n===== VIDEOS: %d files (go-mp4 vs ffprobe) =====\n", len(files))

	type vGo struct {
		ct       string // raw creation_time (UTC second), pre duration-subtraction
		dur      float64
		brands   []string
		handler  string
		androidR bool // "com.android" reachable in moov bytes
		ok       bool
	}
	goRes := make([]vGo, len(files))
	t0 := time.Now()
	var goErr int
	for i, f := range files {
		g := vGo{}
		if ct, dur, br, hn, ar, err := mp4Extract(f); err == nil {
			g = vGo{ct, dur, br, hn, ar, true}
		} else {
			goErr++
		}
		goRes[i] = g
	}
	goDur := time.Since(t0)

	// ffprobe per-file (only mode it has)
	type vRef struct {
		ct         string
		dur        float64
		brands     []string
		handler    string
		hasAndroid bool
	}
	refRes := make([]vRef, len(files))
	t1 := time.Now()
	for i, f := range files {
		ct, dur, br, hn, andr := ffprobeOne(f)
		refRes[i] = vRef{ct, dur, br, hn, andr}
	}
	refDur := time.Since(t1)

	var ctMatch, ctDiff, ctBothEmpty, ctGoMissing int
	var durMatch, durDiff int
	var brandOK, brandGap int
	var hnMatch, hnGoMissing int
	var androidInSample, androidReachable int
	for i := range files {
		g, r := goRes[i], refRes[i]
		switch {
		case g.ct != "" && g.ct == r.ct:
			ctMatch++
		case g.ct == "" && r.ct == "":
			ctBothEmpty++
		case g.ct == "" && r.ct != "":
			ctGoMissing++
		default:
			ctDiff++
		}
		if r.dur > 0 {
			if abs(g.dur-r.dur) <= 1.0 {
				durMatch++
			} else {
				durDiff++
			}
		}
		if subset(r.brands, g.brands) && len(r.brands) > 0 {
			brandOK++
		} else if len(r.brands) > 0 {
			brandGap++
		}
		if eqCI(g.handler, r.handler) && r.handler != "" {
			hnMatch++
		} else if g.handler == "" && r.handler != "" {
			hnGoMissing++
		}
		if r.hasAndroid {
			androidInSample++
			if g.androidR {
				androidReachable++
			}
		}
	}

	fmt.Printf("SPEED   go-mp4(in-proc): %v  (%.1f files/s)\n", goDur.Round(time.Millisecond), fps(len(files), goDur))
	fmt.Printf("        ffprobe per-file: %v  (%.1f files/s)  -> go-mp4 %.1fx faster\n", refDur.Round(time.Millisecond), fps(len(files), refDur), ratio(refDur, goDur))
	fmt.Printf("FIDELITY vs ffprobe (of %d):\n", len(files))
	fmt.Printf("  creation_time (raw): match %d | diff %d | both-empty %d | GO MISSING %d\n", ctMatch, ctDiff, ctBothEmpty, ctGoMissing)
	fmt.Printf("  duration (±1s):      match %d | diff %d\n", durMatch, durDiff)
	fmt.Printf("  compatible_brands:   go covers ffprobe %d | gap %d\n", brandOK, brandGap)
	fmt.Printf("  handler_name:        match %d | GO MISSING %d\n", hnMatch, hnGoMissing)
	fmt.Printf("  android tags: in sample (ffprobe) %d | reachable in moov by go-mp4 %d\n", androidInSample, androidReachable)
	if goErr > 0 {
		fmt.Printf("  go-mp4 parse errors: %d\n", goErr)
	}
}

func mp4Extract(path string) (ct string, dur float64, brands []string, handler string, androidReach bool, err error) {
	f, e := os.Open(path)
	if e != nil {
		return "", 0, nil, "", false, e
	}
	defer f.Close()

	info, e := mp4.Probe(f)
	if e != nil {
		return "", 0, nil, "", false, e
	}
	if info.Timescale > 0 {
		dur = float64(info.Duration) / float64(info.Timescale)
	}
	for _, b := range info.CompatibleBrands {
		brands = append(brands, strings.TrimRight(string(b[:]), "\x00 "))
	}

	// mvhd -> raw creation_time (QuickTime epoch 1904 -> UTC)
	if boxes, _ := mp4.ExtractBoxWithPayload(f, nil, mp4.BoxPath{mp4.BoxTypeMoov(), mp4.BoxTypeMvhd()}); len(boxes) > 0 {
		if mv, ok := boxes[0].Payload.(*mp4.Mvhd); ok {
			if secs := mv.GetCreationTime(); secs > 2082844800 {
				ct = time.Unix(int64(secs)-2082844800, 0).UTC().Format("2006-01-02 15:04:05")
			}
		}
	}
	// video-track hdlr -> handler_name
	if boxes, _ := mp4.ExtractBoxWithPayload(f, nil, mp4.BoxPath{mp4.BoxTypeMoov(), mp4.BoxTypeTrak(), mp4.BoxTypeMdia(), mp4.BoxTypeHdlr()}); len(boxes) > 0 {
		for _, b := range boxes {
			if h, ok := b.Payload.(*mp4.Hdlr); ok {
				if strings.TrimRight(string(h.HandlerType[:]), "\x00 ") == "vide" || handler == "" {
					handler = strings.TrimRight(h.Name, "\x00")
				}
			}
		}
	}
	// android reachability: scan the raw moov box bytes for "com.android"
	if bi, _ := mp4.ExtractBox(f, nil, mp4.BoxPath{mp4.BoxTypeMoov()}); len(bi) > 0 {
		size := bi[0].Size
		if size > 4<<20 {
			size = 4 << 20 // cap the scan at 4 MiB of moov
		}
		buf := make([]byte, size)
		if _, e := f.ReadAt(buf, int64(bi[0].Offset)); e == nil || e.Error() == "EOF" {
			androidReach = strings.Contains(string(buf), "com.android")
		}
	}
	return ct, dur, brands, handler, androidReach, nil
}

func ffprobeOne(path string) (ct string, dur float64, brands []string, handler string, hasAndroid bool) {
	out, err := exec.Command("ffprobe", "-v", "quiet", "-print_format", "json",
		"-show_format", "-show_streams", path).Output()
	if err != nil {
		return "", 0, nil, "", false
	}
	var p struct {
		Format struct {
			Duration string            `json:"duration"`
			Tags     map[string]string `json:"tags"`
		} `json:"format"`
		Streams []struct {
			CodecType string            `json:"codec_type"`
			Tags      map[string]string `json:"tags"`
		} `json:"streams"`
	}
	if json.Unmarshal(out, &p) != nil {
		return "", 0, nil, "", false
	}
	if cti, ok := p.Format.Tags["creation_time"]; ok {
		if t, e := time.Parse(time.RFC3339, cti); e == nil {
			ct = t.UTC().Format("2006-01-02 15:04:05")
		}
	}
	fmt.Sscanf(p.Format.Duration, "%f", &dur)
	if cb, ok := p.Format.Tags["compatible_brands"]; ok {
		for i := 0; i+4 <= len(cb); i += 4 {
			brands = append(brands, strings.TrimRight(cb[i:i+4], "\x00 "))
		}
	}
	for k := range p.Format.Tags {
		if strings.HasPrefix(k, "com.android") {
			hasAndroid = true
		}
	}
	for _, s := range p.Streams {
		if s.CodecType == "video" {
			if hn, ok := s.Tags["handler_name"]; ok {
				handler = strings.TrimSpace(hn)
			}
			for k := range s.Tags {
				if strings.HasPrefix(k, "com.android") {
					hasAndroid = true
				}
			}
		}
	}
	return ct, dur, brands, handler, hasAndroid
}

// ---------- helpers ----------

func fps(n int, d time.Duration) float64 {
	if d <= 0 {
		return 0
	}
	return float64(n) / d.Seconds()
}
func ratio(a, b time.Duration) float64 {
	if b <= 0 {
		return 0
	}
	return float64(a) / float64(b)
}
func eqCI(a, b string) bool { return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b)) }
func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
func subset(want, have []string) bool {
	h := map[string]bool{}
	for _, x := range have {
		h[strings.ToLower(x)] = true
	}
	for _, w := range want {
		if !h[strings.ToLower(w)] {
			return false
		}
	}
	return true
}

// sameClock reports whether two "Y-m-d H:M:S" strings share the same date and
// minute:second but differ in hour (i.e. a pure timezone offset), a common EXIF
// vs container discrepancy we want to categorise rather than call a mismatch.
func sameClock(a, b string) bool {
	if len(a) != 19 || len(b) != 19 {
		return false
	}
	return a[:10] == b[:10] && a[13:] == b[13:] && a[11:13] != b[11:13]
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func toInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case string:
		var i int
		fmt.Sscanf(x, "%d", &i)
		return i
	}
	return 0
}

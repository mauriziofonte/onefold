// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package similar lists golden photos that look alike (a perceptual
// difference hash within a distance) for a person to review. It never drops
// anything and never changes a plan: perceptual hashes give too many false
// positives to decide on their own (a burst, a scan of a print, two shots of
// the same scene).
package similar

import (
	"fmt"
	"image"
	"math/bits"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/zeebo/xxh3"

	"github.com/mauriziofonte/onefold/internal/kinds"
	"github.com/mauriziofonte/onefold/internal/plan"
	"github.com/mauriziofonte/onefold/internal/runctl"
	"github.com/mauriziofonte/onefold/internal/textexport"
	"github.com/mauriziofonte/onefold/internal/tools"
)

// DHash is the 64-bit difference hash of an image: the image is reduced to
// 9x8 gray cells and each bit says whether a cell is brighter than its right
// neighbour. Resizing, recompression and small edits keep most bits.
func DHash(img image.Image) uint64 {
	h, _ := dhash(img)
	return h
}

// dhash also reports a flat image (no cell differs visibly from its
// neighbour): its hash is all zeros whatever it shows, so it pairs with
// every other flat image and is left out of the pairs.
func dhash(img image.Image) (uint64, bool) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return 0, true
	}
	var cells [8][9]float64
	var counts [8][9]float64
	// average every pixel into its cell (box filter): sample at most ~256
	// pixels per cell so a 12 MP photo costs the same as a thumbnail
	stepX, stepY := max(1, w/(9*16)), max(1, h/(8*16))
	for y := b.Min.Y; y < b.Max.Y; y += stepY {
		cy := (y - b.Min.Y) * 8 / h
		for x := b.Min.X; x < b.Max.X; x += stepX {
			cx := (x - b.Min.X) * 9 / w
			r, g, bl, _ := img.At(x, y).RGBA()
			cells[cy][cx] += 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(bl)
			counts[cy][cx]++
		}
	}
	var hsh uint64
	flat := true
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			l, r := cells[y][x]/max(1, counts[y][x]), cells[y][x+1]/max(1, counts[y][x+1])
			hsh <<= 1
			if l > r {
				hsh |= 1
			}
			if d := l - r; d > flatDelta || d < -flatDelta {
				flat = false
			}
		}
	}
	return hsh, flat
}

// flatDelta: about 1% of the 16-bit gray range.
const flatDelta = 655

// Distance is the number of differing bits.
func Distance(a, b uint64) int { return bits.OnesCount64(a ^ b) }

// Photo is one hashed golden photo.
type Photo struct {
	CanonRel string `json:"canonRel"`
	SrcHost  string `json:"srcHost"`
	SrcPath  string `json:"srcPath"`
	Hash     uint64 `json:"dhash"`
}

// Pair is two golden photos that look alike.
type Pair struct {
	A        Photo `json:"a"`
	B        Photo `json:"b"`
	Distance int   `json:"distance"`
}

// Result of a Find.
type Result struct {
	Hashed int `json:"hashed"`
	// Of the photos read: camera RAW read through the JPEG preview they
	// embed, and HEIF/HEIC/AVIF read through ffmpeg.
	Previews int      `json:"rawPreviews"`
	FFmpeg   int      `json:"viaFfmpeg"`
	Skipped  int      `json:"skipped"`  // photos with no decoder here (HEIC/AVIF without ffmpeg, other formats)
	NoFFmpeg int      `json:"noFfmpeg"` // of the skipped: HEIC/AVIF while ffmpeg is missing or ONEFOLD_NO_EXTERNAL=1
	Flat     int      `json:"flat"`     // plain images (one colour, no detail): not compared
	Failed   []string `json:"failed,omitempty"`
	Pairs    []Pair   `json:"pairs"`
}

// Options of Find.
type Options struct {
	MaxDistance int    // pairs at most this many bits apart (0..16)
	OnlyHost    string // hash only the actions of this host ("" = all)
	Workers     int    // 0 = NumCPU
	Gate        *runctl.Gate
}

// Find hashes the photo actions of a plan (reading their sources) and pairs
// the ones within MaxDistance. Read-only.
func Find(acts []plan.Action, o Options) (*Result, error) {
	if o.MaxDistance < 0 || o.MaxDistance > 16 {
		return nil, fmt.Errorf("max distance %d: use 0..16", o.MaxDistance)
	}
	if o.Workers <= 0 {
		o.Workers = runtime.NumCPU()
	}
	res := &Result{}
	type job struct {
		a   plan.Action
		how string
	}
	var todo []job
	for _, a := range acts {
		if a.Kind != "photo" || (o.OnlyHost != "" && a.SrcHost != o.OnlyHost) {
			continue
		}
		how := decoderOf(kinds.Ext(a.SrcPath))
		if how == "" || (how == viaFFmpeg && !tools.Has("ffmpeg")) {
			res.Skipped++
			if how != "" {
				res.NoFFmpeg++
			}
			continue
		}
		todo = append(todo, job{a, how})
	}
	photos := make([]Photo, 0, len(todo))
	var mu sync.Mutex
	var wg sync.WaitGroup
	jobs := make(chan job)
	for w := 0; w < o.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				if !o.Gate.Wait() {
					continue
				}
				a := j.a
				h, flat, err := hashFile(a.SrcPath, j.how)
				mu.Lock()
				if err == nil {
					switch j.how {
					case viaRAW:
						res.Previews++
					case viaFFmpeg:
						res.FFmpeg++
					}
				}
				switch {
				case err != nil:
					res.Failed = append(res.Failed, a.SrcPath+": "+err.Error())
				case flat:
					res.Flat++
				default:
					photos = append(photos, Photo{CanonRel: a.CanonRel, SrcHost: a.SrcHost, SrcPath: a.SrcPath, Hash: h})
				}
				mu.Unlock()
			}
		}()
	}
	for _, j := range todo {
		jobs <- j
	}
	close(jobs)
	wg.Wait()
	if o.Gate.Stopped() {
		return nil, runctl.ErrStopped
	}
	sort.Slice(photos, func(i, j int) bool { return photos[i].CanonRel < photos[j].CanonRel })
	sort.Strings(res.Failed)
	res.Hashed = len(photos)
	res.Pairs = pairs(photos, o.MaxDistance)
	return res, nil
}

func hashFile(p, how string) (uint64, bool, error) {
	img, err := decode(p, how)
	if err != nil {
		return 0, false, err
	}
	h, flat := dhash(img)
	return h, flat, nil
}

// pairs finds every pair within d bits. Pigeonhole: two hashes within d
// bits agree on at least one of d+1 disjoint bit slices, so only photos
// sharing a slice value are compared.
func pairs(ps []Photo, d int) []Pair {
	n := d + 1
	seen := map[[2]int]bool{}
	var out []Pair
	for s := 0; s < n; s++ {
		lo, hi := s*64/n, (s+1)*64/n
		mask := uint64(1)<<(hi-lo) - 1
		if hi-lo == 64 {
			mask = ^uint64(0)
		}
		buckets := map[uint64][]int{}
		for i, p := range ps {
			k := (p.Hash >> lo) & mask
			buckets[k] = append(buckets[k], i)
		}
		for _, idx := range buckets {
			for x := 0; x < len(idx); x++ {
				for y := x + 1; y < len(idx); y++ {
					i, j := idx[x], idx[y]
					if seen[[2]int{i, j}] {
						continue
					}
					seen[[2]int{i, j}] = true
					if dist := Distance(ps[i].Hash, ps[j].Hash); dist <= d {
						out = append(out, Pair{A: ps[i], B: ps[j], Distance: dist})
					}
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Distance != out[j].Distance {
			return out[i].Distance < out[j].Distance
		}
		if out[i].A.CanonRel != out[j].A.CanonRel {
			return out[i].A.CanonRel < out[j].A.CanonRel
		}
		return out[i].B.CanonRel < out[j].B.CanonRel
	})
	return out
}

// DocGroup is golden documents whose extracted text is the same (after
// normalization): the same document saved again, exported twice, re-printed
// to PDF. Listed for review, never dropped: the files differ.
type DocGroup struct {
	Chars   int      `json:"chars"` // normalized text length
	Members []Photo  `json:"members"`
	Formats []string `json:"formats"`
}

// MinDocChars: shorter texts (a title page, a blank form) are not grouped.
const MinDocChars = 200

// FindDocs extracts the text of the document actions (textexport.Extract:
// pure Go for office formats, pdftotext for PDF) and groups the ones whose
// normalized text is equal. Read-only.
func FindDocs(acts []plan.Action, o Options) ([]DocGroup, int, error) {
	if o.Workers <= 0 {
		o.Workers = runtime.NumCPU()
	}
	type res struct {
		a    plan.Action
		key  string
		size int
	}
	var todo []plan.Action
	for _, a := range acts {
		if a.Kind == kinds.Document && (o.OnlyHost == "" || a.SrcHost == o.OnlyHost) {
			todo = append(todo, a)
		}
	}
	var mu sync.Mutex
	var out []res
	var wg sync.WaitGroup
	jobs := make(chan plan.Action)
	for w := 0; w < o.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for a := range jobs {
				if !o.Gate.Wait() {
					continue
				}
				norm := normalizeText(textexport.Extract(a.SrcPath))
				if len(norm) < MinDocChars {
					continue
				}
				h := xxh3.HashString128(norm).Bytes()
				mu.Lock()
				out = append(out, res{a, string(h[:]), len(norm)})
				mu.Unlock()
			}
		}()
	}
	for _, a := range todo {
		jobs <- a
	}
	close(jobs)
	wg.Wait()
	if o.Gate.Stopped() {
		return nil, 0, runctl.ErrStopped
	}
	byKey := map[string][]res{}
	for _, r := range out {
		byKey[r.key] = append(byKey[r.key], r)
	}
	var groups []DocGroup
	for _, rs := range byKey {
		if len(rs) < 2 {
			continue
		}
		g := DocGroup{Chars: rs[0].size}
		seen := map[string]bool{}
		for _, r := range rs {
			g.Members = append(g.Members, Photo{CanonRel: r.a.CanonRel, SrcHost: r.a.SrcHost, SrcPath: r.a.SrcPath})
			if f := kinds.Ext(r.a.SrcPath); !seen[f] {
				seen[f] = true
				g.Formats = append(g.Formats, f)
			}
		}
		sort.Slice(g.Members, func(i, j int) bool { return g.Members[i].CanonRel < g.Members[j].CanonRel })
		sort.Strings(g.Formats)
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].Members[0].CanonRel < groups[j].Members[0].CanonRel })
	return groups, len(todo), nil
}

// normalizeText: lower case, every run of non-letters and non-digits one
// space, so line breaks, hyphenation spacing and punctuation of an export do
// not hide the same text.
func normalizeText(s string) string {
	var b strings.Builder
	space := true
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			space = false
		} else if !space {
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

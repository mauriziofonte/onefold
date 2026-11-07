// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package similar

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // decoders for image.Decode
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"golang.org/x/image/tiff"
	_ "golang.org/x/image/webp" // decoder for image.Decode

	"github.com/mauriziofonte/onefold/internal/kinds"
	"github.com/mauriziofonte/onefold/internal/meta"
	"github.com/mauriziofonte/onefold/internal/tools"
)

// How the pixels of a photo are read.
const (
	viaImage  = "image"  // pure Go: JPEG, PNG, GIF, TIFF, WebP
	viaRAW    = "raw"    // the JPEG preview a camera RAW embeds (pure Go)
	viaFFmpeg = "ffmpeg" // HEIF, HEIC, AVIF: ffmpeg, when it is installed
)

// decoderOf returns how a photo with extension ext is read ("" = it is not).
func decoderOf(ext string) string {
	if kinds.IsRAW(ext) {
		return viaRAW
	}
	switch kinds.Format(ext) {
	case "jpeg", "png", "gif", "tiff", "webp":
		return viaImage
	case "heif":
		return viaFFmpeg
	}
	return ""
}

// decode reads the photo at p the way how says.
func decode(p, how string) (image.Image, error) {
	switch how {
	case viaRAW:
		return rawPreview(p, kinds.Format(kinds.Ext(p)) == "tiff")
	case viaFFmpeg:
		return ffmpegFrame(p)
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(bufio.NewReader(f))
	return img, err
}

// PreviewMin is the long side from which a JPEG embedded in a RAW is taken
// as its preview without looking further. A difference hash needs 9x8 cells;
// the smaller JPEGs are thumbnails, often with black bars.
const PreviewMin = 1024

// headMax bounds the bytes read to size one embedded JPEG.
const headMax = 1 << 20

var errNoPreview = errors.New("no embedded JPEG preview")

// rawPreview decodes the JPEG preview a camera RAW embeds: the first one
// whose long side is at least PreviewMin, else the largest one that decodes.
// The file is scanned for JPEG start markers in chunks, and only a candidate
// is read further, so a RAW whose preview comes first costs a few megabytes.
// tiffFallback: a TIFF-based RAW without a JPEG preview (some DNG) is read
// as a TIFF (its first image) instead.
func rawPreview(p string, tiffFallback bool) (image.Image, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := fi.Size()
	type cand struct {
		off int64
		px  int
	}
	var small []cand
	full := func(off int64) (image.Image, error) {
		return jpeg.Decode(bufio.NewReader(io.NewSectionReader(f, off, size-off)))
	}
	buf := make([]byte, 1<<20)
	for base := int64(0); base < size; {
		n, rerr := f.ReadAt(buf, base)
		if n < 3 {
			break
		}
		for i := 0; i+2 < n; i++ {
			if buf[i] != 0xFF || buf[i+1] != 0xD8 || buf[i+2] != 0xFF {
				continue
			}
			off := base + int64(i)
			cfg, err := jpeg.DecodeConfig(bufio.NewReader(io.NewSectionReader(f, off, min(headMax, size-off))))
			if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
				continue
			}
			if max(cfg.Width, cfg.Height) >= PreviewMin {
				if img, err := full(off); err == nil {
					return img, nil
				}
				continue
			}
			small = append(small, cand{off, cfg.Width * cfg.Height})
		}
		if rerr != nil {
			break
		}
		base += int64(n - 2)
	}
	sort.SliceStable(small, func(i, j int) bool { return small[i].px > small[j].px })
	for _, c := range small {
		if img, err := full(c.off); err == nil {
			return img, nil
		}
	}
	if tiffFallback {
		if img, err := tiff.Decode(f); err == nil {
			return img, nil
		}
	}
	return nil, errNoPreview
}

// ffmpegTimeout bounds one ffmpeg decode.
const ffmpegTimeout = 2 * time.Minute

var errNoFFmpeg = errors.New("ffmpeg is not installed")

// ffmpegFrame decodes a HEIF, HEIC or AVIF photo with ffmpeg, at one eighth
// of its size (a PNG on a pipe). The size ffmpeg read is checked against
// the size the file declares: an ffmpeg older than 7.1 reads one tile of
// the tiled HEIC phones write, and a tile is not the photo.
func ffmpegFrame(p string) (image.Image, error) {
	cmd := tools.Command("ffmpeg", "-nostdin", "-hide_banner", "-loglevel", "error", "-i", p,
		"-frames:v", "1", "-vf", "scale=w='max(2,trunc(iw/16)*2)':h='max(2,trunc(ih/16)*2)'",
		"-f", "image2pipe", "-c:v", "png", "-")
	if cmd == nil {
		return nil, errNoFFmpeg
	}
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	t := time.AfterFunc(ffmpegTimeout, func() { _ = cmd.Process.Kill() })
	err := cmd.Wait()
	t.Stop()
	if err != nil {
		msg, _, _ := strings.Cut(strings.TrimSpace(stderr.String()), "\n")
		return nil, fmt.Errorf("ffmpeg: %v %s", err, msg)
	}
	img, err := png.Decode(&out)
	if err != nil {
		return nil, fmt.Errorf("ffmpeg output: %w", err)
	}
	if w, h := meta.HEIFSize(p); w > 0 && h > 0 {
		b := img.Bounds()
		gw, gh := b.Dx()*8, b.Dy()*8
		if !(near(gw, w) && near(gh, h)) && !(near(gw, h) && near(gh, w)) {
			return nil, fmt.Errorf("ffmpeg read %dx%d of a %dx%d photo (one tile: FFmpeg 7.1 or later reads tiled HEIC)", gw, gh, w, h)
		}
	}
	return img, nil
}

// near: a size read at one eighth (rounded down to an even number) is
// within 16 pixels of the size declared.
func near(got, want int) bool { return got <= want && want-got < 16 }

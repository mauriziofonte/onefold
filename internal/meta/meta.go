// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package meta extracts the canonical metadata onefold needs per asset:
// photos via imagemeta (pure-Go), videos via ffprobe. All READ-ONLY.
package meta

import (
	"bufio"
	"encoding/json"
	"errors"
	"image"
	_ "image/jpeg" // register JPEG for DecodeConfig (SOF dimensions)
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mauriziofonte/onefold/internal/kinds"
	"github.com/mauriziofonte/onefold/internal/perf"
	"github.com/mauriziofonte/onefold/internal/tools"

	"github.com/evanoberholster/imagemeta/exif2"
	"github.com/evanoberholster/imagemeta/imagetype"
	"github.com/evanoberholster/imagemeta/jpeg"
	"github.com/evanoberholster/imagemeta/xmp"
)

// Meta is the extracted, normalized metadata for one asset. Fields after
// DurationSec are generic-kind metadata; all omitempty so media manifests keep
// their historical shape.
type Meta struct {
	Kind        string  `json:"kind"`        // photo | video | image | audio | document | text | archive | other
	Time        string  `json:"time"`        // canonical "YYYY-MM-DD HH:MM:SS" (wall clock of the canonical zone, meta.Zone) or ""
	TimeSource  string  `json:"timeSource"`  // "metadata" | "filename" | "takeout" | "filemtime" | "none"
	SubSec      bool    `json:"subSec"`      // photo captured a sub-second
	SubSecNs    int64   `json:"subSecNs"`    // sub-second VALUE (ns); disambiguates bursts in the photo cluster key
	Device      string  `json:"device"`      // RAW make/model (photo/image) or vendor slug (video)
	CanonDevice string  `json:"canonDevice"` // canonical device name (registry), persisted in silvering
	Category    string  `json:"category"`    // photo category (smartphone/camera/...)
	Prefix      string  `json:"prefix"`      // filename prefix (IMG/DSC/VID/...)
	Width       int     `json:"width"`       // 0 if unknown
	Height      int     `json:"height"`      // 0 if unknown
	DurationSec float64 `json:"durationSec"` // video only

	FileCategory string `json:"fileCategory,omitempty"` // type-registry category (raster_image, spreadsheet, ...)
	Title        string `json:"title,omitempty"`        // document title / audio title
	Author       string `json:"author,omitempty"`       // document author / audio artist
	Album        string `json:"album,omitempty"`        // audio album
	TagYear      string `json:"tagYear,omitempty"`      // audio release year tag
	Track        int    `json:"track,omitempty"`        // audio track number
	Disc         int    `json:"disc,omitempty"`         // audio disc number
	Pages        int    `json:"pages,omitempty"`        // document pages
	Entries      int    `json:"entries,omitempty"`      // archive: non-junk members
	ExtMismatch  bool   `json:"extMismatch,omitempty"`  // magic bytes disagree with the extension
	SniffExt     string `json:"sniffExt,omitempty"`     // extension implied by magic bytes (only if unambiguous)
	// Lat, Lon: where a photo or video was taken, in degrees (WGS84); both 0
	// = unknown. From its metadata, else from its Google Takeout file.
	Lat float64 `json:"lat,omitempty"`
	Lon float64 `json:"lon,omitempty"`
}

// Options tune extraction. Quick = generic kinds get filesystem time only (no
// archive listing, no document parsing, no external tools).
type Options struct {
	Quick bool
	Data  []byte // whole file content already read by the caller (nil = read from disk)
	// Takeout reads the Google Takeout metadata file next to a photo or
	// video (applyTakeout): its capture time below the file's own metadata
	// and name, its place, and "-edited" copies as edits.
	Takeout bool
	// Places keeps the place a photo or video's own metadata gives (EXIF
	// GPS, a video's ISO 6709 location) in Lat and Lon; without it they stay
	// empty (Takeout still fills them from the .json).
	Places bool
}

// InScope reports whether the extension belongs to the media scope (the
// "media" preset, the golden scope). Kept for callers that predate the kind registry.
func InScope(path string) bool {
	return mediaPreset[kinds.Ext(path)]
}

var mediaPreset, _, _ = kinds.PresetExts([]string{"media"})

// Extract is ExtractWith with default options.
func Extract(path string) (Meta, error) { return ExtractWith(path, Options{}) }

// ExtractWith dispatches by kind. READ-ONLY. Returns a best-effort Meta even on
// partial failures (Time may be ""). A FileModifyDate fallback is applied last,
// tracked as its own source ("filemtime") since mtime is the least trusted.
func ExtractWith(path string, o Options) (Meta, error) {
	if o.Data != nil {
		preloaded.Store(path, o.Data)
		defer preloaded.Delete(path)
	}
	ext := kinds.Ext(path)
	kind := kinds.KindOf(ext)
	var (
		m   Meta
		err error
	)
	switch {
	case kind == kinds.Photo && kinds.Format(ext) == "jpeg":
		m, err = extractPhoto(path)
	case kind == kinds.Photo:
		m = extractPhotoImage(path, ext) // HEIC/HEIF/AVIF, PNG, WebP, TIFF, camera RAW
	case kind == kinds.Video && kinds.Format(ext) == "riff-avi":
		m, err = extractAVI(path) // digicam AVIs embed EXIF that ffprobe ignores
	case kind == kinds.Video:
		m, err = extractVideo(path)
	default:
		m = extractGeneric(path, ext, kind, o)
	}
	m.FileCategory = kinds.Category(ext)
	if !o.Places {
		m.Lat, m.Lon = 0, 0
	}
	if o.Takeout && kinds.IsMedia(kind) {
		applyTakeout(path, &m)
	}
	tsn := perf.Start()
	var fam string
	if b, ok := preloadedHead(path, 16); ok {
		fam = kinds.SniffFamily(b)
	} else {
		fam = kinds.SniffFile(path)
	}
	perf.End("sniff", tsn, 0)
	if mm, fix := kinds.Mismatch(ext, fam); mm {
		m.ExtMismatch, m.SniffExt = true, fix
	}
	if m.Time == "" {
		if t, ok := fileMtimeCanonical(path); ok {
			m.Time, m.TimeSource = t, "filemtime"
		}
	}
	return m, err
}

// extractAVI reads embedded EXIF via exiftool (imagemeta/ffprobe can't). Old
// digicam AVIs (e.g. Fujifilm FinePix) carry DateTimeOriginal + Make/Model that
// ffprobe reports as empty: this recovers them.
func extractAVI(path string) (Meta, error) {
	m := Meta{Kind: "video", Prefix: "VID", TimeSource: "none"}
	// Raw date strings (no -d, which drops zones): same zone-aware parsing as
	// every other exiftool read.
	if t := exifJSON(path, "-DateTimeOriginal", "-CreateDate", "-Make", "-Model", "-ImageWidth", "-ImageHeight"); t != nil {
		exifTime(t, &m, "DateTimeOriginal", "CreateDate")
		m.Device = strings.TrimSpace(asStr(t["Make"]) + " " + asStr(t["Model"]))
		m.Width, m.Height = asInt(t["ImageWidth"]), asInt(t["ImageHeight"])
	}
	if m.Time == "" {
		if d, ok := DeduceFromFilename(filepath.Base(path)); ok {
			m.Time, m.TimeSource = d, "filename"
		}
	}
	m.Device = strings.TrimSpace(m.Device)
	return m, nil
}

func fileMtimeCanonical(path string) (string, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	t := fi.ModTime().In(Zone())
	if !yearOK(t.Year()) {
		return "", false
	}
	return t.Format(canonFmt), true
}

func asStr(v any) string {
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}
func asInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(x)
		return n
	}
	return 0
}

func extractPhoto(path string) (Meta, error) {
	m := Meta{Kind: "photo", Prefix: "IMG", TimeSource: "none"}
	f, _, err := openSrc(path)
	if err != nil {
		return m, err
	}
	defer f.Close()

	ex, xm, err := decodePhoto(f)
	if err != nil {
		// unreadable EXIF: fall back to filename
		if t, ok := DeduceFromFilename(filepath.Base(path)); ok {
			m.Time, m.TimeSource = t, "filename"
		}
		return m, nil
	}

	if dto := ex.DateTimeOriginal(); !dto.IsZero() {
		if t, ok := CanonicalPhoto(dto); ok {
			m.Time, m.TimeSource = t, "metadata"
			m.SubSecNs = int64(dto.Nanosecond())
			m.SubSec = m.SubSecNs != 0
		}
	}
	if m.Time == "" && photoDateFallback.Load() {
		if !setTime(&m, ex.CreateDate(), true) {
			setTime(&m, ex.ModifyDate(), true)
		}
	}
	if m.Time == "" {
		if t, ok := DeduceFromFilename(filepath.Base(path)); ok {
			m.Time, m.TimeSource = t, "filename"
		}
	}
	// Device signals: EXIF make/model/software fall back to XMP (many JPEGs carry
	// the device ONLY in XMP: tiff:Make/Model or CreatorTool). One reader pass.
	make_ := firstNonEmpty(strings.TrimSpace(ex.Make), strings.TrimSpace(xm.Tiff.Make))
	model := firstNonEmpty(strings.TrimSpace(ex.Model), strings.TrimSpace(xm.Tiff.Model))
	software := firstNonEmpty(ex.Software, ex.ProcessingSoftware)
	creator := strings.TrimSpace(xm.Basic.CreatorTool)
	m.CanonDevice, m.Category, m.Prefix = CanonicalDevice(make_, model, software, creator)
	m.Device = strings.TrimSpace(make_ + " " + model) // RAW, for reference
	setGeo(&m, ex.GPS.Latitude(), ex.GPS.Longitude())
	m.Width, m.Height = int(ex.ImageWidth), int(ex.ImageHeight)
	// EXIF often omits pixel dimensions (they live in the JPEG SOF). Read them
	// from the SOF via DecodeConfig (header only, no pixel decode) so the winner
	// selection is never distorted by a missing (0x0) resolution.
	if m.Width == 0 || m.Height == 0 {
		if _, err := f.Seek(0, 0); err == nil {
			if cfg, _, derr := image.DecodeConfig(f); derr == nil {
				m.Width, m.Height = cfg.Width, cfg.Height
			}
		}
	}
	return m, nil
}

// extractPhotoImage reads the non-JPEG photo formats through the image readers
// (imagemeta, exiftool fallback); always fully, like JPEG, whatever Quick says:
// a photo of the golden scope needs its capture time and device.
func extractPhotoImage(path, ext string) Meta {
	m := Meta{Kind: kinds.Photo, TimeSource: "none"}
	extractImage(path, ext, &m)
	if m.Prefix == "" {
		m.Prefix = "IMG"
	}
	if m.Time == "" {
		if t, ok := DeduceFromFilename(filepath.Base(path)); ok {
			m.Time, m.TimeSource = t, "filename"
		}
	}
	return m
}

var errNotJPEG = errors.New("not a JPEG stream (no SOI marker)")

// decodePhoto reads EXIF and XMP in ONE pass. imagemeta.Decode discards XMP
// (passes a nil xmp reader to ScanJPEG); here we wire xmp.ParseXmp so the device
// signals that live only in XMP (tiff:Make/Model, CreatorTool) are recovered —
// pure-Go, no exiftool. JPEG photos only; the other photo formats go through extractPhotoImage.
func decodePhoto(rs io.ReadSeeker) (exif2.Exif, xmp.XMP, error) {
	rr := bufio.NewReader(rs)
	ir := exif2.NewIfdReader(exif2.Logger)
	defer ir.Close()

	// Guard: imagemeta v0.3.1 jpeg.ScanJPEG spins forever when the stream does
	// not start with the SOI marker (e.g. a PNG named .jpg). Refuse early; the
	// caller falls back to filename/mtime.
	if head, err := rr.Peek(3); err != nil || head[0] != 0xff || head[1] != 0xd8 || head[2] != 0xff {
		return exif2.Exif{}, xmp.XMP{}, errNotJPEG
	}
	it, err := imagetype.ScanBuf(rr)
	if err != nil {
		return exif2.Exif{}, xmp.XMP{}, err
	}
	ir.Exif.ImageType = it

	var x xmp.XMP
	xmpFn := func(r io.Reader) error {
		x, _ = xmp.ParseXmp(r) // best-effort: malformed XMP just yields a zero value
		return nil
	}
	if err := jpeg.ScanJPEG(rr, ir.DecodeJPEGIfd, xmpFn); err != nil {
		return ir.Exif, x, err
	}
	return ir.Exif, x, nil
}

// ffprobe JSON we care about.
type ffprobe struct {
	Format struct {
		Duration string            `json:"duration"`
		Tags     map[string]string `json:"tags"`
	} `json:"format"`
	Streams []struct {
		CodecType string            `json:"codec_type"`
		Width     int               `json:"width"`
		Height    int               `json:"height"`
		CodedW    int               `json:"coded_width"`
		CodedH    int               `json:"coded_height"`
		Tags      map[string]string `json:"tags"`
	} `json:"streams"`
}

func extractVideo(path string) (Meta, error) {
	m := Meta{Kind: "video", Prefix: "VID", TimeSource: "none"}
	out, ok, err := tools.Output(nil, "ffprobe", "-v", "quiet", "-print_format", "json",
		"-show_format", "-show_streams", path)
	if !ok {
		return extractVideoNative(path)
	}
	if err != nil {
		if t, ok := DeduceFromFilename(filepath.Base(path)); ok {
			m.Time, m.TimeSource = t, "filename"
		}
		return m, nil
	}
	var p ffprobe
	if json.Unmarshal(out, &p) != nil {
		return m, nil
	}

	ft := p.Format.Tags
	dur, _ := strconv.ParseFloat(p.Format.Duration, 64)
	m.DurationSec = dur

	if t, ok := CanonicalVideo(ft["creation_time"], dur); ok {
		m.Time, m.TimeSource = t, "metadata"
	} else if t, ok := DeduceFromFilename(filepath.Base(path)); ok {
		m.Time, m.TimeSource = t, "filename"
	}

	// vendor signals
	sig := VSignals{
		AndroidMfr:   ft["com.android.manufacturer"],
		AndroidModel: ft["com.android.model"],
		AndroidVer:   ft["com.android.version"],
		CompatBrands: ft["compatible_brands"],
		Comment:      firstNonEmpty(ft["comment"], ft["comment-eng"]),
		EncoderFmt:   ft["encoder"],
		MajorBrand:   ft["major_brand"],
		Firmware:     ft["firmware"],
	}
	for _, s := range p.Streams {
		if s.CodecType == "video" {
			sig.StreamEncoder = s.Tags["encoder"]
			sig.HandlerName = s.Tags["handler_name"]
			sig.VendorID = s.Tags["vendor_id"]
			m.Width, m.Height = pick(s.Width, s.CodedW), pick(s.Height, s.CodedH)
			break
		}
	}
	m.Device = VendorSlug(sig)
	videoGeo(&m, ft)
	return m, nil
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}
func pick(a, b int) int {
	if a > 0 {
		return a
	}
	return b
}

// extractVideoNative is the ffprobe-less path (Windows): ISO-BMFF containers
// (mp4/mov/3gp/m4v) via the pure-Go box reader, Matroska/WebM via the EBML
// reader, FLV via its onMetaData; other containers (wmv, avi, mpg: ffprobe
// reports no creation time for them either) are dated by filename, then mtime
// by the caller.
func extractVideoNative(path string) (Meta, error) {
	m := Meta{Kind: "video", Prefix: "VID", TimeSource: "none"}
	e, err := readEBML(path)
	if err != nil {
		e, err = readFLV(path)
	}
	if err == nil {
		// Matroska / WebM: DateUTC and duration of the Segment Info, size of the
		// first video track; FLV: onMetaData (what ffprobe reports for them)
		m.DurationSec = e.DurationSec
		if !e.Creation.IsZero() {
			if t, ok := CanonicalVideo(e.Creation.UTC().Format(time.RFC3339), e.DurationSec); ok {
				m.Time, m.TimeSource = t, "metadata"
			}
		}
		m.Width, m.Height = e.Width, e.Height
		m.Device = VendorSlug(VSignals{})
	} else if info, err := readISOBMFF(path); err == nil {
		m.DurationSec = info.DurationSec
		if !info.Creation.IsZero() && info.Creation.Year() > 1904 {
			if t, ok := CanonicalVideo(info.Creation.UTC().Format(time.RFC3339), info.DurationSec); ok {
				m.Time, m.TimeSource = t, "metadata"
			}
		}
		m.Width, m.Height = info.Width, info.Height
		m.Device = VendorSlug(info.vendorSignals())
		videoGeo(&m, info.Tags)
	}
	if m.Time == "" {
		if t, ok := DeduceFromFilename(filepath.Base(path)); ok {
			m.Time, m.TimeSource = t, "filename"
		}
	}
	return m, nil
}

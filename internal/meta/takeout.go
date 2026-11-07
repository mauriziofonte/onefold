// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/mauriziofonte/onefold/internal/kinds"
)

// TakeoutMax is the largest Google Takeout metadata file read (they are a
// few kilobytes): anything bigger is not one.
const TakeoutMax = 1 << 20

// takeoutFile is the part of a Google Takeout metadata file onefold reads.
type takeoutFile struct {
	PhotoTakenTime struct {
		Timestamp string `json:"timestamp"` // Unix seconds, UTC
	} `json:"photoTakenTime"`
	GeoData     takeoutGeo `json:"geoData"`
	GeoDataExif takeoutGeo `json:"geoDataExif"`
}

type takeoutGeo struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// applyTakeout completes m from the Google Takeout metadata file next to the
// photo or video at path (kinds.TakeoutNames): the capture time when neither
// the file's metadata nor its name gave one (time source "takeout"), the
// place when the file has none, and an edited copy ("-edited", which Takeout
// exports next to the original) becomes an edit (category editing): its own
// asset, never dropped as a lower-resolution variant of the original.
func applyTakeout(path string, m *Meta) {
	dir, base := filepath.Split(path)
	if kinds.TakeoutEdited(base) != "" {
		m.Category, m.Prefix = "editing", "EDT"
	}
	if m.TimeSource == "metadata" || m.TimeSource == "filename" {
		if m.Lat != 0 || m.Lon != 0 {
			return
		}
	}
	var tf takeoutFile
	found := false
	for _, n := range kinds.TakeoutNames(base) {
		fi, err := os.Stat(filepath.Join(dir, n))
		if err != nil || !fi.Mode().IsRegular() || fi.Size() > TakeoutMax {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, n))
		if err == nil && json.Unmarshal(b, &tf) == nil {
			found = true
			break
		}
	}
	if !found {
		return
	}
	if m.Time == "" || m.TimeSource == "none" {
		if ts, err := strconv.ParseInt(tf.PhotoTakenTime.Timestamp, 10, 64); err == nil && ts > 0 {
			if t := time.Unix(ts, 0).In(Zone()); yearOK(t.Year()) {
				m.Time, m.TimeSource = t.Format(canonFmt), "takeout"
			}
		}
	}
	if m.Lat == 0 && m.Lon == 0 {
		for _, g := range []takeoutGeo{tf.GeoDataExif, tf.GeoData} {
			if validGeo(g.Latitude, g.Longitude) {
				m.Lat, m.Lon = round6(g.Latitude), round6(g.Longitude)
				break
			}
		}
	}
}

// validGeo: a real position (0,0 is what files without one carry).
func validGeo(lat, lon float64) bool {
	return (lat != 0 || lon != 0) && lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
}

// round6: about 10 cm, more than any phone measures; keeps manifests short.
func round6(v float64) float64 {
	f, _ := strconv.ParseFloat(strconv.FormatFloat(v, 'f', 6, 64), 64)
	return f
}

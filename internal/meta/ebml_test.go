// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import (
	"math"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mauriziofonte/onefold/internal/tools"
)

// Matroska and WebM made by ffmpeg: the EBML reader (the path without
// ffprobe) gives what ffprobe gives: time, duration, size; without DateUTC
// the file is not dated by metadata.
func TestEBMLParity(t *testing.T) {
	ff, ok := tools.Path("ffmpeg")
	if !ok || !tools.Has("ffprobe") {
		t.Skip("ffmpeg/ffprobe not installed")
	}
	prev := Zone()
	defer SetZone(prev)
	ny, _ := time.LoadLocation("America/New_York")
	SetZone(ny)
	d := t.TempDir()
	clips := []struct {
		name  string
		args  []string
		dated bool
	}{
		{"a.mkv", []string{"-c:v", "mpeg4", "-metadata", "creation_time=2023-07-08T09:10:11Z"}, true},
		{"a.webm", []string{"-c:v", "libvpx", "-b:v", "50k", "-metadata", "creation_time=2023-07-08T09:10:11Z"}, true},
		{"nodate.mkv", []string{"-c:v", "mpeg4"}, false},
		{"a.flv", []string{"-metadata", "creation_time=2023-07-08T09:10:11Z"}, true},
		{"nodate.flv", nil, false},
	}
	for _, c := range clips {
		p := filepath.Join(d, c.name)
		args := append([]string{"-loglevel", "error", "-f", "lavfi", "-i", "color=c=red:s=320x240:d=3:r=5"}, c.args...)
		if out, err := exec.Command(ff, append(args, p)...).CombinedOutput(); err != nil {
			t.Logf("ffmpeg cannot write %s here (encoder missing): %v %s", c.name, err, out)
			continue
		}
		probe, _ := extractVideo(p)
		native, _ := extractVideoNative(p)
		if native.Time != probe.Time || native.TimeSource != probe.TimeSource || native.Width != probe.Width ||
			native.Height != probe.Height || native.Device != probe.Device || math.Abs(native.DurationSec-probe.DurationSec) > 0.001 {
			t.Errorf("%s:\nnative %+v\nffprobe %+v", c.name, native, probe)
		}
		if (native.TimeSource == "metadata") != c.dated {
			t.Errorf("%s: time source %s", c.name, native.TimeSource)
		}
	}
	if _, err := readEBML(filepath.Join("testdata", "photo", "createdate.jpg")); err == nil {
		t.Error("a JPEG read as Matroska")
	}
}

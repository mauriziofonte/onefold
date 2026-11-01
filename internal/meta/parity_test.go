// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mauriziofonte/onefold/internal/tools"
)

// The same corpus extracted with external tools and without them (Windows
// condition, ONEFOLD_NO_EXTERNAL=1) must agree. Documented exceptions only.
var parityExceptions = map[string]string{
	// pdfinfo reads the full xref; the pure-Go scan only sees the fragment
	"documents/corrupted/truncated.pdf": "pages",
}

func TestParityWithoutExternalTools(t *testing.T) {
	if _, err := os.Stat(corpus); err != nil {
		t.Skip("testdata/fixtures missing")
	}
	with := map[string]Meta{}
	filepath.WalkDir(corpus, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		m, _ := ExtractWith(p, Options{})
		with[p] = m
		return nil
	})
	t.Setenv("ONEFOLD_NO_EXTERNAL", "1")
	tools.Reset()
	defer tools.Reset()
	for p, a := range with {
		b, _ := ExtractWith(p, Options{})
		rel, _ := filepath.Rel(corpus, p)
		rel = filepath.ToSlash(rel)
		ex := parityExceptions[rel]
		if a.Kind != b.Kind || a.Time != b.Time || a.TimeSource != b.TimeSource || a.Entries != b.Entries ||
			a.Title != b.Title || a.Author != b.Author || (ex != "pages" && a.Pages != b.Pages) || a.ExtMismatch != b.ExtMismatch {
			t.Errorf("%s differs without tools:\n with    %+v\n without %+v", rel, a, b)
		}
	}
}

// Video: ffprobe vs the pure-Go ISO-BMFF reader on a real ffmpeg-muxed mp4/mov.
func TestVideoParity(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	d := t.TempDir()
	for _, name := range []string{"clip.mp4", "clip.mov", "clip.3gp"} {
		p := filepath.Join(d, name)
		args := []string{"-loglevel", "error", "-f", "lavfi", "-i", "testsrc=d=3:s=320x240", "-f", "lavfi", "-i", "sine=d=3",
			"-shortest", "-metadata", "creation_time=2021-04-04T11:26:44Z"}
		if name == "clip.3gp" {
			args = append(args, "-c:v", "mpeg4", "-c:a", "aac", "-ar", "8000", "-ac", "1", "-s", "176x144")
		} else {
			args = append(args, "-pix_fmt", "yuv420p")
		}
		if out, err := exec.Command("ffmpeg", append(args, p)...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %v %s", name, err, out)
		}
		a, _ := ExtractWith(p, Options{})
		n, _ := extractVideoNative(p)
		if a.Time != n.Time || a.TimeSource != "metadata" || a.Width != n.Width || a.Height != n.Height ||
			int(a.DurationSec+0.5) != int(n.DurationSec+0.5) || a.Device != n.Device {
			t.Errorf("%s: ffprobe %+v\n native %+v", name, a, n)
		}
	}
}

// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package meta

import (
	"io/fs"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/mauriziofonte/onefold/internal/tools"
)

// The exiftool readers give exactly what one exiftool per file gives, on
// every file of the corpus, from parallel callers, in two zones.
func TestExifReadersParity(t *testing.T) {
	if !tools.Has("exiftool") {
		t.Skip("exiftool not installed")
	}
	var files []string
	for _, dir := range []string{filepath.Join("..", "..", "testdata", "fixtures"), "testdata"} {
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err == nil && d.Type().IsRegular() {
				files = append(files, p)
			}
			return nil
		})
	}
	prev := Zone()
	defer func() { SetZone(prev); useReaders = true; CloseExifReaders() }()
	for _, zone := range []string{"UTC", "America/New_York"} {
		l, _ := time.LoadLocation(zone)
		SetZone(l)
		useReaders = false
		want := map[string]Meta{}
		for _, f := range files {
			m, _ := Extract(f)
			want[f] = m
		}
		useReaders = true
		var mu sync.Mutex
		got := map[string]Meta{}
		var wg sync.WaitGroup
		for w := 0; w < 6; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				for i := w; i < len(files); i += 6 {
					m, _ := Extract(files[i])
					mu.Lock()
					got[files[i]] = m
					mu.Unlock()
				}
			}(w)
		}
		wg.Wait()
		for _, f := range files {
			if !reflect.DeepEqual(got[f], want[f]) {
				t.Errorf("%s %s:\nreaders %+v\none-shot %+v", zone, f, got[f], want[f])
			}
		}
	}
}

// exiftool through a reader answers the same JSON as a one-shot run.
func TestExifReaderAnswers(t *testing.T) {
	if !tools.Has("exiftool") {
		t.Skip("exiftool not installed")
	}
	defer CloseExifReaders()
	p := filepath.Join("testdata", "photo", "createdate.jpg")
	m := exifJSON(p, "-CreateDate")
	if m == nil || m["CreateDate"] != "2018:03:04 05:06:07" {
		t.Fatalf("%v", m)
	}
	readers.mu.Lock()
	idle := len(readers.free)
	readers.mu.Unlock()
	if idle != 1 {
		t.Fatalf("%d idle readers after one request, want 1 (the request did not use a reader)", idle)
	}
	if m := exifJSON(filepath.Join(t.TempDir(), "missing.jpg"), "-CreateDate"); m != nil {
		t.Fatalf("missing file: %v", m)
	}
	if m := exifJSON(p, "-CreateDate"); m == nil {
		t.Fatal("the reader is gone after a missing file")
	}
}

// A reader nobody uses stops by itself: exiftool -stay_open never exits on
// its own, so a forgotten reader would outlive onefold.
func TestExifReaderRetiresWhenIdle(t *testing.T) {
	if !tools.Has("exiftool") {
		t.Skip("exiftool not installed")
	}
	defer CloseExifReaders()
	if m := exifJSON(filepath.Join("testdata", "photo", "createdate.jpg"), "-CreateDate"); m == nil {
		t.Fatal("no answer")
	}
	deadline := time.Now().Add(exifIdle + 5*time.Second)
	for {
		readers.mu.Lock()
		reaping, idle := readers.reaping, len(readers.free)
		readers.mu.Unlock()
		if !reaping && idle == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("after %v: %d idle readers still running", exifIdle+5*time.Second, idle)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

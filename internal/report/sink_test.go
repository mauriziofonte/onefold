// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package report

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// With a sink, a Reporter emits start/progress/error/done events and nothing
// reaches the terminal; without it the CLI path is unchanged.
func TestSinkEvents(t *testing.T) {
	var mu sync.Mutex
	var evs []Event
	SetSink(func(e Event) { mu.Lock(); evs = append(evs, e); mu.Unlock() })
	defer SetSink(nil)
	r := New(Options{Label: "copy", Unit: Bytes, Growing: true})
	r.AddTotal(100)
	r.AddTotal(50)
	r.TotalKnown()
	r.Item(100, time.Millisecond)
	r.ErrorAt("/x/broken.bin", errors.New("read error"))
	r.Advance(50)
	r.Event("ERR /x/y: copy failed")
	r.Event("name taken")
	Printf("cataloging %d rooms\n", 2)
	r.Finish()

	kinds := map[string]int{}
	var last Event
	for _, e := range evs {
		kinds[e.Kind]++
		last = e
	}
	if kinds["start"] != 1 || kinds["done"] != 1 || kinds["error"] != 2 || kinds["warn"] != 1 || kinds["status"] != 1 || kinds["progress"] < 1 {
		t.Fatalf("event kinds %v", kinds)
	}
	if last.Kind != "status" && last.Kind != "done" {
		t.Fatalf("last event %+v", last)
	}
	var done Event
	for _, e := range evs {
		if e.Kind == "done" {
			done = e
		}
		if e.Kind == "error" && e.Path == "/x/broken.bin" && e.Msg != "read error" {
			t.Fatalf("error event %+v", e)
		}
	}
	if done.Done != 150 || done.Total != 150 || !done.Final || done.Errors != 1 || done.Label != "copy" {
		t.Fatalf("done event %+v", done)
	}
}

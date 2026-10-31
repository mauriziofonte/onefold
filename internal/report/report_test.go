// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package report

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestLogModeGrowingItems(t *testing.T) {
	var buf bytes.Buffer
	r := New(Options{Label: "catalog", Unit: Items, Growing: true, Out: &buf, Heartbeat: time.Hour})
	for i := 0; i < 3; i++ {
		r.AddTotal(100)
	}
	r.Item(100, time.Millisecond)
	r.Err()
	r.Advance(100) // failed item still advances
	Printf("status %d\n", 7)
	r.TotalKnown()
	r.Item(100, time.Millisecond)
	r.Stop()
	r.Stop() // idempotent
	out := buf.String()
	for _, want := range []string{"task=catalog", "status 7", "items=3", "of=3 ", "pct=100.0%"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if active.Load() != nil {
		t.Fatal("reporter still active after Stop")
	}
	if HumanBytes(1536) != "1.50 KB" || HumanBytes(10) != "10 B" {
		t.Fatal("HumanBytes")
	}
}

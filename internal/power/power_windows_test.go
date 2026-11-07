// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package power

import "testing"

// The execution state is taken and given back; background mode starts and
// ends (asking twice is not an error).
func TestPowerWindows(t *testing.T) {
	release, err := keepAwake(Reason)
	if err != nil {
		t.Fatal(err)
	}
	release()
	for _, on := range []bool{true, true, false, false} {
		if err := SetLowPriority(on); err != nil {
			t.Fatalf("SetLowPriority(%v): %v", on, err)
		}
	}
}

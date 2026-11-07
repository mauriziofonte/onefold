// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package power

import (
	"os/exec"
	"strings"
	"testing"
)

func assertions(t *testing.T) string {
	out, err := exec.Command("pmset", "-g", "assertions").Output()
	if err != nil {
		t.Fatalf("pmset: %v", err)
	}
	return string(out)
}

// The lock is a PreventUserIdleSystemSleep assertion named by Reason, gone
// after release.
func TestKeepAwakeDarwin(t *testing.T) {
	release, err := keepAwake(Reason)
	if err != nil {
		t.Fatal(err)
	}
	if a := assertions(t); !strings.Contains(a, Reason) || !strings.Contains(a, "PreventUserIdleSystemSleep") {
		release()
		t.Fatalf("assertion not listed:\n%s", a)
	}
	release()
	if a := assertions(t); strings.Contains(a, Reason) {
		t.Fatalf("assertion left after release:\n%s", a)
	}
}

func TestLowPriorityDarwin(t *testing.T) {
	if err := SetLowPriority(true); err != nil {
		t.Fatal(err)
	}
	if err := SetLowPriority(false); err != nil {
		t.Fatal(err)
	}
}

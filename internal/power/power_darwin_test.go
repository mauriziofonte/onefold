// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package power

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The assertions this process holds, one line each, and the whole listing.
// Other onefold processes (the command tests run beside this package) hold
// assertions with the same name, so only lines of this pid count.
func assertions(t *testing.T) (ours []string, all string) {
	out, err := exec.Command("pmset", "-g", "assertions").Output()
	if err != nil {
		t.Fatalf("pmset: %v", err)
	}
	pid := fmt.Sprintf("pid %d(", os.Getpid())
	for _, l := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), pid) {
			ours = append(ours, l)
		}
	}
	return ours, string(out)
}

func holds(lines []string, sub ...string) bool {
	for _, l := range lines {
		ok := true
		for _, s := range sub {
			ok = ok && strings.Contains(l, s)
		}
		if ok {
			return true
		}
	}
	return false
}

// The lock is a PreventUserIdleSystemSleep assertion named by Reason, gone
// after release.
func TestKeepAwakeDarwin(t *testing.T) {
	release, err := keepAwake(Reason)
	if err != nil {
		t.Fatal(err)
	}
	if ours, all := assertions(t); !holds(ours, "PreventUserIdleSystemSleep", Reason) {
		release()
		t.Fatalf("assertion not listed for pid %d:\n%s", os.Getpid(), all)
	}
	release()
	if ours, all := assertions(t); holds(ours, Reason) {
		t.Fatalf("assertion left after release by pid %d:\n%s", os.Getpid(), all)
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

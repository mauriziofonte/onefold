// onefold
// Author: Maurizio Fonte <fonte.maurizio@gmail.com>
// Copyright (c) 2026 Maurizio Fonte
// SPDX-License-Identifier: AGPL-3.0-or-later

package power

import "testing"

// Nested locks share one: the system lock ends with the last release, and a
// second release of the same lock does nothing.
func TestKeepAwakeNests(t *testing.T) {
	calls, stops := 0, 0
	defer func(f func(string) (func(), error)) { keepAwakeFn = f }(keepAwakeFn)
	keepAwakeFn = func(string) (func(), error) {
		calls++
		return func() { stops++ }, nil
	}
	r1, err1 := KeepAwake(Reason)
	r2, err2 := KeepAwake(Reason)
	if err1 != nil || err2 != nil || calls != 1 {
		t.Fatalf("calls %d, errors %v %v", calls, err1, err2)
	}
	r1()
	r1()
	if stops != 0 {
		t.Fatal("the lock ended while another holder had it")
	}
	r2()
	if stops != 1 {
		t.Fatalf("stops %d after the last release", stops)
	}
}

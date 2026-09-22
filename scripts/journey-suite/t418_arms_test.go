// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

// Each arm is satisfied only by its own seat's classify, retry, or surface
// line. A T517 reap does not count: that short-circuit is withdrawn (🎯T850).
func TestHandoverLineForArms(t *testing.T) {
	blob := "INFO 🎯T418 handover classify agent=jv-t418h-1 action=retry\n" +
		"INFO 🎯T418 handover classify agent=jv-t418w-1 action=retry\n"
	if handoverLineFor(blob, "jv-t418h-1", t418ClassifyNeedles...) == "" {
		t.Fatal("aside arm missed its line")
	}
	if handoverLineFor(blob, "jv-t418w-1", t418ClassifyNeedles...) == "" {
		t.Fatal("second arm missed its line")
	}
	if handoverLineFor("INFO 🎯T418 handover classify agent=jv-t418w-1\n", "jv-t418h-1", t418ClassifyNeedles...) != "" {
		t.Fatal("aside arm satisfied by the other seat")
	}
	if handoverLineFor("INFO 🎯T517 handover reaped agent=jv-t418h-1 reason=control-plane\n", "jv-t418h-1", t418ClassifyNeedles...) != "" {
		t.Fatal("T517 reap satisfied the aside arm")
	}
	if handoverLineFor("", "x", t418ClassifyNeedles...) != "" {
		t.Fatal("empty log matched")
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

// 🎯T625.11: each arm is satisfied only by its own seat's line, and the
// control-plane arm only by the T517 reap.
func TestHandoverLineForArms(t *testing.T) {
	blob := "INFO 🎯T517 handover reaped agent=jv-t418h-1 reason=control-plane\n" +
		"INFO 🎯T418 handover classify agent=jv-t418w-1 action=retry\n"
	if handoverLineFor(blob, "jv-t418h-1", t418ReapNeedles...) == "" {
		t.Fatal("reap arm missed its line")
	}
	if handoverLineFor(blob, "jv-t418w-1", t418ClassifyNeedles...) == "" {
		t.Fatal("classify arm missed its line")
	}
	// Controls: a sweep that only classified the worker, or only reaped the
	// aside, must not satisfy the other arm.
	if handoverLineFor("INFO 🎯T418 handover classify agent=jv-t418w-1\n", "jv-t418h-1", t418ReapNeedles...) != "" {
		t.Fatal("reap arm satisfied without a reap")
	}
	if handoverLineFor("INFO 🎯T517 handover reaped agent=jv-t418h-1\n", "jv-t418w-1", t418ClassifyNeedles...) != "" {
		t.Fatal("classify arm satisfied by the other seat's reap")
	}
	if handoverLineFor("", "x", t418ReapNeedles...) != "" {
		t.Fatal("empty log matched")
	}
}

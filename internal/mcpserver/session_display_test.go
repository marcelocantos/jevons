// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
)

func TestSessionDisplayDistinguishesUUIDV7Peers(t *testing.T) {
	// Two concurrent UUID-v7-shaped ids sharing the 8-char time prefix
	// that agent_list used to show alone.
	a := "019fbaf1-aaaa-7000-8000-111111111111"
	b := "019fbaf1-bbbb-7000-8000-222222222222"
	da := sessionDisplay(a)
	db := sessionDisplay(b)
	if da == db {
		t.Fatalf("sessionDisplay collided for peers: both %q", da)
	}
	if strings.HasPrefix(da, "019fbaf1") && len(da) == 8 {
		t.Fatalf("sessionDisplay still 8-char only: %q", da)
	}
	if !strings.Contains(da, "…") {
		t.Fatalf("sessionDisplay(%q) = %q, want head…tail form", a, da)
	}
	// Tail must differ when entropy differs.
	if strings.HasSuffix(da, "111111") == strings.HasSuffix(db, "222222") &&
		da[len(da)-6:] == db[len(db)-6:] {
		t.Fatalf("tails not from distinct entropy: %q vs %q", da, db)
	}
	if da[len(da)-6:] == db[len(db)-6:] {
		t.Fatalf("tails equal %q — not distinguishing", da[len(da)-6:])
	}
}

func TestSessionDisplayShortPassthrough(t *testing.T) {
	if got := sessionDisplay("abc"); got != "abc" {
		t.Fatalf("short id: got %q", got)
	}
}

func TestShortAliasesSessionDisplay(t *testing.T) {
	id := "019fbaf1-cccc-7000-8000-333333333333"
	if short(id) != sessionDisplay(id) {
		t.Fatalf("short != sessionDisplay: %q vs %q", short(id), sessionDisplay(id))
	}
}

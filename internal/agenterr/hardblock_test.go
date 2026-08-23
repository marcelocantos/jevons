// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr_test

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

// 🎯T406: HardBlock is the narrower wall predicate. Spend/billing and auth
// refuse work forever; ordinary 429 / Internal error do not.
func TestHardBlockFixtures(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw  string
		want bool
	}{
		{"You've hit your monthly spend limit. Run /usage-credits to manage your limit", true},
		{"organization spend limit reached", true},
		{"Billing hard limit exceeded — raise it at claude.ai/settings/usage", true},
		{"out of credits", true},
		{"402 Payment Required", true},
		{"your API key has been revoked", true},
		{"account has been suspended", true},
		{"401 Unauthorized", true},
		{"invalid API key", true},

		// Transient — must NOT enter the fleet wall.
		{"rate limit exceeded", false},
		{"HTTP 429 Too Many Requests", false},
		{"resource_exhausted: throttle", false},
		{"Internal error", false},
		{"connection refused", false},
		{"502 Bad Gateway", false},
		{"Continuing mission work on T406", false},
		{"", false},
	}
	for _, tc := range cases {
		class := agenterr.ClassifyText(tc.raw)
		got := agenterr.HardBlock(class, tc.raw)
		if got != tc.want {
			t.Errorf("HardBlock(class=%s, %q)=%v want %v", class, tc.raw, got, tc.want)
		}
		if tc.want && agenterr.HardBlockReason(class, tc.raw) == "" {
			t.Errorf("HardBlockReason empty for hard-block %q", tc.raw)
		}
		if !tc.want && agenterr.HardBlockReason(class, tc.raw) != "" {
			t.Errorf("HardBlockReason=%q for non-block %q", agenterr.HardBlockReason(class, tc.raw), tc.raw)
		}
	}
}

// Over-broadness: ClassNone never hard-blocks, even when the raw text names a
// spend wall. That is what keeps a mutant that treats every error as a wall
// from also treating unclassified prose as a wall via the class gate.
func TestHardBlockClassNoneNeverBlocks(t *testing.T) {
	t.Parallel()
	raw := "You've hit your monthly spend limit"
	if agenterr.HardBlock(agenterr.ClassNone, raw) {
		t.Fatal("ClassNone must never hard-block")
	}
}

// Spend walls classify as rate_limit (T407 cluster) but HardBlock is true;
// ordinary rate_limit is not.
func TestHardBlockSpendVsThrottle(t *testing.T) {
	t.Parallel()
	spend := "You've hit your monthly spend limit"
	if c := agenterr.ClassifyText(spend); c != agenterr.ClassRateLimit {
		t.Fatalf("spend class=%s want rate_limit", c)
	}
	if !agenterr.HardBlock(agenterr.ClassRateLimit, spend) {
		t.Fatal("spend wall must hard-block under rate_limit")
	}
	throttle := "rate limit exceeded"
	if agenterr.HardBlock(agenterr.ClassRateLimit, throttle) {
		t.Fatal("ordinary 429 must not hard-block")
	}
}

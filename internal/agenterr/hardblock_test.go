// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr_test

import (
	"errors"
	"testing"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

// 🎯T406: HardBlock is the narrower wall predicate. Spend/billing and auth
// refuse work forever; ordinary 429 / Internal error do not.
func TestHardBlockFixtures(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw   string
		cause string
		want  bool
	}{
		{"You've hit your monthly spend limit. Run /usage-credits to manage your limit", agenterr.CauseRateLimit, true},
		{"organization spend limit reached", agenterr.CauseRateLimit, true},
		{"Billing hard limit exceeded — raise it at claude.ai/settings/usage", agenterr.CauseRateLimit, true},
		{"out of credits", agenterr.CauseRateLimit, true},
		{"402 Payment Required", agenterr.CauseRateLimit, true},
		{"your API key has been revoked", agenterr.CauseAuth, true},
		{"account has been suspended", agenterr.CauseAuth, true},
		{"401 Unauthorized", agenterr.CauseAuth, true},
		{"invalid API key", agenterr.CauseAuth, true},

		// Prose alone must NOT enter the fleet wall.
		{"rate limit exceeded", "", false},
		{"HTTP 429 Too Many Requests", "", false},
		{"resource_exhausted: throttle", "", false},
		{"Internal error", "", false},
		{"connection refused", "", false},
		{"502 Bad Gateway", "", false},
		{"Continuing mission work on T406", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		class := agenterr.ClassifyText(tc.raw)
		if tc.cause != "" {
			class = agenterr.Classify(agenterr.WithCause(tc.cause, errors.New(tc.raw)))
		}
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
	if c := agenterr.ClassifyText(spend); c == agenterr.ClassRateLimit {
		t.Fatal("spend prose is not a rate_limit class")
	}
	if c := agenterr.Classify(agenterr.WithCause(agenterr.CauseRateLimit, errors.New(spend))); c != agenterr.ClassRateLimit {
		t.Fatalf("caused spend class=%s want rate_limit", c)
	}
	if !agenterr.HardBlock(agenterr.ClassRateLimit, spend) {
		t.Fatal("spend wall must hard-block under rate_limit")
	}
	throttle := "rate limit exceeded"
	if agenterr.HardBlock(agenterr.ClassRateLimit, throttle) {
		t.Fatal("ordinary 429 must not hard-block")
	}
}

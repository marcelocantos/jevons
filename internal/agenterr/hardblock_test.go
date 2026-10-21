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
		{"You've hit your monthly spend limit. Run /usage-credits to manage your limit", agenterr.CauseSpendWall, true},
		{"organization spend limit reached", agenterr.CauseSpendWall, true},
		{"Billing hard limit exceeded — raise it at claude.ai/settings/usage", agenterr.CauseSpendWall, true},
		{"out of credits", agenterr.CauseSpendWall, true},
		{"402 Payment Required", agenterr.CauseSpendWall, true},
		{"rate limit exceeded", agenterr.CauseSpendWall, true},
		{"your API key has been revoked", agenterr.CauseAuth, true},
		{"account has been suspended", agenterr.CauseAccountWall, true},
		{"401 Unauthorized", agenterr.CauseAuth, true},
		{"invalid API key", agenterr.CauseAuth, true},

		// The same sentences under a throttle, or with no cause, are not a wall.
		{"You've hit your monthly spend limit", agenterr.CauseRateLimit, false},
		{"account has been suspended", agenterr.CauseRateLimit, false},
		{"You've hit your monthly spend limit", "", false},
		{"your API key has been revoked", "", false},
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

// A typed spend cause hard-blocks. The same sentence under rate_limit does
// not, and ordinary throttle does not. The class decides; the sentence does not.
func TestHardBlockSpendVsThrottle(t *testing.T) {
	t.Parallel()
	spend := "You've hit your monthly spend limit"
	if c := agenterr.ClassifyText(spend); c == agenterr.ClassRateLimit || c == agenterr.ClassSpendWall {
		t.Fatalf("spend prose class=%s", c)
	}
	rate := agenterr.Classify(agenterr.WithCause(agenterr.CauseRateLimit, errors.New(spend)))
	if rate != agenterr.ClassRateLimit {
		t.Fatalf("caused spend class=%s want rate_limit", rate)
	}
	if agenterr.HardBlock(rate, spend) {
		t.Fatal("rate_limit must not hard-block, including spend-limit English")
	}
	wall := agenterr.Classify(agenterr.WithCause(agenterr.CauseSpendWall, errors.New(spend)))
	if wall != agenterr.ClassSpendWall {
		t.Fatalf("spend cause class=%s", wall)
	}
	if !agenterr.HardBlock(wall, "rate limit exceeded") {
		t.Fatal("spend class must hard-block even when the sentence is a throttle")
	}
	if agenterr.HardBlockReason(wall, spend) != "provider spend/billing limit" {
		t.Fatalf("reason=%q", agenterr.HardBlockReason(wall, spend))
	}
	if agenterr.TransientBackend(agenterr.ClassSpendWall) || agenterr.TransientBackend(agenterr.ClassAccountWall) {
		t.Fatal("spend and account walls must not rebrief")
	}
	throttle := "rate limit exceeded"
	if agenterr.HardBlock(agenterr.ClassRateLimit, throttle) {
		t.Fatal("ordinary 429 must not hard-block")
	}
}

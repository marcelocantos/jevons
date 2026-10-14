// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr_test

import (
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

// 🎯T862.12: a budget of one does not silently kill a turn one step from
// done without a classified retry-exhausted outcome.
func TestClassifyTextRetryExhausted(t *testing.T) {
	t.Parallel()
	cases := []string{
		"retry_exhausted: connection timed out after 1 retry",
		"retries exceeded: 503 Service Unavailable after 1 retry",
		"max retries exceeded",
		"Max retries exceeded while connecting",
		"retries exhausted after timeout",
		"retry limit exceeded",
		"retry budget exhausted",
		"out of retries: connection timed out",
		"gave up after 1 retry: 500 internal server error",
	}
	for _, in := range cases {
		if got := agenterr.ClassifyText(in); got != agenterr.ClassRetryExhausted {
			t.Errorf("ClassifyText(%q) = %q, want retry_exhausted", in, got)
		}
	}
}

func TestIsRetryExhausted(t *testing.T) {
	t.Parallel()
	if !agenterr.IsRetryExhausted("Max Retries Exceeded") {
		t.Fatal("expected retry-exhausted marker to match case-insensitively")
	}
	if agenterr.IsRetryExhausted("Internal error") {
		t.Fatal("a plain backend failure must not classify as retry_exhausted")
	}
}

func TestRetryExhaustedNotTransient(t *testing.T) {
	t.Parallel()
	// A retry-exhausted turn already spent its budget; blindly
	// re-pressuring the identical step is the failure mode this class
	// exists to name, not to repeat.
	if agenterr.TransientBackend(agenterr.ClassRetryExhausted) {
		t.Fatal("retry_exhausted must not be treated as poll-and-retry transient")
	}
}

func TestOwnerCopyRetryExhaustedNamesBudget(t *testing.T) {
	t.Parallel()
	got := agenterr.OwnerCopy(agenterr.ClassRetryExhausted, "max retries exceeded after 1 retry: dial tcp timeout")
	if !strings.Contains(got, "retry_exhausted") {
		t.Fatalf("owner copy missing class tag: %q", got)
	}
	if !strings.Contains(got, "after 1 retry") || !strings.Contains(got, "provider-specific") {
		t.Fatalf("owner copy should preserve the reported budget and explain its source: %q", got)
	}
}

func TestRetryBudgetIsNotInvented(t *testing.T) {
	t.Parallel()
	got := agenterr.OwnerCopy(agenterr.ClassRetryExhausted, "max retries exceeded")
	if strings.Contains(got, "budget 1") || strings.Contains(got, "retried once") || !strings.Contains(got, "provider-specific") {
		t.Fatalf("unknown provider budget must not be presented as one: %q", got)
	}
}

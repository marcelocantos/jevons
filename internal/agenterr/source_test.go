// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr_test

import (
	"errors"
	"testing"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

// 🎯T455: the same refusal string is not a provider failure from either
// side of the wire. Quoting it is not an outage, and neither is receiving
// the sentence as transport text. The class is a typed cause.
func TestT455ClassifyFromFixturePairs(t *testing.T) {
	t.Parallel()

	pairs := []string{
		"Internal error",
		"429 Too Many Requests",
		"401 Unauthorized",
		"grok acp: no session",
		"failed earlier slices",
		"failed",
		`The backend said "Internal error" but that was last hour; the fleet is working now.`,
		"**The whole fleet is dead — Claude account monthly spend limit reached.**",
		"The spend limit is still in force and I'm holding the fleet.",
	}

	for _, msg := range pairs {
		if got := agenterr.ClassifyFrom(agenterr.SourceAuthored, msg); got != agenterr.ClassNone {
			t.Errorf("authored %q: ClassifyFrom=%q want none", msg, got)
		}
		if got := agenterr.ClassifyFrom(agenterr.SourceTransport, msg); got != agenterr.ClassNone {
			t.Errorf("transport %q: ClassifyFrom=%q want none", msg, got)
		}
		if agenterr.ClassifyText(msg) != agenterr.ClassNone {
			t.Errorf("ClassifyText(%q) = %q, want none", msg, agenterr.ClassifyText(msg))
		}
	}

	caused := agenterr.Classify(agenterr.WithCause(agenterr.CauseBackendUnavailable, errors.New("Internal error")))
	if caused != agenterr.ClassBackendUnavailable {
		t.Fatalf("typed backend cause = %q", caused)
	}
	if agenterr.Classify(errors.New("Internal error")) != agenterr.ClassNone {
		t.Fatal("the same sentence without a cause classified")
	}

	// Empty / unknown source fail closed.
	if got := agenterr.ClassifyFrom("", "Internal error"); got != agenterr.ClassNone {
		t.Errorf("empty source classified as %q, want none", got)
	}
	if got := agenterr.ClassifyFrom(agenterr.Source("chat"), "Internal error"); got != agenterr.ClassNone {
		t.Errorf("unknown source classified as %q, want none", got)
	}
}

func TestT455ClassifyFromDoesNotBlindDetector(t *testing.T) {
	t.Parallel()
	// The detector is the cause. Transport prose is not a failure, and a
	// typed cause still classifies when the sentence is unrelated.
	for _, msg := range []string{
		"Internal error",
		"acp session/prompt: Internal error",
		"failed",
		"failed earlier slices",
	} {
		if agenterr.ClassifyText(msg).IsFailure() {
			t.Fatalf("fixture %q classified from its words", msg)
		}
		if got := agenterr.ClassifyFrom(agenterr.SourceTransport, msg); got != agenterr.ClassNone {
			t.Errorf("transport %q = %q want none", msg, got)
		}
	}
	unrelated := agenterr.WithCause(agenterr.CauseBackendUnavailable, errors.New("the fleet is fine"))
	if got := agenterr.Classify(unrelated); got != agenterr.ClassBackendUnavailable {
		t.Fatalf("typed cause = %q", got)
	}
}

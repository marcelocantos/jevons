// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr_test

import (
	"testing"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

// 🎯T455: the same refusal string is a provider failure only when the
// backend returned it. Quoting it is not an outage.
func TestT455ClassifyFromFixturePairs(t *testing.T) {
	t.Parallel()

	// Load-bearing pairs: ClassifyText would fire on every row (so a
	// classifier that ignores Source fails the authored arm), and the
	// transport arm must keep that same class (so a classifier that
	// records nothing also fails).
	pairs := []struct {
		name string
		msg  string
		want agenterr.Class
	}{
		{"bare internal error", "Internal error", agenterr.ClassBackendUnavailable},
		{"rate limit", "429 Too Many Requests", agenterr.ClassRateLimit},
		{"auth", "401 Unauthorized", agenterr.ClassAuth},
		{"client bug", "grok acp: no session", agenterr.ClassClientBug},
		{
			name: "quoted internal error in a report",
			msg:  `The backend said "Internal error" but that was last hour; the fleet is working now.`,
			want: agenterr.ClassBackendUnavailable,
		},
		{
			// Verbatim opening of the 2026-08-15 overseer status that
			// manufactured a provider_failure. Whatever class the words
			// map to, authored must not record it.
			name: "incident overseer spend-limit status",
			msg:  "**The whole fleet is dead — Claude account monthly spend limit reached.**",
			want: agenterr.ClassifyText("**The whole fleet is dead — Claude account monthly spend limit reached.**"),
		},
		{
			name: "incident overseer holding copy",
			msg:  "The spend limit is still in force and I'm holding the fleet.",
			want: agenterr.ClassifyText("The spend limit is still in force and I'm holding the fleet."),
		},
	}

	sawTransportFailure := false
	sawAuthoredWouldHaveFired := false
	for _, p := range pairs {
		if got := agenterr.ClassifyFrom(agenterr.SourceAuthored, p.msg); got != agenterr.ClassNone {
			t.Errorf("authored %s: ClassifyFrom=%q want none", p.name, got)
		}
		if got := agenterr.ClassifyFrom(agenterr.SourceTransport, p.msg); got != p.want {
			t.Errorf("transport %s: ClassifyFrom=%q want %q", p.name, got, p.want)
		}
		if p.want.IsFailure() {
			sawTransportFailure = true
			if agenterr.ClassifyText(p.msg).IsFailure() {
				sawAuthoredWouldHaveFired = true
			}
		}
	}
	if !sawTransportFailure {
		t.Fatal("over-broadness: fixture set must include a genuine transport refusal")
	}
	if !sawAuthoredWouldHaveFired {
		t.Fatal("oracle too weak: at least one authored row must be a string ClassifyText would have recorded")
	}

	// Empty / unknown source fail closed — guessing authored→transport is
	// the pollution T455 exists to stop.
	if got := agenterr.ClassifyFrom("", "Internal error"); got != agenterr.ClassNone {
		t.Errorf("empty source classified as %q, want none", got)
	}
	if got := agenterr.ClassifyFrom(agenterr.Source("chat"), "Internal error"); got != agenterr.ClassNone {
		t.Errorf("unknown source classified as %q, want none", got)
	}

	// The pair invariant itself: identical characters cannot classify
	// the same from both sides. A pre-fix ClassifyText-only tree fails
	// this; an always-none classifier fails the transport arm above.
	authored := agenterr.ClassifyFrom(agenterr.SourceAuthored, "Internal error")
	transport := agenterr.ClassifyFrom(agenterr.SourceTransport, "Internal error")
	if authored == transport {
		t.Fatal("source must discriminate; identical refusal string classified the same from both sides")
	}
}

func TestT455ClassifyFromDoesNotBlindDetector(t *testing.T) {
	t.Parallel()
	// 🎯T406 clause 4 / T455 clause 3: a real backend refusal still
	// classifies, with the same class ClassifyText already assigned.
	for _, msg := range []string{
		"Internal error",
		"acp session/prompt: Internal error",
		"429 Too Many Requests",
		"401 Unauthorized",
		"grok acp: no session",
	} {
		want := agenterr.ClassifyText(msg)
		if !want.IsFailure() {
			t.Fatalf("fixture %q is not a failure under ClassifyText; oracle is weak", msg)
		}
		if got := agenterr.ClassifyFrom(agenterr.SourceTransport, msg); got != want {
			t.Errorf("transport %q = %q want %q (class drifted)", msg, got, want)
		}
	}
}

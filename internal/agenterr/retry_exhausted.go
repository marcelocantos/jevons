// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr

import "strings"

// ClassRetryExhausted: the provider/ACP wire itself gave up retrying a
// transient failure and reported that the retry budget ran out — not the
// underlying transient cause (a 500, a timeout, an overload) that first
// triggered the retry (🎯T862.12).
//
// The harness war story this answers: a turn one step from done hit a
// transient hiccup, the CLI's own retry loop tried exactly once more,
// failed again, and gave up — and the resulting error string ("max retries
// exceeded", "retries exhausted") does not contain any of the backend/
// rate-limit/auth markers ClassifyText already knows, so it fell through
// to ClassUnknown: an ambiguous generic failure indistinguishable from a
// wire bug or a genuinely unclassified fault. A budget of one silently
// ate an almost-finished turn and nobody could tell, from the class alone,
// that "it retried and still lost" is what happened.
//
// DefaultRetryBudget documents the number so "a budget of one" is a
// checkable fact, not tribal knowledge: most agent CLIs in this fleet
// (Claude Code, Codex, Grok Build) retry a failed turn-step exactly once
// before surfacing the failure to the operator. That default is not
// configured by jevons — it lives in the harness — but the OUTCOME is
// observable from the wire text, and is classified so 🎯T236 recovery and
// the owner-visible copy both say "retried once, still failed" instead of
// treating it as an opaque unknown.
const ClassRetryExhausted Class = "retry_exhausted"

// DefaultRetryBudget is the number of retry attempts most agent CLIs make
// before giving up on a transient step-level failure and surfacing it to
// the operator, absent an explicit override. Documented here (🎯T862.12)
// so "a budget of one" is a named, checkable constant rather than an
// unnoticed cause of near-complete turn loss.
const DefaultRetryBudget = 1

// retryExhaustedMarkers are the wire/owner-copy phrasings that mean "the
// retry budget itself ran out", as distinct from the transient failure
// that triggered a retry in the first place.
var retryExhaustedMarkers = []string{
	"max retries exceeded",
	"maximum retries exceeded",
	"retries exhausted",
	"retry limit exceeded",
	"retry limit reached",
	"retry budget exhausted",
	"out of retries",
	"exhausted retries",
	"no more retries",
	"giving up after",
	"gave up after",
}

// IsRetryExhausted reports whether msg names the retry budget itself
// running out (checked ahead of the generic backend/timeout markers,
// since the underlying cause — e.g. "timed out" — is often quoted
// alongside the exhaustion phrase and would otherwise mask it as a plain
// backend_unavailable).
func IsRetryExhausted(msg string) bool {
	return containsAny(strings.ToLower(msg), retryExhaustedMarkers...)
}

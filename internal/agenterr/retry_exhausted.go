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
// Retry policy belongs to the selected provider/harness, not this
// classifier. Jevons does not configure a common step retry budget.
// Preserve any reported attempt count in owner copy; never infer one
// from the exhaustion class. See docs/retry-policy.md.
const ClassRetryExhausted Class = "retry_exhausted"

// retryExhaustedMarkers are the wire/owner-copy phrasings that mean "the
// retry budget itself ran out", as distinct from the transient failure
// that triggered a retry in the first place.
var retryExhaustedMarkers = []string{
	"retry_exhausted",
	"retries exceeded",
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

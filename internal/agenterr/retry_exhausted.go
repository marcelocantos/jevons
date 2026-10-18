// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr

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

// IsRetryExhausted reports whether err carries the retry-exhausted cause.
// A sentence that only says the budget ran out does not.
func IsRetryExhausted(err error) bool {
	return ProviderCause(err) == CauseRetryExhausted
}

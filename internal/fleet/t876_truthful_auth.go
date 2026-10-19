// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import "strings"

// preferRehydrateFailure keeps the more informative of two launch refusals
// for one seat (🎯T876).
//
// On 2026-09-27 cl-t130-grok-final and cl-t73-frame-cap stopped after
// Anthropic returned invalid_grant. A later relaunch, while the broker's
// plan Keychain shot was still closed, failed with the generic
// "omp: keychain was not read at startup" and overwrote the recorded
// cause. The owner saw a Keychain error on those seats while other seats
// on working plans stayed up — the real subscription failure was gone.
//
// A specific plan-login refusal (invalid_grant, refresh token, …) is kept
// when a later failure is only a host-level "keychain not open yet /
// broker not ready" message. A success (empty next) still clears. A more
// specific new failure still replaces a generic prior one.
func preferRehydrateFailure(prior, next string) string {
	prior = strings.TrimSpace(prior)
	next = strings.TrimSpace(next)
	if next == "" {
		return prior
	}
	if prior == "" {
		return next
	}
	if isGenericHostAuthFailure(next) && isSpecificPlanAuthFailure(prior) {
		return prior
	}
	return next
}

// isGenericHostAuthFailure is a host/process readiness refusal, not a
// verdict about this seat's subscription token. Claudia surfaces
// "keychain was not read at startup" when OpenOMPPlans has not completed
// (or failed) in this broker process — every Launch then fails the same
// way until the shot is open.
func isGenericHostAuthFailure(s string) bool {
	l := strings.ToLower(s)
	for _, m := range []string{
		"keychain was not read at startup",
		"keychain was not read",
		"keychain not open",
		"plan keychain was not read",
		"broker not listening",
		"broker connection closed",
		"connect: connection refused",
	} {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}

// isSpecificPlanAuthFailure is a subscription login refusal the owner can
// act on (sign in again): invalid_grant, spent refresh token, and friends.
// These outrank a generic host-readiness message for the fleet row.
func isSpecificPlanAuthFailure(s string) bool {
	l := strings.ToLower(s)
	for _, m := range []string{
		"invalid_grant",
		"refresh token",
		"refresh failed",
		"oautherror",
		"authentication_error",
		"invalid_token",
		"token was rejected",
		"login rejected",
		"not logged in",
		"no login",
		"login required",
	} {
		if strings.Contains(l, m) {
			return true
		}
	}
	return false
}

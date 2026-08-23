// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr

import "strings"

// Hard-block detection for 🎯T406.
//
// A provider hard-block is a refusal no retry can fix: a spend limit, a
// revoked key, an account block. It is not an ordinary transient outage
// (Internal error, connection refused) and not a short throttle (429 /
// "rate limit exceeded"). The fleet records that as blocked_provider and
// stands every spawn/nudge/revive control down until a successful call
// proves the provider accepts work again.
//
// Classification and hard-block are separate questions. ClassifyText still
// maps spend-limit prose to rate_limit (so 🎯T407's auth/rate_limit cluster
// keeps seeing it); HardBlock is the narrower predicate that decides
// whether the refusal is a wall.

// HardBlock reports whether class+raw is a provider refusal no retry can
// fix. class must already be a failure class; ClassNone never hard-blocks,
// which is what keeps an over-broad "any error" mutant from passing the
// 🎯T406 oracle.
func HardBlock(class Class, raw string) bool {
	if !class.IsFailure() {
		return false
	}
	low := strings.ToLower(strings.TrimSpace(raw))
	switch class {
	case ClassAuth:
		// Auth failures do not recover by waiting — revoked key, unsigned-in
		// session, forbidden account. Fleet-wide: nothing can work.
		return true
	case ClassRateLimit:
		// Ordinary 429 / "rate limit exceeded" stays transient. Only the
		// account-level spend/billing walls enter the hard-block state.
		return isSpendWall(low)
	default:
		// backend_unavailable, client_bug, unknown: never a hard block on
		// class alone. A spend wall that somehow arrived under another
		// class still counts when the text names it.
		return isSpendWall(low) || isAccountWall(low)
	}
}

// HardBlockReason is a short stable reason for logs and fleet-intent
// provenance when HardBlock is true. Empty when HardBlock is false.
func HardBlockReason(class Class, raw string) string {
	if !HardBlock(class, raw) {
		return ""
	}
	low := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case isSpendWall(low):
		return "provider spend/billing limit"
	case isAccountWall(low):
		return "provider account blocked"
	case class == ClassAuth:
		return "provider auth refusal"
	default:
		return "provider hard-block (" + class.String() + ")"
	}
}

func isSpendWall(low string) bool {
	return containsAny(low, spendWallMarkers...)
}

func isAccountWall(low string) bool {
	return containsAny(low, accountWallMarkers...)
}

// spendWallMarkers are account-level spend/billing refusals. Deliberately
// narrower than rateLimitMarkers: "rate limit" / "429" / "throttl" alone
// must NOT match, or ordinary transient throttle becomes a fleet wall.
var spendWallMarkers = []string{
	"spend limit",
	"monthly spend",
	"hit your monthly",
	"billing limit",
	"billing hard limit",
	"usage limit",
	"out of credits",
	"credit balance",
	"insufficient credits",
	"payment required",
	"402",
	"raise it at",
	"claude.ai/settings/usage",
	"organization spend",
	"workspace spend",
}

// accountWallMarkers are permanent account/key refusals that ClassifyText
// may not yet have mapped to ClassAuth (e.g. "your key has been revoked").
var accountWallMarkers = []string{
	"key has been revoked",
	"api key has been revoked",
	"api key revoked",
	"revoked api key",
	"account has been blocked",
	"account blocked",
	"account has been suspended",
	"account suspended",
	"account has been disabled",
	"account disabled",
	"account locked",
}

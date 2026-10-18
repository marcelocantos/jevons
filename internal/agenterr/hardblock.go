// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr

// Hard-block detection for 🎯T406.
//
// A provider hard-block is a refusal no retry can fix: a spend limit, a
// revoked key, an account block. It is not an ordinary transient outage
// (Internal error, connection refused) and not a short throttle (429 /
// "rate limit exceeded"). The fleet records that as blocked_provider and
// stands every spawn/nudge/revive control down until a successful call
// proves the provider accepts work again.
//
// Classification and hard-block are separate questions. The class arrives
// from a provider-failure cause. HardBlock decides from that class alone.
// Spend-limit English is not a class, and ClassRateLimit never hard-blocks,
// including when the sentence says "monthly spend limit".
//
// HardBlock reports whether class is a provider refusal no retry can fix.
// raw is ignored. class must already be a failure class; ClassNone never
// hard-blocks, which is what keeps an over-broad "any error" mutant from
// passing the 🎯T406 oracle.
func HardBlock(class Class, _ string) bool {
	if !class.IsFailure() {
		return false
	}
	switch class {
	case ClassAuth, ClassSpendWall, ClassAccountWall:
		return true
	default:
		return false
	}
}

// HardBlockReason is a short stable reason for logs and fleet-intent
// provenance when HardBlock is true. Empty when HardBlock is false.
// The sentence is not consulted.
func HardBlockReason(class Class, raw string) string {
	if !HardBlock(class, raw) {
		return ""
	}
	switch class {
	case ClassSpendWall:
		return "provider spend/billing limit"
	case ClassAccountWall:
		return "provider account blocked"
	case ClassAuth:
		return "provider auth refusal"
	default:
		return "provider hard-block (" + class.String() + ")"
	}
}

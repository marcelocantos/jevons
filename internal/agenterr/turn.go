// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr

import "strings"

// TurnKind classifies a completed agent turn for impatience / recovery
// decisions (🎯T454). It is about what the turn *produced*, not about
// whether a send call returned nil.
type TurnKind int

const (
	// TurnEmpty: no terminal text.
	TurnEmpty TurnKind = iota
	// TurnRefusalOnly: the entire output is a provider refusal (spend
	// limit, auth failure, revoked key, bare outage). Not agent work.
	TurnRefusalOnly
	// TurnSubstantive: the agent produced real work. A turn that quotes a
	// refusal while also reporting progress is substantive — ClassifyReply
	// already returns ClassNone when work markers or length say so.
	TurnSubstantive
)

func (k TurnKind) String() string {
	switch k {
	case TurnRefusalOnly:
		return "refusal_only"
	case TurnSubstantive:
		return "substantive"
	default:
		return "empty"
	}
}

// ClassifyTurnOutput maps a finished turn's text to TurnKind (🎯T454).
//
// Refusal-only is ClassifyReply's whole-turn failure reading, plus whole-turn
// spend/auth walls that ClassifyText may not yet map (🎯T406 HardBlock is a
// separate fleet-state question). Anything else non-empty is substantive —
// including a mixed turn that mentions a refusal while reporting progress.
func ClassifyTurnOutput(text string) TurnKind {
	s := strings.TrimSpace(text)
	if s == "" {
		return TurnEmpty
	}
	if ClassifyReply(s).IsFailure() {
		return TurnRefusalOnly
	}
	if wholeTurnProviderWall(s) {
		return TurnRefusalOnly
	}
	return TurnSubstantive
}

// RefusalOnlyTurn reports whether text is nothing but a provider refusal.
func RefusalOnlyTurn(text string) bool {
	return ClassifyTurnOutput(text) == TurnRefusalOnly
}

// wholeTurnProviderWall is the T454-shaped reading of a spend/auth wall when
// it is the entire short reply. It deliberately shares ClassifyReply's
// length / work-marker veto so a long report that quotes a spend limit stays
// substantive.
func wholeTurnProviderWall(s string) bool {
	if len([]rune(s)) > maxReplyFailureChars || countNonEmptyLines(s) > maxReplyFailureLines {
		return false
	}
	low := strings.ToLower(s)
	if containsAny(low, workMarkers...) {
		return false
	}
	return containsAny(low, turnWallMarkers...)
}

// turnWallMarkers are account-level refusals T454 must treat as refusal-only
// even when ClassifyText has not yet mapped them to a failure class. Keep
// aligned with the spend/account walls 🎯T406 names; do not include ordinary
// "rate limit" / "429" alone — those stay ClassifyReply's job.
var turnWallMarkers = []string{
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
	"organization spend",
	"workspace spend",
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

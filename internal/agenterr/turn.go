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
	// TurnRefusalOnly: the entire output is a provider failure ClassifyReply
	// already names (a bare Internal error). A spend limit or a revoked key
	// is a cause, not this reading.
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
// Refusal-only is ClassifyReply's whole-turn failure reading. A spend-limit
// or revoked-key sentence is not a refusal: those are provider-failure
// causes, and HardBlock decides from the class. Anything else non-empty is
// substantive, including a mixed turn that quotes a refusal while reporting
// progress.
func ClassifyTurnOutput(text string) TurnKind {
	s := strings.TrimSpace(text)
	if s == "" {
		return TurnEmpty
	}
	if ClassifyReply(s).IsFailure() {
		return TurnRefusalOnly
	}
	return TurnSubstantive
}

// RefusalOnlyTurn reports whether text is nothing but a provider refusal.
func RefusalOnlyTurn(text string) bool {
	return ClassifyTurnOutput(text) == TurnRefusalOnly
}

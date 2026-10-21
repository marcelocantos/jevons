// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package seatprovider is Jevons' one answer to which provider a seat runs
// on (🎯T1079).
//
// A registry row, a config file or a tool argument names a provider in one
// of two vocabularies: a plan name (claude, codex, grok, cursor), which is
// what the owner picks and what the plan meter and model catalog rank, or a
// launch name (anthropic, openai-codex, xai-oauth, cursor), which is the id
// Claudia's Launch talks to on the Oh My Pi sidecar. Since claudia v0.58
// both reach the same seat, but each decision Jevons made on the raw
// string knew only one vocabulary. The overseer row stored grok while
// workers stored xai-oauth, so the sidecar checks never matched it and a
// restarted overseer read phase_unknown (🎯T1077).
//
// Resolve maps either spelling to one Identity. Every Jevons decision that
// depends on the provider compares Identity fields, never the stored
// string; scripts/docratchet's T1079 ratchet fails on a raw comparison
// anywhere else in cmd/ and internal/.
package seatprovider

import (
	"strings"

	"github.com/marcelocantos/claudia"
)

// Plan is the subscription or backend a seat spends. It is a distinct type
// so a comparison against one can only have come through Resolve.
type Plan string

// The plans Jevons recognises. Unset is an empty stored provider: the
// caller decides what that means (the daemon default, usually), so Resolve
// does not guess.
const (
	Unset   Plan = ""
	Claude  Plan = "claude"
	Codex   Plan = "codex"
	Grok    Plan = "grok"
	Cursor  Plan = "cursor"
	Bedrock Plan = "bedrock"
	Ollama  Plan = "ollama"
)

// AsProvider is the plan as Claudia's catalog and plan meter name it.
func (p Plan) AsProvider() claudia.Provider { return claudia.Provider(p) }

// String is the plan id.
func (p Plan) String() string { return string(p) }

// Identity is what a stored provider name means.
type Identity struct {
	// Plan is the subscription the seat spends. An id Jevons does not
	// recognise passes through lowercased, so two spellings of an unknown
	// id still compare equal and a future backend is not blocked here.
	Plan Plan
	// Launch is the id Claudia's Launch talks to: anthropic, openai-codex,
	// xai-oauth or cursor for the subscription plans; bedrock and ollama
	// as themselves; an unknown id as given (trimmed).
	Launch claudia.Provider
	// Sidecar reports that Launch reaches the Oh My Pi sidecar, whose seats
	// keep their records in the dated spool rather than a vendor transcript.
	Sidecar bool
	// Known reports that the name is one of the plans above, in either
	// vocabulary.
	Known bool
}

// Resolve maps a stored or configured provider name, in either vocabulary
// and any case, to the seat's identity. It is the only place Jevons reads
// a provider string's meaning.
func Resolve(p claudia.Provider) Identity {
	raw := strings.TrimSpace(string(p))
	switch strings.ToLower(raw) {
	case "":
		return Identity{}
	case "claude", "anthropic":
		return Identity{Plan: Claude, Launch: "anthropic", Sidecar: true, Known: true}
	case "codex", "openai-codex":
		return Identity{Plan: Codex, Launch: "openai-codex", Sidecar: true, Known: true}
	case "grok", "xai-oauth", "xai":
		return Identity{Plan: Grok, Launch: "xai-oauth", Sidecar: true, Known: true}
	case "cursor":
		return Identity{Plan: Cursor, Launch: "cursor", Sidecar: true, Known: true}
	case "bedrock":
		return Identity{Plan: Bedrock, Launch: claudia.ProviderBedrock, Known: true}
	case "ollama":
		return Identity{Plan: Ollama, Launch: claudia.ProviderOllama, Known: true}
	default:
		return Identity{Plan: Plan(strings.ToLower(raw)), Launch: claudia.Provider(raw)}
	}
}

// Same reports whether two stored names are the same plan. Two unset names
// are the same; unset and a named plan are not.
func Same(a, b claudia.Provider) bool { return Resolve(a).Plan == Resolve(b).Plan }

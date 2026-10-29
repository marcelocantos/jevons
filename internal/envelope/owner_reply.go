// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import (
	"strings"

	"github.com/marcelocantos/jevons/internal/silentresponse"
)

// OwnerPurpose is the typed purpose slot on owner-reply (🎯T1140).
type OwnerPurpose string

const (
	OwnerPurposeNone       OwnerPurpose = ""
	OwnerPurposeDecision   OwnerPurpose = "decision"
	OwnerPurposeFault      OwnerPurpose = "fault"
	OwnerPurposePlanChange OwnerPurpose = "plan-change"
	OwnerPurposeAnswer     OwnerPurpose = "answer"
	OwnerPurposeRoutine    OwnerPurpose = "routine"
)

var allOwnerPurposes = []OwnerPurpose{
	OwnerPurposeDecision,
	OwnerPurposeFault,
	OwnerPurposePlanChange,
	OwnerPurposeAnswer,
	OwnerPurposeRoutine,
}

// ParseOwnerPurpose maps a slot value onto OwnerPurpose. Unknown names fail.
func ParseOwnerPurpose(raw string) (OwnerPurpose, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.Join(strings.Fields(s), "-")
	if s == "" {
		return OwnerPurposeNone, true
	}
	p := OwnerPurpose(s)
	for _, known := range allOwnerPurposes {
		if p == known {
			return p, true
		}
	}
	return "", false
}

// OwnerFacing is true when the purpose slot says the owner must see this
// (decision, fault, or change in what they would do next).
func (p OwnerPurpose) OwnerFacing() bool {
	switch p {
	case OwnerPurposeDecision, OwnerPurposeFault, OwnerPurposePlanChange, OwnerPurposeAnswer:
		return true
	default:
		return false
	}
}

// OverseerOwnerStreamClass is the progressive classification of an
// accumulating overseer assistant stream for the owner chat wire (🎯T1140).
type OverseerOwnerStreamClass int

const (
	// OverseerOwnerPending — not enough text yet (incomplete fence or turn).
	OverseerOwnerPending OverseerOwnerStreamClass = iota
	// OverseerOwnerSuppress — routine / undeclared / explicit routine purpose.
	OverseerOwnerSuppress
	// OverseerOwnerShow — owner-reply with an owner-facing purpose slot.
	OverseerOwnerShow
)

// ClassifyOverseerOwnerStream classifies accumulated overseer assistant text.
// terminal marks the final fragment of the turn.
func ClassifyOverseerOwnerStream(acc string, terminal bool) OverseerOwnerStreamClass {
	if silentresponse.Is(acc) {
		return OverseerOwnerSuppress
	}
	body, _ := StripPrefixes(acc)
	body = strings.TrimLeft(body, " \t\r")
	if body == "" {
		if terminal {
			return OverseerOwnerSuppress
		}
		return OverseerOwnerPending
	}
	trim := strings.TrimLeft(body, " \t")
	if strings.HasPrefix(trim, "```") {
		fence, payload, ok := splitFence(body)
		if !ok {
			if terminal {
				return OverseerOwnerSuppress
			}
			return OverseerOwnerPending
		}
		msg, err := parseSlots(fence)
		if msg == nil {
			if terminal {
				return OverseerOwnerSuppress
			}
			return OverseerOwnerPending
		}
		if msg.Kind != KindOwnerReply {
			if terminal {
				return OverseerOwnerSuppress
			}
			return OverseerOwnerPending
		}
		if err != nil {
			if terminal {
				return OverseerOwnerSuppress
			}
			return OverseerOwnerPending
		}
		if !msg.OwnerPurpose.OwnerFacing() {
			return OverseerOwnerSuppress
		}
		if !terminal {
			return OverseerOwnerPending
		}
		if strings.TrimSpace(payload) == "" {
			return OverseerOwnerSuppress
		}
		return OverseerOwnerShow
	}
	if !terminal {
		return OverseerOwnerPending
	}
	// Unenveloped overseer prose at seal is routine (🎯T1140).
	return OverseerOwnerSuppress
}

// OverseerOwnerChatBody returns the owner-visible prose for an overseer turn.
// Valid owner-reply envelopes surface Payload only; everything else is unchanged.
func OverseerOwnerChatBody(text string) string {
	msg, err := Parse(text)
	if msg != nil && err == nil && msg.Kind == KindOwnerReply && strings.TrimSpace(msg.Payload) != "" {
		return msg.Payload
	}
	return text
}

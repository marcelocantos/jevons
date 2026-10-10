// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package ownercomms defines the evidence contract for overseer-to-owner
// communication. It does not infer facts from arbitrary assistant prose: a
// caller must establish the facts before using this policy.
package ownercomms

import "github.com/marcelocantos/jevons/internal/silentresponse"

// Category is the reason a candidate is (or is not) owner-facing.
type Category string

const (
	OwnerDecision Category = "owner_decision"
	NewAnomaly    Category = "new_anomaly"
	DirectAnswer  Category = "direct_answer"
	AskedStatus   Category = "asked_status"
	Routine       Category = "routine"
)

// Evidence describes the current turn, not merely keywords in its proposed
// text. A caller must check whether a decision really is owner-only and an
// anomaly really is material, new, uncontained and not already communicated.
// DirectOwnerQuestion and OwnerRequestedStatus mean the request is STILL
// unanswered from a genuine owner [user] turn, not an overseer-to-PO
// question or a candidate claiming "this answers what I asked". Clear them
// once a substantive response was emitted, including
// in an interim toolUse fragment of the same owner turn.
// DeliveredIDs are issue/event identities already communicated to the owner;
// do not use prose equality as the dedup key.
type Evidence struct {
	OwnerOnlyDecision             bool
	MaterialNewUncontainedAnomaly bool
	AnomalyID                     string
	DirectOwnerQuestion           bool
	OwnerRequestedStatus          bool
	DeliveredIDs                  map[string]bool
}

// Classify applies precedence to the independent reasons to send. An
// already-communicated anomaly cannot suppress a direct answer or an owner
// decision in the same turn. Routine work and duplicate reports are silent.
func Classify(e Evidence) Category {
	if e.OwnerOnlyDecision {
		return OwnerDecision
	}
	if e.MaterialNewUncontainedAnomaly && e.AnomalyID != "" && !e.DeliveredIDs[e.AnomalyID] {
		return NewAnomaly
	}
	if e.DirectOwnerQuestion {
		return DirectAnswer
	}
	if e.OwnerRequestedStatus {
		return AskedStatus
	}
	return Routine
}

func (c Category) Send() bool {
	switch c {
	case OwnerDecision, NewAnomaly, DirectAnswer, AskedStatus:
		return true
	default:
		return false
	}
}

// Response is the wire-compatible output of the policy for an established
// classification. [silent] is suppressed by the existing whole-stream chat
// filter, including a terminal stop. No acknowledgment is emitted for Routine.
// A caller must not pass an owner-facing draft for Routine; the draft is
// intentionally discarded. This does NOT classify untagged model output.
func Response(c Category, draft string) string {
	if !c.Send() {
		return silentresponse.Prefix
	}
	return draft
}

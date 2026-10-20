// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import (
	"errors"
	"strings"
)

// SlotError is a slot line whose value the schema does not accept
// (🎯T1083). It names the slot and, for a closed vocabulary, every value
// the slot does accept, so the author's correction notice can say exactly
// what to resend instead of leaving the worker to guess a second synonym.
//
// The parser does not read a near-miss as its intended value: `status done`
// is not the absence of status, whatever the English suggests (🎯T766.4).
type SlotError struct {
	// Slot is the key as the author wrote it (after the jevons: sigil).
	Slot string
	// Value is the value as the author wrote it.
	Value string
	// Accepted lists the closed vocabulary, or is nil when the slot's
	// value is structured rather than enumerated (see Err).
	Accepted []string
	// Hint is one sentence of schema guidance for this slot, or "".
	Hint string
	// Err is the underlying parse error for a structured slot. When set,
	// Error() is Err's message unchanged.
	Err error
}

func (e *SlotError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	var b strings.Builder
	b.WriteString("unknown ")
	b.WriteString(e.Slot)
	b.WriteString(` "`)
	b.WriteString(e.Value)
	b.WriteString(`"`)
	if len(e.Accepted) > 0 {
		b.WriteString(" (accepted: ")
		b.WriteString(strings.Join(e.Accepted, " | "))
		b.WriteString(")")
	}
	if e.Hint != "" {
		b.WriteString("; ")
		b.WriteString(e.Hint)
	}
	return b.String()
}

func (e *SlotError) Unwrap() error { return e.Err }

// AsSlotError returns the SlotError inside err, if there is one.
func AsSlotError(err error) (*SlotError, bool) {
	var se *SlotError
	if errors.As(err, &se) {
		return se, true
	}
	return nil, false
}

// statusHint is what the author of a status slot outside the enum needs:
// a finished report has no status of its own to declare.
const statusHint = "a finish-report that is done omits status — in-progress and blocked are not done, and live, landed and shipped need owner-visible product evidence (🎯T176)"

func statusSlotError(value string) *SlotError {
	accepted := make([]string, len(allProgress))
	for i, p := range allProgress {
		accepted[i] = string(p)
	}
	return &SlotError{Slot: "status", Value: value, Accepted: accepted, Hint: statusHint}
}

func kindSlotError(value string) *SlotError {
	accepted := make([]string, len(allKinds))
	for i, k := range allKinds {
		accepted[i] = string(k)
	}
	return &SlotError{Slot: "kind", Value: value, Accepted: accepted}
}

func verdictSlotError(value string) *SlotError {
	accepted := make([]string, len(allVerdicts))
	for i, v := range allVerdicts {
		accepted[i] = string(v)
	}
	return &SlotError{Slot: "verdict", Value: value, Accepted: accepted}
}

func riskSlotError(value string) *SlotError {
	accepted := make([]string, len(allRisks))
	for i, r := range allRisks {
		accepted[i] = string(r)
	}
	return &SlotError{Slot: "risk", Value: value, Accepted: accepted}
}

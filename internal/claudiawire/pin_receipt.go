// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

//go:build !jevons_claudia_next

package claudiawire

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ReceiptState is a fact recorded for one logical request, not a transport ACK.
// Copied from Claudia omp at 2b10c121. Published v0.53.0 does not export it.
type ReceiptState string

const (
	ReceiptAccepted  ReceiptState = "accepted"
	ReceiptStarted   ReceiptState = "started"
	ReceiptCompleted ReceiptState = "completed"
	ReceiptCancelled ReceiptState = "cancelled"
	ReceiptFailed    ReceiptState = "failed"
	ReceiptUnknown   ReceiptState = "unknown"
)

// ReceiptCause is the typed execution outcome on a receipt.
type ReceiptCause string

const (
	ReceiptCauseNone         ReceiptCause = ""
	ReceiptCauseAuth         ReceiptCause = "auth"
	ReceiptCauseCapacity     ReceiptCause = "capacity"
	ReceiptCauseCancellation ReceiptCause = "cancellation"
	ReceiptCauseFailure      ReceiptCause = "failure"
	ReceiptCauseUnknown      ReceiptCause = "unknown"
)

// RequestReceipt identifies an obligation within one durable seat generation.
type RequestReceipt struct {
	RequestID  string       `json:"request_id"`
	SessionID  string       `json:"session_id"`
	Seat       string       `json:"seat"`
	State      ReceiptState `json:"state"`
	AttemptID  string       `json:"attempt_id"`
	TurnIDs    []string     `json:"turn_ids"`
	Cause      ReceiptCause `json:"cause,omitempty"`
	Reason     string       `json:"reason,omitempty"`
	AcceptedAt string       `json:"accepted_at,omitempty"`
	UpdatedAt  string       `json:"updated_at,omitempty"`
}

// ReceiptQuery names one durable receipt. The published pin cannot read it.
type ReceiptQuery struct {
	Seat      string
	SessionID string
	RequestID string
}

// QueryReceipt fails closed. Published claudia v0.53.0 has no receipt read.
// The jevons_claudia_next build calls Claudia.
func QueryReceipt(context.Context, ReceiptQuery) (*RequestReceipt, error) {
	return nil, fmt.Errorf("receipt query unavailable: published claudia v0.53.0 has no receipt API")
}

// ValidateIdentity refuses incomplete or foreign receipts before a caller can
// use them as evidence. Copied from Claudia omp at 2b10c121.
func (r *RequestReceipt) ValidateIdentity(seat, session, request string) error {
	if r == nil || r.Seat != seat || r.SessionID != session || r.RequestID != request {
		return fmt.Errorf("receipt identity mismatch for seat %q session %q request %q", seat, session, request)
	}
	switch r.State {
	case ReceiptUnknown:
		return validateReceiptCause(r.State, r.Cause)
	case ReceiptAccepted, ReceiptStarted, ReceiptCompleted, ReceiptCancelled, ReceiptFailed:
	default:
		return fmt.Errorf("invalid receipt state %q", r.State)
	}
	accepted, aerr := time.Parse(time.RFC3339Nano, r.AcceptedAt)
	updated, uerr := time.Parse(time.RFC3339Nano, r.UpdatedAt)
	if strings.TrimSpace(r.AttemptID) == "" || aerr != nil || uerr != nil || updated.Before(accepted) || r.TurnIDs == nil {
		return fmt.Errorf("incomplete durable receipt for request %q", request)
	}
	seen := map[string]bool{}
	for _, id := range r.TurnIDs {
		if strings.TrimSpace(id) == "" || seen[id] {
			return fmt.Errorf("invalid receipt turn identity")
		}
		seen[id] = true
	}
	if (r.State == ReceiptAccepted) != (len(r.TurnIDs) == 0) {
		return fmt.Errorf("receipt state and turn identities disagree")
	}
	if err := validateReceiptCause(r.State, r.Cause); err != nil {
		return err
	}
	return nil
}

func validateReceiptCause(state ReceiptState, cause ReceiptCause) error {
	switch cause {
	case ReceiptCauseNone, ReceiptCauseAuth, ReceiptCauseCapacity, ReceiptCauseCancellation, ReceiptCauseFailure, ReceiptCauseUnknown:
	default:
		return fmt.Errorf("invalid receipt cause %q", cause)
	}
	switch state {
	case ReceiptAccepted, ReceiptStarted, ReceiptCompleted:
		if cause != ReceiptCauseNone {
			return fmt.Errorf("receipt cause %q on state %s", cause, state)
		}
	case ReceiptCancelled:
		if cause != ReceiptCauseNone && cause != ReceiptCauseCancellation {
			return fmt.Errorf("receipt cause %q on state %s", cause, state)
		}
	case ReceiptFailed:
		if cause != ReceiptCauseNone && cause != ReceiptCauseAuth && cause != ReceiptCauseCapacity && cause != ReceiptCauseFailure {
			return fmt.Errorf("receipt cause %q on state %s", cause, state)
		}
	case ReceiptUnknown:
		if cause != ReceiptCauseNone && cause != ReceiptCauseUnknown {
			return fmt.Errorf("receipt cause %q on state %s", cause, state)
		}
	}
	return nil
}

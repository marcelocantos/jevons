// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sendq

import "fmt"

// Phase is the one reading of a sendq entry (🎯T766.5).
//
// DeliveryState on disk is unchanged. Six historical readings of the word
// "pending" are phases of this machine, not six queues and not six renamed
// JSON fields:
//
//  1. admitted — State is empty, there is no digest, and no operator has
//     requeued it. Accepted, and not claimed.
//  2. digest — State is empty and Members is set. One admitted obligation.
//     The originals live in the digest archive.
//  3. in_flight — State is attempting and this store owns AttemptID.
//  4. held_attempt — State is attempting and this store does not own it.
//     A dead daemon left it. It is not offered again.
//  5. uncertain — State is uncertain. Held until an attempt result or an
//     operator reconciliation. It is not offered again.
//  6. requeued — State is empty and Reconciled is set. The operator sent it
//     back to pending. The record is history on that same state.
type Phase string

const (
	PhaseAdmitted    Phase = "admitted"
	PhaseDigest      Phase = "digest"
	PhaseInFlight    Phase = "in_flight"
	PhaseHeldAttempt Phase = "held_attempt"
	PhaseUncertain   Phase = "uncertain"
	PhaseRequeued    Phase = "requeued"
)

// EntryPhase reads one entry. activeAttempt is the attempt id this store
// owns for the seat, or empty when it owns none. An empty DeliveryState is
// admitted, digest, or requeued. Those three are the claimable pending.
func EntryPhase(e Entry, activeAttempt string) Phase {
	switch e.State {
	case Attempting:
		if activeAttempt != "" && e.AttemptID == activeAttempt {
			return PhaseInFlight
		}
		return PhaseHeldAttempt
	case Uncertain:
		return PhaseUncertain
	case Pending:
		if e.Members != "" {
			return PhaseDigest
		}
		if e.Reconciled != nil {
			return PhaseRequeued
		}
		return PhaseAdmitted
	default:
		return ""
	}
}

// claimablePhase is the one pending meaning: the drain may take the entry.
func claimablePhase(p Phase) bool {
	return p == PhaseAdmitted || p == PhaseDigest || p == PhaseRequeued
}

// blockReason is the authority's reason a queue is not only waiting.
// Claimable pending has an empty reason.
func blockReason(p Phase) string {
	switch p {
	case PhaseInFlight:
		return "in flight"
	case PhaseHeldAttempt:
		return "held attempt"
	case PhaseUncertain:
		return "uncertain"
	default:
		return ""
	}
}

type deliveryStep int

const (
	stepClaim deliveryStep = iota
	stepNotSent
	stepUnverified
	stepConfirm
	stepTerminal
	stepRequeue
	stepReconcileRemove
)

// applyDelivery is the one writer of DeliveryState. Claim, resolve, digest
// claim, and reconcile all go through it. remove reports that the entry
// leaves the queue; the caller keeps the payload for the receipt.
func applyDelivery(e Entry, step deliveryStep, detail, attemptID string, by *Reconciliation) (Entry, bool, error) {
	switch step {
	case stepClaim:
		if e.State != Pending {
			return e, false, fmt.Errorf("sendq: claim requires a pending entry")
		}
		if attemptID == "" {
			return e, false, fmt.Errorf("sendq: claim requires an attempt id")
		}
		e.State, e.AttemptID, e.Detail = Attempting, attemptID, ""
		return e, false, nil
	case stepNotSent:
		if e.State == Pending {
			return e, false, fmt.Errorf("sendq: not-sent requires an attempt")
		}
		e.State, e.AttemptID, e.Detail = Pending, "", detail
		return e, false, nil
	case stepUnverified:
		if e.State == Pending {
			return e, false, fmt.Errorf("sendq: unverified requires an attempt")
		}
		e.State, e.Detail = Uncertain, detail
		return e, false, nil
	case stepConfirm, stepTerminal:
		if e.State == Pending {
			return e, false, fmt.Errorf("sendq: resolve requires an attempt")
		}
		return e, true, nil
	case stepRequeue:
		if e.State == Pending {
			return e, false, fmt.Errorf("sendq: entry %s is already pending; nothing to requeue", e.ID)
		}
		if by == nil {
			return e, false, fmt.Errorf("sendq: requeue requires an operator record")
		}
		e.State, e.AttemptID, e.Detail = Pending, "", ""
		recorded := *by
		e.Reconciled = &recorded
		return e, false, nil
	case stepReconcileRemove:
		if by == nil {
			return e, false, fmt.Errorf("sendq: reconcile requires an operator record")
		}
		return e, true, nil
	default:
		return e, false, fmt.Errorf("sendq: unknown delivery step")
	}
}

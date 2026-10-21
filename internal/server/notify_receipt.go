// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"log/slog"
	"slices"
	"time"
)

// Fleet-note receipts on the overseer notify queue.
//
// The drain hands every batch to the overseer with a request id (sendNotes
// mints fn- for a fleet batch), and Claudia records a receipt against it. An
// owner batch's receipt was read (noteOwnerDeliveredFromReceipt, 🎯T1081). A
// fleet batch's never was: every agent-origin send to the overseer — a
// worker report, a sentinel notice, a 🎯T902 mid-turn answer relay — reported
// delivered_unconfirmed and nothing later settled it, on the development
// daemon and in isolate J38 alike. This file reads that receipt once per
// batch, logs the outcome for owner and fleet batches alike, and lets the
// caller whose own drain sent the batch wait for it.

// sentNotifyBatch is one fleet batch the drain handed to the overseer.
type sentNotifyBatch struct {
	requestID string
	notes     []string
	done      chan struct{}
	settled   bool // valid once done is closed
}

// carries reports whether text went out in this batch as one of its notes.
func (b *sentNotifyBatch) carries(text string) bool {
	return b != nil && slices.Contains(b.notes, text)
}

// fleetReceiptCallerWait bounds how long a sender waits for its note's batch
// receipt. A receipt normally follows the handoff within milliseconds; the
// read itself goes on to its own deadline and logs the outcome either way, so
// a caller is never held for a receipt that is not coming.
const fleetReceiptCallerWait = 3 * time.Second

// waitFor returns whether the batch's receipt settled within d.
func (b *sentNotifyBatch) waitFor(d time.Duration) bool {
	if b == nil {
		return false
	}
	select {
	case <-b.done:
		return b.settled
	case <-time.After(d):
		return false
	}
}

// settleFleetBatchReceipt starts the one receipt read for a fleet batch that
// was just handed off. It runs off the drain so the queue never waits on it.
func (s *Server) settleFleetBatchReceipt(requestID string, batch []string) *sentNotifyBatch {
	sent := &sentNotifyBatch{
		requestID: requestID,
		notes:     append([]string(nil), batch...),
		done:      make(chan struct{}),
	}
	s.mu.RLock()
	seat := s.overseerName
	s.mu.RUnlock()
	go func() {
		sent.settled = s.namedSendReceiptSettles(seat, requestID)
		close(sent.done)
		logNotifyReceipt(requestID, false, len(batch), sent.settled)
	}()
	return sent
}

// logNotifyReceipt records how a handed-off batch's receipt read came out.
// settled=false is an unconfirmed handoff, not a failed one.
func logNotifyReceipt(requestID string, owner bool, notes int, settled bool) {
	slog.Info("notify_queue",
		"component", "notify_queue",
		"decision", "receipt",
		"request_id", requestID,
		"owner_batch", owner,
		"notes", notes,
		"settled", settled,
	)
}

// DeliverToOverseerAsReceipted is DeliverToOverseerAs that also reports
// whether a structured receipt settled the handoff. Only an agent-origin note
// that this call's own drain handed off is waited for; a note left queued
// behind a busy overseer is settled by the drain that sends it, later, and
// an owner turn's receipt stays with the owner-delivery path.
func (s *Server) DeliverToOverseerAsReceipted(text, origin string) (bool, error) {
	if origin != sendOriginAgent {
		return false, s.sendToOverseerAsOwner(text)
	}
	sent, err := s.sendToOverseerAs(text, "")
	if err != nil || !sent.carries(text) {
		return false, err
	}
	return sent.waitFor(fleetReceiptCallerWait), nil
}

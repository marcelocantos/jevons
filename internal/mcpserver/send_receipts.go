// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/sendq"
)

// One unavailable receipt service must not cost a full RPC timeout for every
// held entry or seat. Sweep and drain each share one deadline across their pass.
const heldReceiptQueryTimeout = 10 * time.Second

// reconcileHeldReceipts settles transport handoffs, never mission acceptance.
// No live registry or current seat generation participates in the lookup.
// Unbound historical attempts remain held: their text and today's session
// cannot establish which logical input the old transport actually received.
func (s *Server) reconcileHeldReceipts(ctx context.Context, name string) int {
	q := s.sendQueue()
	entries, err := q.Snapshot(name)
	if err != nil {
		slog.Warn("receipt recovery cannot read held queue", "agent", name, "error", err)
		return 0
	}
	s.mu.Lock()
	query, journal := s.querySendReceipt, s.eventJournal
	s.mu.Unlock()
	if query == nil {
		query = claudia.QueryReceipt
	}
	settled := 0
	for _, e := range entries {
		if e.State == sendq.Pending || e.TransportRequestID == "" || e.SessionID == "" {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		r, err := query(ctx, claudia.ReceiptQuery{Seat: name, SessionID: e.SessionID, RequestID: e.TransportRequestID})
		if err == nil {
			err = r.ValidateIdentity(name, e.SessionID, e.TransportRequestID)
		}
		if err != nil {
			slog.Debug("held delivery receipt unavailable; retaining obligation", "agent", name, "entry_id", e.ID, "error", err)
			continue
		}
		switch r.State {
		case claudia.ReceiptAccepted, claudia.ReceiptStarted, claudia.ReceiptCompleted, claudia.ReceiptCancelled, claudia.ReceiptFailed:
			// Each is proof the transport accepted this exact input. Failed or
			// cancelled execution is retained as such, not called completed work.
		default:
			continue
		}
		// LogEvent is intentionally best effort; receipt evidence is not.
		// Append to the same product journal and require its fsync before
		// removing the queue's only mapping to the original request. A crash
		// after append can duplicate evidence, but cannot lose the failure or
		// cancellation reason. This event records observation, not removal:
		// the CAS below may still reject a stale or locally owned attempt.
		err = journal.Append(eventlog.Event{
			Component: "agent_send", Decision: "sendq_receipt_observed",
			Msg: "Durable receipt proves transport acceptance; mission outcome is separate",
			Fields: map[string]any{
				"agent": name, "entry_id": e.ID, "attempt_id": e.AttemptID,
				"owner_request_id": e.RequestID, "transport_request_id": e.TransportRequestID,
				"session_id": e.SessionID, "receipt": r,
				"transport_accepted": true, "text": e.Text, "enqueued_at": e.EnqueuedAt,
			},
		})
		if err != nil {
			slog.Warn("held receipt evidence could not be persisted; retaining obligation", "agent", name, "entry_id", e.ID, "error", err)
			continue
		}
		detail := fmt.Sprintf("Claudia durable receipt request=%q session=%q state=%s reason=%q; transport accepted, mission outcome separate", r.RequestID, r.SessionID, r.State, r.Reason)
		if _, err := q.Reconcile(name, e.ID, e.AttemptID, sendq.ReconcileConfirmed, "daemon:receipt", detail, time.Now()); err != nil {
			slog.Debug("receipt observed but held attempt changed or remains locally owned", "agent", name, "entry_id", e.ID, "error", err)
			continue
		}
		settled++
	}
	if settled > 0 {
		if _, blocked, err := q.BlockedHead(name); err == nil && !blocked {
			s.clearSendqPin(name)
		}
	}
	return settled
}

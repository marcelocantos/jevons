// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/claudiawire"
	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/sendq"
)

// errHandoffQueued means the message was admitted behind earlier queue work
// and must not be handed to the process on this call.
var errHandoffQueued = errors.New("handoff admitted behind earlier queue work")

// SetRequestMissionRecorder installs the host write that freezes a seat
// target onto a transport request at receipt bind.
func (s *Server) SetRequestMissionRecorder(fn func(ctx context.Context, seat, requestID, mission string) error) {
	s.mu.Lock()
	s.recordRequestMission = fn
	s.mu.Unlock()
}

// receiptBind reports whether this handoff must be bound before send.
// A process that cannot carry a host request id keeps the historical send:
// binding would mint an id that transport would drop. A process that can
// carry one must already have a registry generation. No registry, no seat,
// or an empty session is the same refusal: nothing is sent.
func (s *Server) receiptBind(name string, proc agentSender) (sessionID, mission string, bind bool, err error) {
	if s == nil || !supportsRequestID(proc) {
		return "", "", false, nil
	}
	var d *claudia.AgentDef
	if s.registry != nil {
		d = s.registry.Def(name)
	}
	if d == nil || strings.TrimSpace(d.SessionID) == "" {
		return "", "", false, fmt.Errorf("agent %q has no receiving generation", name)
	}
	return strings.TrimSpace(d.SessionID), strings.TrimSpace(d.TargetID), true, nil
}

func (s *Server) freezeRequestMission(seat, requestID, mission string) error {
	s.mu.Lock()
	record := s.recordRequestMission
	s.mu.Unlock()
	if record == nil {
		return nil
	}
	return record(context.Background(), seat, requestID, mission)
}

// bindOwnedAttempt binds the current locally owned attempt and freezes its
// mission before any transport write. The returned entry is what the send
// and the later Resolve must use.
func (s *Server) bindOwnedAttempt(name string, attempt sendq.Entry, wire, sessionID, mission string) (sendq.Entry, error) {
	bound, err := s.sendQueue().BindReceiptPayload(name, attempt, sessionID, wire)
	if err != nil {
		return sendq.Entry{}, err
	}
	if err := s.freezeRequestMission(name, bound.TransportRequestID, mission); err != nil {
		return sendq.Entry{}, err
	}
	return bound, nil
}

// claimAndBindHandoff admits text and binds it in one step for a send that
// would otherwise skip the queue. On failure no Attempting entry remains.
func (s *Server) claimAndBindHandoff(name, text, requestID string, urgent bool, sessionID, mission string) (sendq.Entry, error) {
	var (
		entry   sendq.Entry
		claimed bool
		err     error
	)
	if urgent {
		entry, _, claimed, err = s.sendQueue().AppendAndClaimUrgent(name, text, requestID, time.Now())
	} else {
		entry, _, claimed, err = s.sendQueue().AppendAndClaimWithRequestID(name, text, requestID, time.Now())
	}
	if err != nil {
		return sendq.Entry{}, err
	}
	if !claimed {
		return sendq.Entry{}, errHandoffQueued
	}
	bound, err := s.bindOwnedAttempt(name, entry, text, sessionID, mission)
	if err != nil {
		_ = s.sendQueue().Resolve(name, entry, sendq.TerminalUndelivered, err.Error())
		return sendq.Entry{}, err
	}
	return bound, nil
}

func (s *Server) settleHandoff(name string, handoff sendq.Entry, outcome sendq.AttemptOutcome, detail string) {
	if handoff.ID == "" {
		return
	}
	if err := s.sendQueue().Resolve(name, handoff, outcome, detail); err != nil {
		slog.Error("agent send: receipt handoff could not be settled", "name", name, "entry_id", handoff.ID, "err", err)
	}
}

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
	settled := 0
	for _, e := range entries {
		if e.State == sendq.Pending || e.TransportRequestID == "" || e.SessionID == "" {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		r, err := s.LookupHeldReceipt(ctx, claudiawire.ReceiptQuery{Seat: name, SessionID: e.SessionID, RequestID: e.TransportRequestID})
		if err == nil {
			err = r.ValidateIdentity(name, e.SessionID, e.TransportRequestID)
		}
		if err != nil {
			slog.Debug("held delivery receipt unavailable; retaining obligation", "agent", name, "entry_id", e.ID, "error", err)
			continue
		}
		if !receiptStateSettles(r.State) {
			// Unknown includes a started dispatch restored as interrupted.
			// That attempt keeps its transport id and is not sent again.
			continue
		}
		// LogEvent is intentionally best effort; receipt evidence is not.
		// Append to the same product journal and require its fsync before
		// removing the queue's only mapping to the original request. A crash
		// after append can duplicate evidence, but cannot lose the failure or
		// cancellation reason. This event records observation, not removal:
		// the CAS below may still reject a stale or locally owned attempt.
		err = s.persistSendReceipt(name, e, r)
		if err != nil {
			slog.Warn("held receipt evidence could not be persisted; retaining obligation", "agent", name, "entry_id", e.ID, "error", err)
			continue
		}
		detail := receiptSettlementDetail(r)
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

// SetSendReceiptQuery installs the production receipt read. Nil falls back
// to claudiawire.QueryReceipt. The published pin fails that read closed.
// jevons_claudia_next dials Claudia. The daemon installs the replica read.
func (s *Server) SetSendReceiptQuery(fn func(context.Context, claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error)) {
	s.mu.Lock()
	s.querySendReceipt = fn
	s.mu.Unlock()
}

// LookupHeldReceipt reads one receipt the way the held-receipt sweep does.
func (s *Server) LookupHeldReceipt(ctx context.Context, q claudiawire.ReceiptQuery) (*claudiawire.RequestReceipt, error) {
	s.mu.Lock()
	query := s.querySendReceipt
	s.mu.Unlock()
	if query == nil {
		query = claudiawire.QueryReceipt
	}
	return query(ctx, q)
}

func (s *Server) persistSendReceipt(name string, e sendq.Entry, r *claudiawire.RequestReceipt) error {
	s.mu.Lock()
	journal := s.eventJournal
	s.mu.Unlock()
	if journal == nil {
		return fmt.Errorf("send receipt journal is not configured")
	}
	return journal.Append(eventlog.Event{
		Component: "agent_send", Decision: "sendq_receipt_observed",
		Msg: "Durable receipt proves transport acceptance; mission outcome is separate",
		Fields: map[string]any{
			"agent": name, "entry_id": e.ID, "attempt_id": e.AttemptID,
			"owner_request_id": e.RequestID, "transport_request_id": e.TransportRequestID,
			"session_id": e.SessionID, "receipt": r, "receipt_cause": receiptExecutionCause(r),
			"transport_accepted": true, "text": e.Text, "wire_text": e.WirePayload(), "enqueued_at": e.EnqueuedAt,
		},
	})
}

// receiptStateSettles is the transport-acceptance fact. Failed and cancelled
// execution still settle the obligation; they are not mission completion.
// Unknown does not settle and must not be retried from transcript text.
func receiptStateSettles(state claudiawire.ReceiptState) bool {
	switch state {
	case claudiawire.ReceiptAccepted, claudiawire.ReceiptStarted, claudiawire.ReceiptCompleted, claudiawire.ReceiptCancelled, claudiawire.ReceiptFailed:
		return true
	default:
		return false
	}
}

// receiptExecutionCause is the typed cause on the receipt. Reason is rendered
// provider text and is not a cause, even when it contains words such as auth.
func receiptExecutionCause(r *claudiawire.RequestReceipt) string {
	if r == nil {
		return ""
	}
	switch r.Cause {
	case claudiawire.ReceiptCauseAuth, claudiawire.ReceiptCauseCapacity, claudiawire.ReceiptCauseCancellation, claudiawire.ReceiptCauseFailure, claudiawire.ReceiptCauseUnknown:
		return string(r.Cause)
	default:
		return ""
	}
}

func receiptSettlementDetail(r *claudiawire.RequestReceipt) string {
	return fmt.Sprintf("Claudia durable receipt request=%q session=%q state=%s cause=%q reason=%q; transport accepted, mission outcome separate", r.RequestID, r.SessionID, r.State, receiptExecutionCause(r), r.Reason)
}

// handoffConfirmed reports whether settleBoundHandoffReceipt removed the
// attempt. A settling receipt resolves Confirmed and the row leaves the
// queue. An unknown receipt leaves the row Uncertain.
func handoffConfirmed(s *Server, name, entryID string) bool {
	if s == nil || entryID == "" {
		return false
	}
	entries, err := s.sendQueue().Snapshot(name)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.ID == entryID {
			return false
		}
	}
	return true
}

// settleBoundHandoffReceipt settles one immediate or steer handoff from the
// structured receipt. A nil send error is not transport acceptance. A missing
// or unknown receipt keeps the attempt. confirmHere reads this result before
// it tells the caller sent or steered. confirmByCaller still chooses its own
// status word and only uses this to settle the queue.
func (s *Server) settleBoundHandoffReceipt(name string, handoff sendq.Entry) {
	if handoff.ID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), heldReceiptQueryTimeout)
	defer cancel()
	r, err := s.LookupHeldReceipt(ctx, claudiawire.ReceiptQuery{Seat: name, SessionID: handoff.SessionID, RequestID: handoff.TransportRequestID})
	if err == nil && r != nil {
		err = r.ValidateIdentity(name, handoff.SessionID, handoff.TransportRequestID)
	}
	if err != nil || r == nil || !receiptStateSettles(r.State) {
		detail := "structured receipt did not settle the transport obligation"
		if err != nil {
			detail = err.Error()
		} else if r != nil {
			detail = fmt.Sprintf("receipt state=%s cause=%q does not settle transport", r.State, receiptExecutionCause(r))
		}
		s.settleHandoff(name, handoff, sendq.Unverified, detail)
		return
	}
	if err := s.persistSendReceipt(name, handoff, r); err != nil {
		slog.Warn("held receipt evidence could not be persisted; retaining obligation", "agent", name, "entry_id", handoff.ID, "error", err)
		s.settleHandoff(name, handoff, sendq.Unverified, err.Error())
		return
	}
	s.settleHandoff(name, handoff, sendq.Confirmed, receiptSettlementDetail(r))
}

// settleBoundDrainReceipt settles one drain from the structured receipt.
// The transcript watch is not consulted. A missing or unknown receipt keeps
// the attempt and does not count as delivery.
func (s *Server) settleBoundDrainReceipt(ctx context.Context, name string, entry sendq.Entry, generation uint64, resolve func(sendq.AttemptOutcome, string) bool) bool {
	r, err := s.LookupHeldReceipt(ctx, claudiawire.ReceiptQuery{Seat: name, SessionID: entry.SessionID, RequestID: entry.TransportRequestID})
	if err == nil {
		err = r.ValidateIdentity(name, entry.SessionID, entry.TransportRequestID)
	}
	if err != nil || r == nil || !receiptStateSettles(r.State) {
		detail := "structured receipt did not settle the transport obligation"
		if err != nil {
			detail = err.Error()
		} else if r != nil {
			detail = fmt.Sprintf("receipt state=%s cause=%q does not settle transport", r.State, receiptExecutionCause(r))
		}
		if resolve(sendq.Unverified, detail) {
			s.reportDrainedSendNotBegun(name, entry, TurnEvidence{}, time.Now())
		}
		return false
	}
	if err := s.persistSendReceipt(name, entry, r); err != nil {
		slog.Warn("held receipt evidence could not be persisted; retaining obligation", "agent", name, "entry_id", entry.ID, "error", err)
		if resolve(sendq.Unverified, err.Error()) {
			s.reportDrainedSendNotBegun(name, entry, TurnEvidence{}, time.Now())
		}
		return false
	}
	if !resolve(sendq.Confirmed, receiptSettlementDetail(r)) {
		return false
	}
	s.markAgentTurnBegan(name)
	ended := s.noteQueuedTurnBegan(name, generation)
	if !ended {
		s.wedges.handedOver(name, entry.ID)
	}
	s.clearSendqPin(name)
	slog.Info("agent send queue: drained one message", "name", name, "entry_id", entry.ID,
		"remaining", s.pendingAgentSends(name))
	return ended
}

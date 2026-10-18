// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sendq

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrActiveHandoff refuses an urgent admission while this process already
// owns an attempt for the seat. Callers classify with errors.Is. A different
// error that only mentions an active handoff is not this refusal.
var ErrActiveHandoff = errors.New("sendq: urgent delivery conflicts with an active handoff")

// logicalTransportID deliberately does not populate RequestID: that field
// marks owner questions, which must not be folded into routine digests.
func (e Entry) logicalTransportID() string {
	if e.RequestID != "" {
		return e.RequestID
	}
	return "sendq:" + e.ID
}

func (e Entry) validateReceiptBinding() error {
	if e.WireText != "" && !e.WireTextSet {
		return fmt.Errorf("entry %s has wire payload without a presence marker", e.ID)
	}
	if e.TransportRequestID == "" && e.SessionID == "" {
		if e.WireTextSet {
			return fmt.Errorf("entry %s has wire payload without receipt identity", e.ID)
		}
		return nil // older queue files remain readable, without new evidence
	}
	if e.ID == "" || strings.TrimSpace(e.TransportRequestID) == "" || strings.TrimSpace(e.SessionID) == "" {
		return fmt.Errorf("entry %s has incomplete receipt identity", e.ID)
	}
	if e.TransportRequestID != e.logicalTransportID() {
		return fmt.Errorf("entry %s has conflicting transport request identity", e.ID)
	}
	return nil
}

// WirePayload returns the immutable text for an identified handoff. Older
// bindings predate transport decoration and retain their original payload.
func (e Entry) WirePayload() string {
	if e.WireTextSet {
		return e.WireText
	}
	return e.Text
}

// BindReceipt durably binds a locally owned attempt to the actual receiving
// generation. Call it before handing the payload to any transport. It returns
// the authoritative entry to use for receipt queries and Resolve. An old
// orphan cannot be bound after restart, and a binding can never be reassigned
// to a replacement session, even after an explicit retry of the same entry.
func (s *Store) BindReceipt(agent string, attempt Entry, sessionID string) (Entry, error) {
	return s.bindReceipt(agent, attempt, sessionID, nil)
}

// BindReceiptPayload freezes the actual wire payload along with its identity.
// Once bound, retries use the stored text, never a newly stamped report.
func (s *Store) BindReceiptPayload(agent string, attempt Entry, sessionID, text string) (Entry, error) {
	return s.bindReceipt(agent, attempt, sessionID, &text)
}

func (s *Store) bindReceipt(agent string, attempt Entry, sessionID string, text *string) (Entry, error) {
	if s == nil {
		return Entry{}, fmt.Errorf("sendq: no store")
	}
	if strings.TrimSpace(sessionID) == "" {
		return Entry{}, fmt.Errorf("sendq: missing receipt session identity")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load(agent)
	if err != nil {
		return Entry{}, err
	}
	for i, e := range f.Entries {
		if e.ID != attempt.ID || e.ID == "" {
			continue
		}
		if e.State != Attempting || e.AttemptID == "" || e.AttemptID != attempt.AttemptID || s.active[agent] != e.AttemptID {
			return Entry{}, fmt.Errorf("sendq: receipt binding requires the current locally owned attempt for %q", agent)
		}
		if e.SessionID != "" && e.SessionID != sessionID {
			return Entry{}, fmt.Errorf("sendq: entry %s is bound to session %q, not %q", e.ID, e.SessionID, sessionID)
		}
		if e.SessionID == "" && text != nil {
			e.WireText, e.WireTextSet = *text, true
		}
		e.TransportRequestID, e.SessionID = e.logicalTransportID(), sessionID
		if err := e.validateReceiptBinding(); err != nil {
			return Entry{}, err
		}
		f.Entries[i] = e
		if err := s.save(f); err != nil {
			return Entry{}, err
		}
		return e, nil
	}
	return Entry{}, fmt.Errorf("sendq: receipt attempt no longer exists for %q", agent)
}

// AppendAndClaimWithRequestID durably admits this exact message, claiming it
// only when no older Pending entry or locally active attempt precedes it.
// Otherwise it returns the new Pending entry with claimed=false; the caller
// must report queued rather than submit it. Uncertain/orphaned attempts are
// flowed past just as ClaimFront does. This is not a priority/overtaking API.
// A claimed entry still requires BindReceipt before its transport handoff.
func (s *Store) AppendAndClaimWithRequestID(agent, text, requestID string, at time.Time) (entry Entry, depth int, claimed bool, err error) {
	return s.appendAndClaim(agent, text, requestID, at, false)
}

// AppendAndClaimUrgent explicitly overtakes older pending input for a steer or
// interrupt. It never steals a locally active handoff. Such a conflict refuses
// admission, rather than silently downgrading urgency to ordinary queued input.
func (s *Store) AppendAndClaimUrgent(agent, text, requestID string, at time.Time) (Entry, int, bool, error) {
	return s.appendAndClaim(agent, text, requestID, at, true)
}

func (s *Store) appendAndClaim(agent, text, requestID string, at time.Time, urgent bool) (entry Entry, depth int, claimed bool, err error) {
	if s == nil {
		return Entry{}, 0, false, fmt.Errorf("sendq: no store")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load(agent)
	if err != nil {
		return Entry{}, 0, false, err
	}
	if err := duplicateRequestID(f.Entries, text, requestID); err != nil {
		return Entry{}, 0, false, err
	}
	if urgent && s.active[agent] != "" {
		return Entry{}, len(f.Entries), false, fmt.Errorf("%w: urgent delivery to %q not admitted", ErrActiveHandoff, agent)
	}
	entry = Entry{ID: NewID(), RequestID: requestID, Text: text, EnqueuedAt: at.UTC()}
	i, _, blocked := claimable(f.Entries, s.active[agent])
	claimed = urgent || (!blocked && i < 0)
	if claimed {
		entry.State, entry.AttemptID = Attempting, NewID()
	}
	f.Entries = append(f.Entries, entry)
	// Ownership is recorded before save so a claimed urgent entry is
	// observed as in flight, not as a held attempt.
	var prev string
	var had bool
	if claimed {
		if s.active == nil {
			s.active = map[string]string{}
		}
		prev, had = s.active[agent]
		s.active[agent] = entry.AttemptID
	}
	if err := s.save(f); err != nil {
		if claimed {
			if had {
				s.active[agent] = prev
			} else {
				delete(s.active, agent)
			}
		}
		return Entry{}, 0, false, err
	}
	return entry, len(f.Entries), claimed, nil
}

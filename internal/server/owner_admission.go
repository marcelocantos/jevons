// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"
)

// T1054: this is the host-controlled admission seam, NOT a classifier of
// authored prose. Do not bind it from a model-supplied category or a TurnID
// alone. The owner request ID must be minted at authenticated intake and
// correlated by the transport across retries before the production caller can
// enable it. Until then the legacy path is unchanged (no unsafe activation).
// An unknown claim never releases authored body: the fail-open behavior is a
// visible, daemon-authored degradation indicator and retained investigation.
const admissionMaxBytes = 64 << 10
const admissionHoldLimit = 5 * time.Second

type AdmissionDecision string

const (
	AdmissionAllowed AdmissionDecision = "allowed"
	AdmissionSilent  AdmissionDecision = "silent"
)

type admissionCandidate struct {
	turnID, requestID string
	held              []claudia.Event
	bytes             int
	deadline          time.Time
	decision          AdmissionDecision
	evidenceID        string
	timer             *time.Timer
}

type overseerAdmission struct {
	mu        sync.Mutex
	candidate *admissionCandidate
	auditDir  string
	// Set by the trusted owner intake; never by provider output.
	requests  map[string]bool
	incidents map[string]bool
}

// EnableOverseerAdmission configures the isolated seam. Not called in the
// production bootstrap until the transport supplies host request correlation.
// auditDir must be outside the chat journal/history directory and private.
func (s *Server) EnableOverseerAdmission(auditDir string) error {
	if auditDir == "" {
		return errors.New("admission: restricted audit directory required")
	}
	if err := os.MkdirAll(auditDir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(auditDir, 0700); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ownerAdmission != nil {
		return errors.New("admission already enabled")
	}
	s.ownerAdmission = &overseerAdmission{auditDir: auditDir, requests: map[string]bool{}, incidents: map[string]bool{}}
	return nil
}

// BindOwnerAdmission is invoked only after authenticated owner intake and
// transport correlation. The request ID is host-minted, not Event.TurnID.
// Rebinding a still-open candidate is refused, including across tool use.
func (s *Server) BindOwnerAdmission(turnID, requestID string) error {
	if turnID == "" || requestID == "" {
		return errors.New("admission: missing trusted identity")
	}
	s.mu.RLock()
	a := s.ownerAdmission
	s.mu.RUnlock()
	if a == nil {
		return errors.New("admission: not enabled")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.candidate != nil {
		return errors.New("admission: candidate already open")
	}
	if _, exists := a.requests[requestID]; exists {
		return errors.New("admission: request ID reused")
	}
	a.requests[requestID] = true
	c := &admissionCandidate{turnID: turnID, requestID: requestID, deadline: time.Now().Add(admissionHoldLimit)}
	a.candidate = c
	c.timer = time.AfterFunc(admissionHoldLimit, func() { s.expireOwnerAdmission(c, "timeout") })
	return nil
}

// AdmitOwnerCandidate takes a HOST decision. An owner answer must refer to
// the still-open request. An incident is independently registered by trusted
// host intake and cannot be invented by a model claim. Routine/silent has no
// body authority. After a tool_use stop the next candidate needs a new claim.
func (s *Server) AdmitOwnerCandidate(turnID, evidenceID string, decision AdmissionDecision) error {
	s.mu.RLock()
	a := s.ownerAdmission
	s.mu.RUnlock()
	if a == nil {
		return errors.New("admission: not enabled")
	}
	a.mu.Lock()
	c := a.candidate
	if c == nil || c.turnID != turnID {
		a.mu.Unlock()
		return errors.New("admission: missing or mismatched turn")
	}
	if time.Now().After(c.deadline) {
		a.mu.Unlock()
		s.expireOwnerAdmission(c, "late claim")
		return errors.New("admission: expired")
	}
	if decision != AdmissionAllowed && decision != AdmissionSilent {
		a.mu.Unlock()
		return errors.New("admission: invalid decision")
	}
	if decision == AdmissionAllowed && !(evidenceID == c.requestID && a.requests[evidenceID]) && !a.incidents[evidenceID] {
		a.mu.Unlock()
		return errors.New("admission: no trusted open evidence")
	}
	if decision == AdmissionSilent && evidenceID != "" {
		a.mu.Unlock()
		return errors.New("admission: silent claim cannot consume evidence")
	}
	c.decision = decision
	c.evidenceID = evidenceID
	held := c.held
	c.held = nil
	c.bytes = 0
	a.mu.Unlock()
	s.releaseAdmissionEvents(c, held)
	return nil
}

// RegisterOwnerIncident is for trusted host incident intake, not authored
// prose. Materiality and containment must be independently adjudicated.
func (s *Server) RegisterOwnerIncident(id string) error {
	if id == "" {
		return errors.New("admission: empty incident ID")
	}
	s.mu.RLock()
	a := s.ownerAdmission
	s.mu.RUnlock()
	if a == nil {
		return errors.New("admission: not enabled")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.incidents[id] {
		return errors.New("admission: incident ID reused")
	}
	a.incidents[id] = true
	return nil
}

// holdOverseerAdmission is the FIRST seam in DeliverOverseerEvent: nothing
// from a candidate can reach chatWireLine, losslessLine, journal or WS before
// a trusted decision. In particular TUI previews and tool-use progress can
// carry candidate text and must not bypass the hold.
func (s *Server) holdOverseerAdmission(ev claudia.Event) bool {
	s.mu.RLock()
	a := s.ownerAdmission
	s.mu.RUnlock()
	if a == nil {
		return false
	}
	a.mu.Lock()
	c := a.candidate
	if c == nil {
		a.mu.Unlock()
		if ev.Type == "assistant" || ev.Type == "progress" {
			s.auditOwnerAdmission(a, &admissionCandidate{turnID: ev.TurnID}, ev, "unbound turn")
			s.admissionDegraded("unbound turn", ev.TurnID)
			return true
		}
		return false
	}
	if ev.TurnID == "" || ev.TurnID != c.turnID {
		a.mu.Unlock()
		s.auditOwnerAdmission(a, c, ev, "missing or mismatched event identity")
		s.expireOwnerAdmission(c, "missing or mismatched event identity")
		return true
	}
	if time.Now().After(c.deadline) {
		a.mu.Unlock()
		s.auditOwnerAdmission(a, c, ev, "timeout")
		s.expireOwnerAdmission(c, "timeout")
		return true
	}
	size := len(ev.Text) + len(ev.Raw)
	if c.bytes+size > admissionMaxBytes {
		a.mu.Unlock()
		s.auditOwnerAdmission(a, c, ev, "buffer limit")
		s.expireOwnerAdmission(c, "buffer limit")
		return true
	}
	c.bytes += size
	c.held = append(c.held, ev)
	// Write a restricted, non-replayable copy on receipt: a daemon restart
	// before the timer fires must not destroy an unadjudicated anomaly.
	s.auditOwnerAdmission(a, c, ev, "held")
	// Even after one allowed fragment, continue holding until seal; an authored
	// candidate cannot grant itself authority for a later tool continuation.
	if c.decision == "" {
		a.mu.Unlock()
		return true
	}
	held := c.held
	c.held = nil
	c.bytes = 0
	a.mu.Unlock()
	s.releaseAdmissionEvents(c, held)
	return true
}

func (s *Server) releaseAdmissionEvents(c *admissionCandidate, events []claudia.Event) {
	s.mu.RLock()
	a := s.ownerAdmission
	s.mu.RUnlock()
	for _, ev := range events {
		if c.decision == AdmissionSilent {
			s.auditOwnerAdmission(a, c, ev, "silent")
			if ev.IsTerminalStop() {
				s.deliverOverseerEventAdmitted(claudia.Event{Type: "assistant", TurnID: c.turnID, StopReason: ev.StopReason})
			}
		} else {
			durable := s.deliverOverseerEventAdmitted(ev)
			if durable && ev.Type == "assistant" && strings.TrimSpace(ev.Text) != "" {
				a.mu.Lock()
				if c.evidenceID == c.requestID {
					a.requests[c.requestID] = false
				} else if c.evidenceID != "" {
					a.incidents[c.evidenceID] = false
				}
				a.mu.Unlock()
			}
		}
		// A tool-use stop is not a terminal turn. Require another host decision.
		if ev.Type == "assistant" && ev.StopReason == "tool_use" {
			a.mu.Lock()
			if a.candidate == c {
				c.decision = ""
				c.evidenceID = ""
				c.deadline = time.Now().Add(admissionHoldLimit)
				c.timer.Stop()
				c.timer = time.AfterFunc(admissionHoldLimit, func() { s.expireOwnerAdmission(c, "timeout") })
			}
			a.mu.Unlock()
		}
		if ev.IsTerminalStop() {
			a.mu.Lock()
			if a.candidate == c {
				a.candidate = nil
				c.timer.Stop() /* Leave obligation open until durable publication is independently observed. */
			}
			a.mu.Unlock()
		}
	}
}

func (s *Server) expireOwnerAdmission(c *admissionCandidate, reason string) {
	s.mu.RLock()
	a := s.ownerAdmission
	s.mu.RUnlock()
	if a == nil {
		return
	}
	a.mu.Lock()
	if a.candidate != c {
		a.mu.Unlock()
		return
	}
	a.candidate = nil
	c.timer.Stop()
	held := c.held
	c.held = nil
	a.mu.Unlock()
	for _, ev := range held {
		s.auditOwnerAdmission(a, c, ev, reason)
	}
	s.admissionDegraded(reason, c.turnID)
	// Settle the working indicator with a body-less host turn. No provider
	// content crosses this path, including a forged terminal payload.
	s.deliverOverseerEventAdmitted(claudia.Event{Type: "assistant", StopReason: "end_turn"})
	// Request/incident obligations remain open; no model prose is an answer.
}

func (s *Server) admissionDegraded(reason, turnID string) {
	slog.Error("owner admission degraded", "reason", reason, "turn_id", turnID)
	b, _ := json.Marshal(map[string]string{"type": "status", "text": "Owner response delayed: admission evidence unavailable; investigating."})
	s.broadcastChatLive(string(b)) // status is daemon-authored, never replayed as assistant prose
}

func (s *Server) auditOwnerAdmission(a *overseerAdmission, c *admissionCandidate, ev claudia.Event, reason string) {
	if a == nil {
		return
	}
	// Event excludes provider Raw/Text in JSON; include both explicitly in the
	// restricted store rather than in the owner chat journal or public logs.
	row, _ := json.Marshal(map[string]any{"time": time.Now().UTC(), "turn_id": c.turnID, "request_id": c.requestID, "reason": reason, "text": ev.Text, "raw": json.RawMessage(ev.Raw)})
	path := filepath.Join(a.auditDir, "owner-admission.jsonl")
	if st, err := os.Lstat(path); err == nil && (st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular()) {
		slog.Error("admission audit unsafe file type")
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		slog.Error("admission audit open failed", "err", err)
		return
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Mode().Perm() != 0600 {
		_ = f.Chmod(0600)
	}
	if _, err = fmt.Fprintln(f, string(row)); err != nil {
		slog.Error("admission audit write failed", "err", err)
	}
	if err = f.Sync(); err != nil {
		slog.Error("admission audit sync failed", "err", err)
	}
}

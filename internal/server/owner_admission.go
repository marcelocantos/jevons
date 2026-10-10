// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
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

type admissionAuthority struct{ nonce string } // unexported capability, never derived from provider strings
type admissionIncident struct {
	turnID string
	open   bool
}

type overseerAdmission struct {
	mu            sync.Mutex
	auditMu       sync.Mutex
	authority     *admissionAuthority
	publishMu     sync.Mutex // serializes admission, events, expiry and release in receipt order
	candidate     *admissionCandidate
	auditDir      string
	auditDev      uint64
	auditIno      uint64
	beforePublish func(claudia.Event) // test-only ordering barrier; nil in production
	// Set by the trusted owner intake; never by provider output.
	requests  map[string]bool
	incidents map[string]admissionIncident
}

// EnableOverseerAdmission configures the isolated seam. Not called in the
// production bootstrap until the transport supplies host request correlation.
// auditDir must be outside the chat journal/history directory and private.
func (s *Server) EnableOverseerAdmission(auditDir string) error {
	if auditDir == "" {
		return errors.New("admission: restricted audit directory required")
	}
	if parent, err := filepath.EvalSymlinks(filepath.Dir(auditDir)); err == nil {
		auditDir = filepath.Join(parent, filepath.Base(auditDir))
	}
	if err := validateAdmissionAuditDir(s, auditDir); err != nil {
		return err
	}
	if err := os.MkdirAll(auditDir, 0700); err != nil {
		return err
	}
	dirfd, err := unix.Open(auditDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	var ds unix.Stat_t
	err = unix.Fstat(dirfd, &ds)
	unix.Close(dirfd)
	if err != nil {
		return err
	}
	if ds.Uid != uint32(os.Getuid()) || ds.Mode&0777 != 0700 {
		return errors.New("admission audit directory must be private and owned by daemon")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ownerAdmission != nil {
		return errors.New("admission already enabled")
	}
	s.ownerAdmission = &overseerAdmission{auditDir: auditDir, auditDev: uint64(ds.Dev), auditIno: ds.Ino, authority: &admissionAuthority{nonce: uuid.NewString()}, requests: map[string]bool{}, incidents: map[string]admissionIncident{}}
	return nil
}

// bindOwnerAdmission is invoked only after authenticated owner intake and
// transport correlation. The request ID is host-minted, not Event.TurnID.
// Rebinding a still-open candidate is refused, including across tool use.
func (s *Server) bindOwnerAdmission(cap *admissionAuthority, turnID, requestID string) error {
	if turnID == "" || requestID == "" {
		return errors.New("admission: missing trusted identity")
	}
	s.mu.RLock()
	a := s.ownerAdmission
	s.mu.RUnlock()
	if a == nil {
		return errors.New("admission: not enabled")
	}
	if cap == nil || cap != a.authority {
		return errors.New("admission: untrusted caller")
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

// admitOwnerCandidate takes a HOST decision. An owner answer must refer to
// the still-open request. An incident is independently registered by trusted
// host intake and cannot be invented by a model claim. Routine/silent has no
// body authority. After a tool_use stop the next candidate needs a new claim.
func (s *Server) admitOwnerCandidate(cap *admissionAuthority, turnID, evidenceID string, decision AdmissionDecision) error {
	s.mu.RLock()
	a := s.ownerAdmission
	s.mu.RUnlock()
	if a == nil {
		return errors.New("admission: not enabled")
	}
	if cap == nil || cap != a.authority {
		return errors.New("admission: untrusted caller")
	}
	a.publishMu.Lock()
	defer a.publishMu.Unlock()
	a.mu.Lock()
	c := a.candidate
	if c == nil || c.turnID != turnID {
		a.mu.Unlock()
		return errors.New("admission: missing or mismatched turn")
	}
	if time.Now().After(c.deadline) {
		a.mu.Unlock()
		s.expireOwnerAdmissionLocked(c, "late claim")
		return errors.New("admission: expired")
	}
	if decision != AdmissionAllowed && decision != AdmissionSilent {
		a.mu.Unlock()
		return errors.New("admission: invalid decision")
	}
	incident := a.incidents[evidenceID]
	if decision == AdmissionAllowed && !(evidenceID == c.requestID && a.requests[evidenceID]) && !(incident.open && incident.turnID == c.turnID) {
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

// registerOwnerIncident is for trusted host incident intake, not authored
// prose. Materiality and containment must be independently adjudicated.
func (s *Server) registerOwnerIncident(cap *admissionAuthority, turnID, id string) error {
	if id == "" || turnID == "" {
		return errors.New("admission: empty incident or turn ID")
	}
	s.mu.RLock()
	a := s.ownerAdmission
	s.mu.RUnlock()
	if a == nil {
		return errors.New("admission: not enabled")
	}
	if cap == nil || cap != a.authority {
		return errors.New("admission: untrusted caller")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.incidents[id]; exists {
		return errors.New("admission: incident ID reused")
	}
	a.incidents[id] = admissionIncident{turnID: turnID, open: true}
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
	a.publishMu.Lock()
	defer a.publishMu.Unlock()
	a.mu.Lock()
	c := a.candidate
	if c == nil {
		a.mu.Unlock()
		if ev.Type == "assistant" || ev.Type == "progress" {
			if err := s.auditOwnerAdmission(a, &admissionCandidate{turnID: ev.TurnID}, ev, "unbound turn"); err != nil {
				s.admissionDegraded("AUDIT LOSS: unbound event", ev.TurnID)
			}
			s.admissionDegraded("unbound turn", ev.TurnID)
			return true
		}
		return false
	}
	if ev.TurnID == "" || ev.TurnID != c.turnID {
		a.mu.Unlock()
		s.auditOwnerAdmission(a, c, ev, "missing or mismatched event identity")
		s.expireOwnerAdmissionLocked(c, "missing or mismatched event identity")
		return true
	}
	if time.Now().After(c.deadline) {
		a.mu.Unlock()
		s.auditOwnerAdmission(a, c, ev, "timeout")
		s.expireOwnerAdmissionLocked(c, "timeout")
		return true
	}
	size := len(ev.Text) + len(ev.Raw)
	if c.bytes+size > admissionMaxBytes {
		a.mu.Unlock()
		s.auditOwnerAdmission(a, c, ev, "buffer limit")
		s.expireOwnerAdmissionLocked(c, "buffer limit")
		return true
	}
	// Audit must be fsynced before holding or publishing a candidate. If the
	// restricted store fails, no authored body is released; a trusted visible
	// loss marker names the preservation failure rather than pretending safety.
	if err := s.auditOwnerAdmission(a, c, ev, "held"); err != nil {
		a.mu.Unlock()
		s.expireOwnerAdmissionLocked(c, "AUDIT LOSS: restricted audit persistence failed")
		return true
	}
	c.bytes += size
	c.held = append(c.held, ev)
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
		if a.beforePublish != nil {
			a.beforePublish(ev)
		}
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
					incident := a.incidents[c.evidenceID]
					incident.open = false
					a.incidents[c.evidenceID] = incident
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
	a.publishMu.Lock()
	defer a.publishMu.Unlock()
	s.expireOwnerAdmissionLocked(c, reason)
}

// expireOwnerAdmissionLocked is called with publishMu held.
func (s *Server) expireOwnerAdmissionLocked(c *admissionCandidate, reason string) {
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
	b, _ := json.Marshal(map[string]string{"type": "status", "text": admissionStatusText(reason)})
	s.broadcastChatLive(string(b)) // status is daemon-authored, never replayed as assistant prose
}

// validateAdmissionAuditDir rejects paths that would be replayed as owner
// chat, and refuses symlinked or foreign-owned directory components. This is
// called before creation; auditOwnerAdmission revalidates via openat on every
// write to close final-component symlink substitution.
func validateAdmissionAuditDir(s *Server, dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	s.mu.RLock()
	clog := s.chatLog
	s.mu.RUnlock()
	if clog != nil {
		journal, err := filepath.Abs(clog.Path())
		if err != nil {
			return err
		}
		if realParent, e := filepath.EvalSymlinks(filepath.Dir(journal)); e == nil {
			journal = filepath.Join(realParent, filepath.Base(journal))
		}
		rel, err := filepath.Rel(abs, journal)
		if err != nil {
			return err
		}
		if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))) {
			return errors.New("admission audit directory contains owner chat journal")
		}
		if abs == filepath.Dir(journal) {
			return errors.New("admission audit directory shares owner chat directory")
		}
	}
	// Existing path components must never be symlinks. New components are
	// created with restrictive permissions; check ownership after creation.
	for p := abs; ; p = filepath.Dir(p) {
		st, e := os.Lstat(p)
		if e == nil && st.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("admission audit symlink component: %s", p)
		}
		if e != nil && !os.IsNotExist(e) {
			return e
		}
		if p == filepath.Dir(p) {
			break
		}
	}
	return nil
}

func (s *Server) auditOwnerAdmission(a *overseerAdmission, c *admissionCandidate, ev claudia.Event, reason string) error {
	if a == nil {
		return errors.New("audit not configured")
	}
	a.auditMu.Lock()
	defer a.auditMu.Unlock()
	row, err := json.Marshal(map[string]any{"time": time.Now().UTC(), "turn_id": c.turnID, "request_id": c.requestID, "reason": reason, "text": ev.Text, "raw": json.RawMessage(ev.Raw)})
	if err != nil {
		return fmt.Errorf("audit marshal: %w", err)
	}
	// O_NOFOLLOW on the directory and on the relative file prevents a race
	// replacing either leaf with a symlink between inspection and append.
	dirfd, err := unix.Open(a.auditDir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("audit directory open: %w", err)
	}
	defer unix.Close(dirfd)
	var ds unix.Stat_t
	if err := unix.Fstat(dirfd, &ds); err != nil {
		return err
	}
	if ds.Uid != uint32(os.Getuid()) || ds.Mode&0777 != 0700 || uint64(ds.Dev) != a.auditDev || ds.Ino != a.auditIno {
		return errors.New("audit directory ownership/mode changed")
	}
	fd, err := unix.Openat(dirfd, "owner-admission.jsonl", unix.O_CREAT|unix.O_WRONLY|unix.O_APPEND|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return fmt.Errorf("audit file open: %w", err)
	}
	f := os.NewFile(uintptr(fd), "owner-admission.jsonl")
	defer f.Close()
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(os.Getuid()) || st.Mode&0777 != 0600 {
		return errors.New("audit file ownership/type/mode changed")
	}
	if _, err = fmt.Fprintln(f, string(row)); err != nil {
		return fmt.Errorf("audit write: %w", err)
	}
	if err = f.Sync(); err != nil {
		return fmt.Errorf("audit sync: %w", err)
	}
	return nil
}

func admissionStatusText(reason string) string {
	if strings.Contains(reason, "AUDIT LOSS") {
		return "Owner response delayed: restricted audit unavailable; candidate content may be lost. Investigating."
	}
	return "Owner response delayed: admission evidence unavailable; investigating."
}

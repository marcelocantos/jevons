// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/discovery"
)

// 🎯T679.2 — a seat that accepts a prompt and never produces a transcript
// is marked born-stuck, and its parent is told once.
//
// Grace runs from the accepted opening/remint prompt, not registration or
// Launch. Cold session loading therefore cannot consume it. The 120s bound
// is the T679 scout's proposal: more headroom than the 45s turn-confirm
// window, explicitly not a measured latency percentile.

// AgentStatusBornStuck is the agent_list phase for a live seat whose
// accepted prompt never produced a transcript (🎯T679.2).
const AgentStatusBornStuck = "born-stuck"

// BornStuckGrace is how long after an accepted opening/remint prompt a
// missing transcript may still be "not yet" rather than born-stuck.
const BornStuckGrace = 120 * time.Second

const bornStuckFileName = "born-stuck.json"

// birthPromptAccepted reports whether a send outcome means the prompt was
// handed to the provider or held by the daemon — the clock starts then.
func birthPromptAccepted(status string, err error) bool {
	if err != nil {
		return false
	}
	switch strings.TrimSpace(status) {
	case "sent", "rehydrated_sent", "interrupted_sent",
		"queued", "interrupted_queued", "delivered_unconfirmed":
		return true
	}
	return false
}

// noticeSubmitted reports whether a parent notice was queued or accepted.
// An error is never success; a missing status is never success.
func noticeSubmitted(status string, err error) bool {
	return birthPromptAccepted(status, err)
}

func birthKey(name, sessionID string) string {
	return strings.TrimSpace(name) + "\x1f" + strings.TrimSpace(sessionID)
}

func bornStuckNoticeKey(name, sessionID string) string {
	return "born-stuck:" + strings.TrimSpace(name) + ":" + strings.TrimSpace(sessionID)
}

type birthRecord struct {
	Name       string    `json:"name"`
	Provider   string    `json:"provider"`
	SessionID  string    `json:"session_id"`
	AcceptedAt time.Time `json:"accepted_at"`
	NoticeKey  string    `json:"notice_key"`
}

type noticeRecord struct {
	Submitted   bool      `json:"submitted"`
	SubmittedAt time.Time `json:"submitted_at,omitempty"`
	LastError   string    `json:"last_error,omitempty"`
}

type birthFile struct {
	Births  map[string]birthRecord  `json:"births"`
	Notices map[string]noticeRecord `json:"notices"`
}

type birthLedger struct {
	mu      sync.Mutex
	path    string
	births  map[string]birthRecord
	notices map[string]noticeRecord
	// loadErr is sticky: a malformed file is a hard error, never a silent
	// reset, and writes must not clobber the corrupt record.
	loadErr error
}

func newBirthLedger(path string) *birthLedger {
	l := &birthLedger{
		path:    strings.TrimSpace(path),
		births:  map[string]birthRecord{},
		notices: map[string]noticeRecord{},
	}
	if l.path != "" {
		l.loadErr = l.load()
	}
	return l
}

func (l *birthLedger) load() error {
	data, err := os.ReadFile(l.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("born-stuck: read %s: %w", l.path, err)
	}
	var f birthFile
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("born-stuck: malformed %s: %w", l.path, err)
	}
	if f.Births == nil {
		f.Births = map[string]birthRecord{}
	}
	if f.Notices == nil {
		f.Notices = map[string]noticeRecord{}
	}
	l.births = f.Births
	l.notices = f.Notices
	return nil
}

func (l *birthLedger) persistLocked() error {
	if l.path == "" {
		return nil
	}
	if l.loadErr != nil {
		return l.loadErr
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("born-stuck: dir: %w", err)
	}
	f := birthFile{Births: l.births, Notices: l.notices}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("born-stuck: encode: %w", err)
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("born-stuck: write: %w", err)
	}
	if err := os.Rename(tmp, l.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("born-stuck: commit: %w", err)
	}
	return nil
}

func (s *Server) births() *birthLedger {
	if s == nil {
		return newBirthLedger("")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.birthStore == nil {
		s.birthStore = newBirthLedger("")
	}
	return s.birthStore
}

func (s *Server) setBirthStoreDir(stateDir string) {
	stateDir = strings.TrimSpace(stateDir)
	if strings.HasPrefix(stateDir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			stateDir = filepath.Join(home, stateDir[2:])
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if stateDir == "" {
		s.birthStore = newBirthLedger("")
		return
	}
	s.birthStore = newBirthLedger(filepath.Join(stateDir, bornStuckFileName))
	if s.birthStore.loadErr != nil {
		slog.Error("born-stuck ledger unreadable; notices will not be recorded until it is repaired",
			"path", s.birthStore.path, "err", s.birthStore.loadErr)
	}
}

// SetBirthClock injects the birth-monitor clock. Test seam: the 120s grace
// must not be crossed by sleeping.
func (s *Server) SetBirthClock(now func() time.Time) {
	s.mu.Lock()
	s.birthNow = now
	s.mu.Unlock()
}

// SetBirthRoots overrides DefaultSessionRoots for hermetic existence lookups.
func (s *Server) SetBirthRoots(roots discovery.Roots) {
	s.mu.Lock()
	cp := roots
	s.birthRoots = &cp
	s.mu.Unlock()
}

// SetSeatAliveFn overrides process aliveness for hermetic list/sweep tests.
func (s *Server) SetSeatAliveFn(fn func(string) bool) {
	s.mu.Lock()
	s.seatAliveFn = fn
	s.mu.Unlock()
}

func (s *Server) birthClock() time.Time {
	if s == nil {
		return time.Now()
	}
	s.mu.Lock()
	fn := s.birthNow
	s.mu.Unlock()
	if fn == nil {
		return time.Now()
	}
	return fn()
}

func (s *Server) transcriptRoots() discovery.Roots {
	if s == nil {
		return DefaultSessionRoots()
	}
	s.mu.Lock()
	roots := s.birthRoots
	s.mu.Unlock()
	if roots != nil {
		return *roots
	}
	return DefaultSessionRoots()
}

func (s *Server) seatAlive(name string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	fn := s.seatAliveFn
	s.mu.Unlock()
	if fn != nil {
		return fn(name)
	}
	if s.registry == nil {
		return false
	}
	proc := s.registry.Get(name)
	return proc != nil && proc.Alive()
}

// observeBirthAcceptance records the first accepted prompt for name's
// current session. Repeated nudges do not restart the clock.
func (s *Server) observeBirthAcceptance(name string, res agentSendResult, err error) {
	if s == nil || strings.TrimSpace(name) == "" {
		return
	}
	if !birthPromptAccepted(res.Status, err) {
		return
	}
	if s.isOverseerAgent(name) {
		return
	}
	s.noteBirthAccepted(name)
}

func (s *Server) noteBirthAccepted(name string) {
	if s == nil || s.registry == nil {
		return
	}
	d := s.registry.Def(name)
	if d == nil {
		return
	}
	sid := strings.TrimSpace(d.SessionID)
	if sid == "" {
		return
	}
	key := birthKey(d.Name, sid)
	rec := birthRecord{
		Name:       d.Name,
		Provider:   string(d.Provider),
		SessionID:  sid,
		AcceptedAt: s.birthClock().UTC(),
		NoticeKey:  bornStuckNoticeKey(d.Name, sid),
	}
	l := s.births()
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, exists := l.births[key]; exists {
		return
	}
	l.births[key] = rec
	if err := l.persistLocked(); err != nil {
		slog.Error("born-stuck: persist accepted prompt", "agent", name, "err", err)
	}
}

type birthDiagnosis struct {
	Stuck     bool
	Unknown   bool
	Elapsed   time.Duration
	Existence TranscriptExistence
	Birth     birthRecord
	Accepted  bool
	PastGrace bool
}

func (s *Server) diagnoseBirth(d claudia.AgentDef, now time.Time) birthDiagnosis {
	var out birthDiagnosis
	sid := strings.TrimSpace(d.SessionID)
	if sid == "" || strings.TrimSpace(d.Name) == "" {
		return out
	}
	l := s.births()
	l.mu.Lock()
	rec, ok := l.births[birthKey(d.Name, sid)]
	l.mu.Unlock()
	if !ok {
		return out
	}
	out.Accepted = true
	out.Birth = rec
	if now.IsZero() {
		now = s.birthClock()
	}
	if !rec.AcceptedAt.IsZero() && !now.Before(rec.AcceptedAt) {
		out.Elapsed = now.Sub(rec.AcceptedAt)
	}
	out.PastGrace = out.Elapsed >= BornStuckGrace
	out.Existence = LookupTranscriptExistence(TranscriptExistenceQuery{
		Name: d.Name, Provider: d.Provider, SessionID: sid, WorkDir: d.WorkDir,
		Roots: s.transcriptRoots(),
	})
	switch out.Existence.Verdict {
	case ExistencePresent:
		return out
	case ExistenceUnobservable:
		out.Unknown = true
		return out
	case ExistenceAbsent:
		if out.PastGrace {
			out.Stuck = true
		}
	}
	return out
}

func (s *Server) seatIsBornStuck(d claudia.AgentDef) bool {
	return s.diagnoseBirth(d, s.birthClock()).Stuck
}

// FormatBornStuckNotice is the one parent message: provider, session,
// elapsed, and that no transcript ever appeared.
func FormatBornStuckNotice(d claudia.AgentDef, elapsed time.Duration) string {
	provider := string(d.Provider)
	if strings.TrimSpace(provider) == "" {
		provider = "claude"
	}
	return fmt.Sprintf(
		"born-stuck: %s (provider=%s session=%s elapsed=%s) — no transcript ever appeared",
		d.Name, provider, strings.TrimSpace(d.SessionID), elapsed.Round(time.Second))
}

// FormatBornStuckLine is the agent_list annotation under a born-stuck row.
func FormatBornStuckLine(d claudia.AgentDef, elapsed time.Duration) string {
	return FormatBornStuckNotice(d, elapsed)
}

func (s *Server) noticeAlreadySubmitted(key string) bool {
	l := s.births()
	l.mu.Lock()
	defer l.mu.Unlock()
	n, ok := l.notices[key]
	return ok && n.Submitted
}

func (s *Server) markNoticeOutcome(key string, submitted bool, err error, now time.Time) {
	l := s.births()
	l.mu.Lock()
	defer l.mu.Unlock()
	n := l.notices[key]
	if submitted {
		n.Submitted = true
		n.SubmittedAt = now.UTC()
		n.LastError = ""
	} else if err != nil {
		n.LastError = err.Error()
	} else {
		n.LastError = "notice not submitted"
	}
	if l.notices == nil {
		l.notices = map[string]noticeRecord{}
	}
	l.notices[key] = n
	if perr := l.persistLocked(); perr != nil {
		slog.Error("born-stuck: persist notice outcome", "key", key, "err", perr)
	}
}

func (s *Server) notifyBornStuckIfDue(d claudia.AgentDef, now time.Time) {
	diag := s.diagnoseBirth(d, now)
	if !diag.Stuck {
		return
	}
	key := diag.Birth.NoticeKey
	if key == "" {
		key = bornStuckNoticeKey(d.Name, d.SessionID)
	}
	if s.noticeAlreadySubmitted(key) {
		return
	}
	parent := strings.TrimSpace(d.Parent)
	if parent == "" {
		parent = s.overseerName()
	}
	if parent == "" || parent == d.Name {
		s.markNoticeOutcome(key, false, fmt.Errorf("no registry parent"), now)
		return
	}
	text := FormatBornStuckNotice(d, diag.Elapsed)
	res, err := s.deliverByName(parent, text, OriginAgent, false)
	if noticeSubmitted(res.Status, err) {
		s.markNoticeOutcome(key, true, nil, now)
		return
	}
	if err == nil {
		err = fmt.Errorf("notice status %q is not submitted", res.Status)
	}
	s.markNoticeOutcome(key, false, err, now)
	slog.Info("born-stuck notice unsubmitted; will retry",
		"agent", d.Name, "parent", parent, "status", res.Status, "err", err)
}

// sweepBornStuck diagnoses live seats and delivers at most one parent notice
// per (name, session). It does not resend the opening prompt, stop, kill, or
// migrate, and it does not touch 🎯T664's undecided-delivery guard.
func (s *Server) sweepBornStuck() {
	if s == nil || s.registry == nil {
		return
	}
	now := s.birthClock()
	for _, d := range s.registry.List() {
		if strings.TrimSpace(d.Name) == "" {
			continue
		}
		if !s.seatAlive(d.Name) {
			continue
		}
		s.notifyBornStuckIfDue(d, now)
	}
}

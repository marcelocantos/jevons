// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/handover"
)

// fakeOverseerMigrator records what the server asked of the registry half.
type fakeOverseerMigrator struct {
	mu        sync.Mutex
	prepared  []string // "name→provider(force)"
	pending   handover.Pending
	prepErr   error
	delivered bool
}

func (m *fakeOverseerMigrator) PrepareMigration(name string, to claudia.Provider, force bool) (handover.Pending, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prepared = append(m.prepared, name+"→"+string(to))
	if m.prepErr != nil {
		return handover.Pending{}, m.prepErr
	}
	m.pending = handover.Pending{
		Agent: name, From: "grok", To: string(to),
		TranscriptPath: "/Users/x/.grok/sessions/abc/chat_history.jsonl",
	}
	return m.pending, nil
}

func (m *fakeOverseerMigrator) CompleteThinBrief(p handover.Pending) (handover.Pending, error) {
	return p, nil
}

// PrepareCompaction is the same rotation with the provider held constant
// (🎯T392.1) — the fake records it distinctly so a test can tell a context
// rotation from a backend switch.
func (m *fakeOverseerMigrator) PrepareCompaction(name string, force bool) (handover.Pending, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.prepared = append(m.prepared, name+"↻compact")
	if m.prepErr != nil {
		return handover.Pending{}, m.prepErr
	}
	m.pending = handover.Pending{
		Agent: name, From: "grok", To: "grok",
		TranscriptPath: "/Users/x/.grok/sessions/abc/chat_history.jsonl",
	}
	return m.pending, nil
}

func (m *fakeOverseerMigrator) PendingHandover(string) (handover.Pending, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pending, m.pending.TranscriptPath != "" && !m.pending.Delivered, nil
}

func (m *fakeOverseerMigrator) MarkHandoverDelivered(string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.delivered = true
	m.pending.Delivered = true
	return nil
}

func (m *fakeOverseerMigrator) wasDelivered() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.delivered
}

// TestMigrateOverseerRequiresWiring: without a registry or a migrator the
// call must refuse, not half-rotate the owner's CEO agent (🎯T285).
func TestCompactOverseerWithdrawn(t *testing.T) {
	s := New("test", t.TempDir())
	if _, err := s.CompactOverseer(false); err == nil {
		t.Fatal("CompactOverseer succeeded — remint is withdrawn")
	} else if !strings.Contains(err.Error(), "withdrawn") {
		t.Fatalf("CompactOverseer err=%v want withdrawn", err)
	}
}

func TestMigrateOverseerRequiresWiring(t *testing.T) {
	s := New("test", t.TempDir())
	s.SetOverseerName("jevons")

	if _, err := s.MigrateOverseer(claudia.ProviderClaude, false); err == nil {
		t.Fatal("migration proceeded with no registry")
	}
	if _, err := s.MigrateOverseer("", false); err == nil {
		t.Fatal("empty target provider was accepted")
	}
}

// TestMigrateOverseerRefusesWhenPrepareFails: a refusal from the registry
// half (no transcript to hand over) must surface, and the server must not
// claim success.
func TestMigrateOverseerRefusesWhenPrepareFails(t *testing.T) {
	s := New("test", t.TempDir())
	s.SetOverseerName("jevons")
	reg, err := claudia.NewRegistry(t.TempDir() + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	s.SetRegistry(reg)
	mig := &fakeOverseerMigrator{prepErr: errRefused{}}
	s.SetOverseerMigrator(mig)

	if _, err := s.MigrateOverseer(claudia.ProviderClaude, false); err == nil {
		t.Fatal("server reported success despite a refused rotation")
	}
	if len(mig.prepared) != 1 || !strings.Contains(mig.prepared[0], "jevons→claude") {
		t.Fatalf("prepare calls = %v", mig.prepared)
	}
}

type errRefused struct{}

func (errRefused) Error() string { return "no transcript found for session" }

// waitFor polls until cond holds, so an asynchronous seed does not need a
// fixed sleep to be observed.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestResumePendingHandoverSeedsOnce: the pending record is the retry
// mechanism, so a successor must be seeded with its predecessor's
// transcript, and marked delivered exactly once (🎯T285).
func TestResumePendingHandoverSeedsOnce(t *testing.T) {
	s := New("test", t.TempDir())
	s.SetOverseerName("jevons")
	mig := &fakeOverseerMigrator{pending: handover.Pending{
		Agent: "jevons", From: "grok", To: "claude",
		TranscriptPath: "/Users/x/.grok/sessions/abc/chat_history.jsonl",
	}}
	s.SetOverseerMigrator(mig)

	var mu sync.Mutex
	var sent []string
	s.notifySender = func(text string) error {
		mu.Lock()
		defer mu.Unlock()
		sent = append(sent, text)
		return nil
	}

	s.ResumePendingHandover()
	waitFor(t, "handover marked delivered", mig.wasDelivered)

	mu.Lock()
	got := append([]string(nil), sent...)
	mu.Unlock()
	if len(got) != 1 {
		t.Fatalf("seeds sent = %d, want 1: %v", len(got), got)
	}
	low := strings.ToLower(got[0])
	if !strings.Contains(low, "provider switch") || !strings.Contains(low, "what was in flight") {
		t.Fatalf("seed is not brief-shaped: %s", got[0])
	}
	if strings.Contains(got[0], "chat_history.jsonl") || strings.Contains(low, "start at the end") {
		t.Fatalf("work seed still assigns a transcript walk: %s", got[0])
	}

	// A second attach must not re-seed: the record is now delivered.
	s.ResumePendingHandover()
	mu.Lock()
	n := len(sent)
	mu.Unlock()
	if n != 1 {
		t.Fatalf("delivered handover was seeded again: %d sends", n)
	}
}

// TestResumePendingHandoverStaysPendingOnFailure: a send that fails must
// leave the record undelivered so the next attach retries it — marking on a
// failed hand-off is how history gets silently lost (🎯T285).
func TestResumePendingHandoverStaysPendingOnFailure(t *testing.T) {
	s := New("test", t.TempDir())
	s.SetOverseerName("jevons")
	mig := &fakeOverseerMigrator{pending: handover.Pending{
		Agent: "jevons", From: "grok", To: "claude",
		TranscriptPath: "/Users/x/.grok/sessions/abc/chat_history.jsonl",
	}}
	s.SetOverseerMigrator(mig)

	var attempts int32
	s.notifySender = func(string) error {
		if atomic.AddInt32(&attempts, 1) == 1 {
			return errRefused{}
		}
		return nil
	}

	s.ResumePendingHandover()
	waitFor(t, "first seed attempt", func() bool { return atomic.LoadInt32(&attempts) == 1 })
	if mig.wasDelivered() {
		t.Fatal("handover marked delivered despite a failed send")
	}

	// The retry a later attach performs must succeed and mark it.
	waitFor(t, "retry to be accepted", func() bool {
		s.ResumePendingHandover()
		return mig.wasDelivered()
	})
}

// TestResumePendingHandoverNoopsWithoutWork: the ordinary attach path calls
// this on every launch, so nothing pending must cost nothing.
func TestResumePendingHandoverNoopsWithoutWork(t *testing.T) {
	s := New("test", t.TempDir())
	s.SetOverseerName("jevons")
	s.notifySender = func(string) error {
		t.Error("seeded with no pending handover")
		return nil
	}

	s.ResumePendingHandover() // no migrator wired
	s.SetOverseerMigrator(&fakeOverseerMigrator{})
	s.ResumePendingHandover() // migrator wired, nothing pending
	time.Sleep(20 * time.Millisecond)
}

// TestOverseerMigrateEndpointValidates: the owner-facing endpoint requires
// a provider and reports a refusal as a conflict rather than a success.
func TestOverseerMigrateEndpointValidates(t *testing.T) {
	s := New("test", t.TempDir())
	s.SetOverseerName("jevons")

	rec := httptest.NewRecorder()
	s.handleOverseerMigrate(rec, httptest.NewRequest(http.MethodPost, "/api/overseer/migrate", strings.NewReader(`{}`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing provider → %d, want 400", rec.Code)
	}

	rec = httptest.NewRecorder()
	s.handleOverseerMigrate(rec, httptest.NewRequest(http.MethodPost,
		"/api/overseer/migrate?provider=claude", strings.NewReader("")))
	if rec.Code != http.StatusConflict {
		t.Fatalf("unwired migration → %d, want 409", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if _, ok := body["error"]; !ok {
		t.Fatalf("refusal carries no error: %s", rec.Body.String())
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/handover"
)

// fakeOverseerMigrator records what the server asked of the registry half.
type fakeOverseerMigrator struct {
	prepared  []string // "name→provider(force)"
	pending   handover.Pending
	prepErr   error
	delivered bool
}

func (m *fakeOverseerMigrator) PrepareMigration(name string, to claudia.Provider, force bool) (handover.Pending, error) {
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

func (m *fakeOverseerMigrator) PendingHandover(string) (handover.Pending, bool, error) {
	return m.pending, m.pending.TranscriptPath != "", nil
}

func (m *fakeOverseerMigrator) MarkHandoverDelivered(string) error {
	m.delivered = true
	return nil
}

// TestMigrateOverseerRequiresWiring: without a registry or a migrator the
// call must refuse, not half-rotate the owner's CEO agent (🎯T285).
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

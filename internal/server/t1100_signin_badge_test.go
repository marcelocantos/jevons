// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/fleet"
)

// 🎯T1100: a seat whose last start failed on a missing conversation is not
// counted for Sign in and auth/recover refuses before any login flow.
func TestT1100MissingConversationExcludedFromSignIn(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	const name = "jv-t1020-auto"
	refusal := errors.New("existing conversation required for this seat and session; refusing to mint a replacement session")
	if err := reg.Register(claudia.AgentDef{Name: name, WorkDir: t.TempDir(), Provider: "anthropic", SessionID: "sid"}); err != nil {
		t.Fatal(err)
	}
	fleet.RecordRehydrateFailure(name, refusal)
	t.Cleanup(func() { fleet.RecordRehydrateFailure(name, nil) })

	def := claudia.AgentDef{Name: name, Provider: "anthropic"}
	if reauthablePlanFailure(def.Provider, def) {
		t.Fatal("missing-conversation seat was offered Sign in")
	}
	if !strings.Contains(fleet.RehydrateHealth(def), "stored conversation is missing") {
		t.Fatalf("rehydrate = %q, want missing-conversation owner reason", fleet.RehydrateHealth(def))
	}

	s := New("test", t.TempDir())
	s.SetRegistry(reg)
	called := 0
	s.authRecover = func(_ context.Context, _ claudia.Provider) error {
		called++
		return nil
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/agents/"+name+"/auth/recover", nil))
	if w.Code != http.StatusConflict || called != 0 {
		t.Fatalf("auth/recover status=%d called=%d body=%s", w.Code, called, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "stored conversation is missing") {
		t.Fatalf("body=%s", w.Body.String())
	}
}

func TestT1100AuthSeatStillOffersSignIn(t *testing.T) {
	const name = "auth-seat"
	fleet.RecordRehydrateFailure(name, agenterr.WithCause(agenterr.CauseAuth, errors.New("anthropic refresh failed: invalid_grant")))
	t.Cleanup(func() { fleet.RecordRehydrateFailure(name, nil) })
	def := claudia.AgentDef{Name: name, Provider: "anthropic"}
	if !reauthablePlanFailure(def.Provider, def) {
		t.Fatal("auth failure seat should offer Sign in")
	}
}

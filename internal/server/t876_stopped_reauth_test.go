// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
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

// 🎯T876: a stopped seat whose plan login failed must surface the broker's auth
// text (not a later Keychain startup fallback) and offer per-seat Reauth.
func TestT876StoppedSeatShowsTruthfulAuthFailureAndReauth(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	const name = "t876-stopped"
	if err := reg.Register(claudia.AgentDef{Name: name, WorkDir: t.TempDir(), Provider: "anthropic", SessionID: "sid"}); err != nil {
		t.Fatal(err)
	}
	auth := agenterr.WithCause(agenterr.CauseAuth, errors.New("omp: anthropic refresh failed: invalid_grant"))
	fleet.RecordRehydrateFailure(name, auth)
	t.Cleanup(func() { fleet.RecordRehydrateFailure(name, nil) })
	// A generic startup refusal must not replace the typed auth failure on the row.
	fleet.RecordRehydrateFailure(name, errors.New("omp: keychain was not read at startup"))

	s := New("test", t.TempDir())
	s.SetRegistry(reg)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/agents", nil))
	var rows []struct {
		Name            string `json:"name"`
		Running         bool   `json:"running"`
		Rehydrate       string `json:"rehydrate"`
		ReauthAvailable bool   `json:"reauth_available"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &rows); err != nil {
		t.Fatalf("agents: %v: %s", err, w.Body.String())
	}
	var found bool
	for _, r := range rows {
		if r.Name != name {
			continue
		}
		found = true
		if r.Running {
			t.Fatal("seat should be stopped")
		}
		if !strings.Contains(r.Rehydrate, "invalid_grant") || strings.Contains(r.Rehydrate, "keychain") {
			t.Fatalf("rehydrate = %q, want broker invalid_grant not keychain fallback", r.Rehydrate)
		}
		if !r.ReauthAvailable {
			t.Fatal("stopped auth failure should offer Reauth")
		}
	}
	if !found {
		t.Fatalf("seat %q missing from agents list: %s", name, w.Body.String())
	}
}

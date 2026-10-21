// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleet"
)

func TestAuthRecoverOnlyOffersStoppedPlanFailures(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider claudia.Provider
		err      error
		want     bool
	}{
		{"auth-offer-anthropic", "anthropic", errors.New("anthropic refresh failed: invalid_grant"), true},
		{"auth-offer-openai-codex", "openai-codex", errors.New("omp: keychain was not read at startup"), true},
		{"auth-offer-grok", "grok", errors.New("no xai-oauth login"), true},
		// 🎯T1079: a codex row is the openai-codex sidecar seat; its login is
		// the plan's, recovered the same way.
		{"auth-offer-codex", "codex", errors.New("invalid_grant"), true},
		{"auth-offer-resumable", "anthropic", nil, false},
		{"auth-offer-exited", "anthropic", errors.New("tool process exited"), true},
		{"auth-offer-ollama", "ollama", errors.New("invalid_grant"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.err != nil {
				fleet.RecordRehydrateFailure(tc.name, tc.err)
				t.Cleanup(func() { fleet.RecordRehydrateFailure(tc.name, nil) })
			}
			def := claudia.AgentDef{Name: tc.name, Provider: tc.provider}
			if got := reauthablePlanFailure(tc.provider, def); got != tc.want {
				t.Errorf("provider=%s err=%v: got %v want %v", tc.provider, tc.err, got, tc.want)
			}
			if tc.want {
				health := fleet.RehydrateHealth(def)
				if !strings.HasPrefix(health, "broken:") {
					t.Errorf("display = %q, want the broken prefix", health)
				}
			}
		})
	}
}

func TestAuthRecoverRechecksSeatAndReportsClaudiaFailure(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	const name = "auth-recover-test-seat"
	if err := reg.Register(claudia.AgentDef{Name: name, WorkDir: t.TempDir(), Provider: "anthropic", SessionID: "sid"}); err != nil {
		t.Fatal(err)
	}
	s := New("test", t.TempDir())
	s.SetRegistry(reg)
	called := 0
	s.authRecover = func(context.Context, claudia.Provider) error {
		called++
		return errors.New("sign-in was cancelled")
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	request := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/agents/"+name+"/auth/recover", nil))
		return w
	}
	if w := request(); w.Code != http.StatusConflict || called != 0 {
		t.Fatalf("stale click status=%d called=%d body=%s", w.Code, called, w.Body.String())
	}
	fleet.RecordRehydrateFailure(name, errors.New("anthropic refresh failed: invalid_grant"))
	t.Cleanup(func() { fleet.RecordRehydrateFailure(name, nil) })
	if w := request(); w.Code != http.StatusBadGateway || called != 1 || !strings.Contains(w.Body.String(), "sign-in was cancelled") {
		t.Fatalf("recovery status=%d called=%d body=%s", w.Code, called, w.Body.String())
	}
	if got := fleet.RehydrateHealth(*reg.Def(name)); !strings.Contains(got, "sign-in was cancelled") {
		t.Fatalf("failure was not updated: %q", got)
	} else if !reauthablePlanFailure("anthropic", *reg.Def(name)) {
		t.Fatalf("failed sign-in hid the retry action: %q", got)
	}
}

func TestAuthRecoverRelaunchesOnlyTheStoppedSeatAndClearsItsFailure(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	const affected = "auth-recover-affected"
	const healthy = "auth-recover-healthy"
	for _, name := range []string{affected, healthy} {
		if err := reg.Register(claudia.AgentDef{Name: name, WorkDir: t.TempDir(), Provider: "anthropic", SessionID: name + "-sid"}); err != nil {
			t.Fatal(err)
		}
	}
	var launched []string
	reg.SetLaunchers(&claudia.RegistryLaunchers{Start: func(_ context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		launched = append(launched, cfg.Name)
		return claudia.NewStubAgent(nil), nil
	}})
	if _, err := reg.Launch(healthy); err != nil {
		t.Fatal(err)
	}
	fleet.RecordRehydrateFailure(affected, errors.New("invalid_grant"))
	t.Cleanup(func() { fleet.RecordRehydrateFailure(affected, nil) })
	s := New("test", t.TempDir())
	s.SetRegistry(reg)
	// 🎯T945: the running seat's refusal on the same plan is answered too.
	var cleared []string
	s.SetPlanAuthFailure(
		func(name string) bool { return name == healthy && len(cleared) == 0 },
		func(names []string) { cleared = append(cleared, names...) },
	)
	recovered := 0
	s.authRecover = func(_ context.Context, provider claudia.Provider) error {
		recovered++
		if provider != "anthropic" {
			t.Fatalf("provider = %q", provider)
		}
		return nil
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/agents/"+affected+"/auth/recover", nil))
	if !slices.Contains(cleared, healthy) {
		t.Fatalf("cleared = %v, want the running seat on the repaired plan", cleared)
	}
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"running"`) {
		t.Fatalf("recovery status=%d body=%s", w.Code, w.Body.String())
	}
	if recovered != 1 || len(launched) != 2 || launched[0] != healthy || launched[1] != affected ||
		!reg.Get(healthy).Alive() || !reg.Get(affected).Alive() {
		t.Fatalf("recovered=%d launched=%v healthy=%v affected=%v", recovered, launched, reg.Get(healthy), reg.Get(affected))
	}
	if got := fleet.RehydrateHealth(*reg.Def(affected)); got != "resumable" {
		t.Fatalf("stale failure after successful relaunch = %q", got)
	}
}

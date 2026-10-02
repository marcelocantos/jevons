// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/planusage"
)

func TestPlanDestinationAuthRecoveryKeepsRunningSourceAndRetriesAfterCancellation(t *testing.T) {
	decision := planusage.PlanAction{
		Name: "jevons", From: "grok", To: "claude", Action: planusage.SeatMigrate,
		Reason: "weekly hot or exhausted", Execution: "failed",
		Failure: "Migrate: context transfer: anthropic refresh failed: invalid_grant",
		Author:  claudia.DecisionAuthor,
	}
	s := New("test", t.TempDir())
	s.SetPlanDecisions(func() []planusage.PlanAction { return []planusage.PlanAction{decision} })
	retried := make(chan struct{}, 1)
	s.SetPlanSweep(func() any { retried <- struct{}{}; return nil })
	var calls []claudia.Provider
	s.authRecover = func(_ context.Context, provider claudia.Provider) error {
		calls = append(calls, provider)
		if len(calls) == 1 {
			return errors.New("sign-in was cancelled")
		}
		return nil
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	request := func(method, path string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}

	w := request(http.MethodGet, "/api/plan-usage/decisions")
	var shown []planusage.PlanAction
	if err := json.Unmarshal(w.Body.Bytes(), &shown); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || len(shown) != 1 || !shown[0].ReauthAvailable ||
		shown[0].From != "grok" || shown[0].To != "claude" || shown[0].Author != claudia.DecisionAuthor {
		t.Fatalf("owner decision = %+v, status=%d", shown, w.Code)
	}
	if w := request(http.MethodPost, "/api/plan-usage/auth/recover/ollama"); w.Code != http.StatusBadRequest {
		t.Fatalf("unsupported provider status=%d body=%s", w.Code, w.Body.String())
	}
	decision.Failure = "destination process exited without authentication error"
	if w := request(http.MethodPost, "/api/plan-usage/auth/recover/claude"); w.Code != http.StatusConflict {
		t.Fatalf("unrelated failure status=%d body=%s", w.Code, w.Body.String())
	}
	decision.Failure = "anthropic refresh failed: invalid_grant"
	if w := request(http.MethodPost, "/api/plan-usage/auth/recover/claude"); w.Code != http.StatusBadGateway ||
		!strings.Contains(w.Body.String(), "sign-in was cancelled") {
		t.Fatalf("cancelled sign-in status=%d body=%s", w.Code, w.Body.String())
	}
	if w := request(http.MethodGet, "/api/plan-usage/decisions"); !strings.Contains(w.Body.String(), `"ReauthAvailable":true`) {
		t.Fatalf("cancelled sign-in hid retry: %s", w.Body.String())
	}
	select {
	case <-retried:
		t.Fatal("cancelled sign-in retried the migration")
	default:
	}
	w = request(http.MethodPost, "/api/plan-usage/auth/recover/claude")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"migration":"retry_pending"`) {
		t.Fatalf("recovered status=%d body=%s", w.Code, w.Body.String())
	}
	if len(calls) != 2 || calls[0] != "anthropic" || calls[1] != "anthropic" {
		t.Fatalf("Claudia recovery calls = %v, want only destination anthropic twice", calls)
	}
	select {
	case <-retried:
	case <-time.After(2 * time.Second):
		t.Fatal("successful sign-in did not retry the current migration")
	}
}

func TestPlanDestinationAuthRecoveryOnlyOffersRejectedLogins(t *testing.T) {
	for _, tc := range []struct {
		name   string
		action planusage.PlanAction
		want   bool
	}{
		{"rejected refresh", planusage.PlanAction{To: "claude", Action: planusage.SeatMigrate, Execution: "failed", Failure: "OAuthError: invalid_grant"}, true},
		{"missed keychain", planusage.PlanAction{To: "cursor", Action: planusage.SeatMigrate, Execution: "failed", Failure: "keychain was not read at startup"}, true},
		{"healthy destination", planusage.PlanAction{To: "claude", Action: planusage.SeatMigrate, Execution: "migrated", Failure: ""}, false},
		{"pending move", planusage.PlanAction{To: "claude", Action: planusage.SeatMigrate, Execution: "pending", Failure: "invalid_grant"}, false},
		{"unrelated error", planusage.PlanAction{To: "claude", Action: planusage.SeatMigrate, Execution: "failed", Failure: "destination process exited"}, false},
		{"non-plan provider", planusage.PlanAction{To: "ollama", Action: planusage.SeatMigrate, Execution: "failed", Failure: "invalid_grant"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := destinationAuthRecoverable(tc.action); got != tc.want {
				t.Fatalf("recoverable=%v want %v for %+v", got, tc.want, tc.action)
			}
		})
	}
}

func TestPlanDestinationAuthRecoveryRequiresMigrationRetry(t *testing.T) {
	s := New("test", t.TempDir())
	s.SetPlanDecisions(func() []planusage.PlanAction {
		return []planusage.PlanAction{{
			Name: "worker", From: "grok", To: "claude", Action: planusage.SeatMigrate,
			Execution: "failed", Failure: "invalid_grant",
		}}
	})
	called := false
	s.authRecover = func(context.Context, claudia.Provider) error { called = true; return nil }
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/plan-usage/auth/recover/claude", nil))
	if w.Code != http.StatusServiceUnavailable || called {
		t.Fatalf("unwired recovery status=%d called=%v body=%s", w.Code, called, w.Body.String())
	}
}

func TestPlanDestinationAuthRecoveryUsesPinnedClaudiaBinary(t *testing.T) {
	t.Setenv("CLAUDIA_BIN", "/usr/bin/false")
	err := runClaudiaAuthRecover(context.Background(), "anthropic")
	if err == nil || !strings.Contains(err.Error(), "exit status 1") {
		t.Fatalf("pinned Claudia client was not run: %v", err)
	}
}

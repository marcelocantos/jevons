// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/planusage"
)

// A successful destination reauth answers the failures that offered it at
// once, and revives seats that broke on the same plan login; a cancelled
// sign-in does neither.
func TestDestinationReauthMarksRetryAndRevivesPlanPeers(t *testing.T) {
	decision := planusage.PlanAction{
		Name: "jevons", From: "grok", To: "claude", Action: claudia.SeatMigrate,
		Execution: "failed", Failure: "anthropic refresh failed: invalid_grant",
		Author: claudia.DecisionAuthor,
	}
	s := New("test", t.TempDir())
	s.SetPlanDecisions(func() []planusage.PlanAction { return []planusage.PlanAction{decision} })
	s.SetPlanSweep(func() any { return nil })
	var marked []claudia.Provider
	s.SetPlanRetryAfterReauth(func(p claudia.Provider) { marked = append(marked, p) })
	revived := make(chan claudia.Provider, 2)
	s.SetPlanAuthRevive(func(p claudia.Provider, skip string) { revived <- p })
	fail := true
	s.authRecover = func(context.Context, claudia.Provider) error {
		if fail {
			return errors.New("sign-in was cancelled")
		}
		return nil
	}
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	post := func() int {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/plan-usage/auth/recover/claude", nil))
		return w.Code
	}

	if code := post(); code != http.StatusBadGateway {
		t.Fatalf("cancelled sign-in status=%d", code)
	}
	if len(marked) != 0 {
		t.Fatalf("cancelled sign-in marked a retry: %v", marked)
	}
	select {
	case p := <-revived:
		t.Fatalf("cancelled sign-in revived %s seats", p)
	case <-time.After(50 * time.Millisecond):
	}

	fail = false
	if code := post(); code != http.StatusOK {
		t.Fatalf("recovered status=%d", code)
	}
	if len(marked) != 1 || marked[0] != "anthropic" {
		t.Fatalf("retry marker = %v, want [anthropic]", marked)
	}
	select {
	case p := <-revived:
		if p != "anthropic" {
			t.Fatalf("revived provider = %s", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("recovered sign-in did not revive plan peers")
	}
}

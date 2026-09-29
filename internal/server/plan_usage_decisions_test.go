// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/planusage"
)

func TestPlanUsageDecisionsShowsClaudiaDeferral(t *testing.T) {
	s := &Server{}
	s.SetPlanDecisions(func() []planusage.PlanAction {
		return []planusage.PlanAction{{
			Name: "jevons", From: "grok", Action: planusage.SeatDefer,
			Reason: "destination plan readings incomplete or stale", Author: claudia.DecisionAuthor,
		}}
	})
	rec := httptest.NewRecorder()
	s.handlePlanUsageDecisions(rec, httptest.NewRequest(http.MethodGet, "/api/plan-usage/decisions", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("decision status = %d: %s", rec.Code, rec.Body.String())
	}
	var decisions []planusage.PlanAction
	if err := json.Unmarshal(rec.Body.Bytes(), &decisions); err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 1 || decisions[0].Action != planusage.SeatDefer ||
		decisions[0].Author != claudia.DecisionAuthor || decisions[0].Reason == "" {
		t.Fatalf("decision missing author or reason: %+v", decisions)
	}
}

func TestPlanUsageDecisionsUnavailableUntilPlanFeedArrives(t *testing.T) {
	s := &Server{}
	s.SetPlanDecisions(func() []planusage.PlanAction { return nil })
	rec := httptest.NewRecorder()
	s.handlePlanUsageDecisions(rec, httptest.NewRequest(http.MethodGet, "/api/plan-usage/decisions", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing feed status = %d: %s", rec.Code, rec.Body.String())
	}
}

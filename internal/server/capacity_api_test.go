// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/marcelocantos/jevons/internal/capacity"
)

// GET /api/capacity is the owner-visible answer to "why is background work
// quiet?", so it must serve the governor's real status.
func TestCapacityAPIServesGovernorStatus(t *testing.T) {
	gov := capacity.NewGovernor(capacity.GovernorArgs{
		Snapshot: func() capacity.Snapshot {
			return capacity.Snapshot{
				Billable: true, Accounting: "list_price",
				SpentTodayUSD: 400, ProjectedTodayUSD: 600, DailyBudgetUSD: 500,
				ActiveSessions: 4, MaxSessions: 20,
			}
		},
	})
	s := &Server{}
	s.SetCapacitySource(func() any { return gov.Status() })

	rec := httptest.NewRecorder()
	s.handleCapacity(rec, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Assessment struct {
			Pressure  string `json:"pressure"`
			OwnerOnly bool   `json:"owner_only"`
		} `json:"assessment"`
		Plan []struct {
			Class   string `json:"class"`
			Verdict string `json:"verdict"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	// Projected spend is past the daily budget: nothing but owner and Build
	// work fits, and the plan must say so for every background class.
	if body.Assessment.Pressure != "critical" || !body.Assessment.OwnerOnly {
		t.Fatalf("assessment = %+v, want critical/owner-only", body.Assessment)
	}
	if len(body.Plan) == 0 {
		t.Fatal("plan is empty — the endpoint must say what each class would get")
	}
	for _, row := range body.Plan {
		if row.Verdict != string(capacity.VerdictDefer) {
			t.Errorf("%s = %s, want defer under critical pressure", row.Class, row.Verdict)
		}
	}
}

// Unwired reports disabled rather than 500 — the same honesty shape as
// GET /api/cost when the budget subsystem is off.
func TestCapacityAPIUnwiredReportsDisabled(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handleCapacity(rec, httptest.NewRequest(http.MethodGet, "/api/capacity", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v (%s)", err, rec.Body.String())
	}
	if body["disabled"] != true {
		t.Fatalf("body = %v, want disabled:true", body)
	}
}

// The owner-turn signal is what makes ambient work yield the seat (🎯T291);
// it must be false unless a prompt is actually in flight for the owner.
func TestOwnerTurnInFlightTracksTheOverseerPrompt(t *testing.T) {
	s := &Server{}
	if s.OwnerTurnInFlight() {
		t.Fatal("idle server reports an owner turn in flight")
	}
	s.mu.Lock()
	s.waiting, s.overseerOwnerTurn = true, false
	s.mu.Unlock()
	if s.OwnerTurnInFlight() {
		t.Fatal("a fleet-driven turn must not count as an owner turn")
	}
	s.mu.Lock()
	s.overseerOwnerTurn = true
	s.mu.Unlock()
	if !s.OwnerTurnInFlight() {
		t.Fatal("an in-flight owner prompt must be visible to capacity admission")
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T924: the plan bar offers Reauth for any plan whose login the broker
// reports unhealthy, with no failed migration and no broken seat. The
// recovery runs only on that request, and a healthy plan is refused.
func TestT924ReauthIsOfferedForAnyUnhealthyPlanLogin(t *testing.T) {
	s := New("test", t.TempDir())
	reads := 0
	s.authStatus = func(context.Context) ([]PlanAuth, error) {
		reads++
		cursor := "missing"
		if reads > 1 {
			cursor = planAuthOK
		}
		return []PlanAuth{
			{Provider: "anthropic", State: planAuthOK},
			{Provider: "cursor", State: cursor},
			{Provider: "xai-oauth", State: "rejected", Detail: "invalid_grant"},
		}, nil
	}
	var calls []claudia.Provider
	s.authRecover = func(_ context.Context, p claudia.Provider) error { calls = append(calls, p); return nil }
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	request := func(method, path string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}

	w := request(http.MethodGet, "/api/plan-usage/auth")
	var body struct{ Plans []PlanAuth }
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != http.StatusOK || len(body.Plans) != 3 ||
		body.Plans[1].State != "missing" || body.Plans[2].Detail != "invalid_grant" {
		t.Fatalf("status=%d body=%s err=%v", w.Code, w.Body.String(), err)
	}
	if len(calls) != 0 {
		t.Fatalf("reading plan health started a recovery: %v", calls)
	}
	if w := request(http.MethodPost, "/api/plan-usage/auth/recover/anthropic"); w.Code != http.StatusConflict {
		t.Fatalf("healthy plan status=%d body=%s", w.Code, w.Body.String())
	}
	w = request(http.MethodPost, "/api/plan-usage/auth/recover/cursor")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"recovered"`) ||
		strings.Contains(w.Body.String(), "retry_pending") {
		t.Fatalf("missing plan status=%d body=%s", w.Code, w.Body.String())
	}
	if len(calls) != 1 || calls[0] != "cursor" {
		t.Fatalf("recovery calls = %v, want cursor once", calls)
	}
	// The repaired login shows at once, not after the cache ages out.
	w = request(http.MethodGet, "/api/plan-usage/auth")
	if !strings.Contains(w.Body.String(), `"provider":"cursor","state":"ok"`) {
		t.Fatalf("after reauth: %s", w.Body.String())
	}
}

func TestT924PlanHealthIsNotReadFromTheRealBrokerUnderTest(t *testing.T) {
	s := New("test", t.TempDir())
	if _, err := s.planAuthStatus(context.Background()); err == nil {
		t.Fatal("an unwired test read plan health from a broker")
	}
}

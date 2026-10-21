// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/delivery"
	"github.com/marcelocantos/jevons/internal/ownercockpit"
)

func browserOwnerPOST(s *Server, path, body string) (*http.Request, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	if s.ownerCockpit != nil {
		req.AddCookie(s.ownerCockpit.TestCookie())
	}
	return req, httptest.NewRecorder()
}

func TestT1129FleetBrowserOwnerSendRefusedAndLogged(t *testing.T) {
	s := New("test", t.TempDir())
	s.SetAgentSendOriginHook(func(name, text, origin string, mode delivery.Mode) (AgentSendOutcome, error) {
		t.Fatal("deliver hook must not run when owner send is refused")
		return AgentSendOutcome{}, nil
	})
	req := httptest.NewRequest(http.MethodPost, "/api/agents/jevons/send", strings.NewReader(`{"text":"probe"}`))
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set(ownercockpit.HeaderFleetBrowser, "1")
	req.SetPathValue("name", "jevons")
	rr := httptest.NewRecorder()
	s.handleAgentSend(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestT1129BrowserOwnerSendWithoutSessionRefused(t *testing.T) {
	s := New("test", t.TempDir())
	req := httptest.NewRequest(http.MethodPost, "/api/agents/jevons/send", strings.NewReader(`{"text":"probe"}`))
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.SetPathValue("name", "jevons")
	rr := httptest.NewRecorder()
	s.handleAgentSend(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestT1129BrowserOwnerSendWithSessionLands(t *testing.T) {
	s := New("test", t.TempDir())
	var got bool
	s.SetAgentSendOriginHook(func(name, text, origin string, mode delivery.Mode) (AgentSendOutcome, error) {
		got = true
		return AgentSendOutcome{Status: "sent"}, nil
	})
	req, rr := browserOwnerPOST(s, "/api/agents/jevons/send", `{"text":"hello owner"}`)
	req.SetPathValue("name", "jevons")
	s.handleAgentSend(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if !got {
		t.Fatal("owner send did not reach deliver hook")
	}
}

func TestT1129OwnerCockpitBootstrapSetsCookie(t *testing.T) {
	s := New("test", t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "/api/owner-cockpit/session", nil)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rr := httptest.NewRecorder()
	s.handleOwnerCockpitSession(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("bootstrap status=%d", rr.Code)
	}
	c := rr.Result().Cookies()
	if len(c) == 0 || c[0].Name != ownercockpit.CookieName {
		t.Fatalf("cookies=%v", c)
	}
}

func TestT1129AgentOriginSendBypassesOwnerGate(t *testing.T) {
	s := New("test", t.TempDir())
	var origin string
	s.SetAgentSendOriginHook(func(name, text, o string, mode delivery.Mode) (AgentSendOutcome, error) {
		origin = o
		return AgentSendOutcome{Status: "sent"}, nil
	})
	req := httptest.NewRequest(http.MethodPost, "/api/agents/jevons/send", strings.NewReader(`{"text":"note","origin":"agent"}`))
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set(ownercockpit.HeaderFleetBrowser, "1")
	req.SetPathValue("name", "jevons")
	rr := httptest.NewRecorder()
	s.handleAgentSend(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp agentSendResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if origin != sendOriginAgent {
		t.Fatalf("origin=%q", origin)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ownercockpit

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func browserReq() *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/agents/jevons/send", nil)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	return r
}

func TestAuthorizeOwnerSendFleetBrowserRefused(t *testing.T) {
	g := NewGate()
	r := browserReq()
	r.Header.Set(HeaderFleetBrowser, "1")
	err := g.AuthorizeOwnerSend(r, "owner")
	if err == nil {
		t.Fatal("fleet browser owner send must be refused")
	}
	if !strings.Contains(err.Error(), "fleet browser") {
		t.Fatalf("error = %v", err)
	}
}

func TestAuthorizeOwnerSendMissingCookieRefused(t *testing.T) {
	g := NewGate()
	if err := g.AuthorizeOwnerSend(browserReq(), "owner"); err == nil {
		t.Fatal("browser owner send without cookie must be refused")
	}
}

func TestAuthorizeOwnerSendWithCookieOK(t *testing.T) {
	g := NewGate()
	r := browserReq()
	r.AddCookie(g.TestCookie())
	if err := g.AuthorizeOwnerSend(r, "owner"); err != nil {
		t.Fatalf("authorized owner send: %v", err)
	}
}

func TestAuthorizeOwnerSendAgentOriginOK(t *testing.T) {
	g := NewGate()
	r := browserReq()
	r.Header.Set(HeaderFleetBrowser, "1")
	if err := g.AuthorizeOwnerSend(r, "agent"); err != nil {
		t.Fatalf("agent origin bypasses owner gate: %v", err)
	}
}

func TestAuthorizeOwnerSendHeaderlessCLI(t *testing.T) {
	g := NewGate()
	if err := g.AuthorizeOwnerSend(httptest.NewRequest(http.MethodPost, "/api/agents/jevons/send", nil), "owner"); err != nil {
		t.Fatalf("header-less CLI should not require cookie: %v", err)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 🎯T182: POST /api/agents/{name}/send is the product path frontier play uses.

func TestHandleAgentSendDeliversViaHook(t *testing.T) {
	s := New("test", t.TempDir())
	var gotName, gotText string
	s.SetAgentSendHook(func(name, text string) (string, error) {
		gotName, gotText = name, text
		return "sent", nil
	})

	body := `{"text":"Start work on frontier target 🎯T182 — tight status/fan."}`
	req := httptest.NewRequest(http.MethodPost, "/api/agents/jevons-po/send", strings.NewReader(body))
	req.SetPathValue("name", "jevons-po")
	rr := httptest.NewRecorder()
	s.handleAgentSend(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	if gotName != "jevons-po" {
		t.Fatalf("name=%q", gotName)
	}
	if !strings.Contains(gotText, "T182") {
		t.Fatalf("text=%q", gotText)
	}
	var resp agentSendResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Name != "jevons-po" || resp.Status != "sent" {
		t.Fatalf("resp=%+v", resp)
	}
}

func TestHandleAgentSendRequiresText(t *testing.T) {
	s := New("test", t.TempDir())
	s.SetAgentSendHook(func(name, text string) (string, error) {
		t.Fatal("hook should not run")
		return "", nil
	})
	req := httptest.NewRequest(http.MethodPost, "/api/agents/jevons-po/send", bytes.NewReader([]byte(`{}`)))
	req.SetPathValue("name", "jevons-po")
	rr := httptest.NewRecorder()
	s.handleAgentSend(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rr.Code)
	}
}

func TestHandleAgentSendNotRegistered(t *testing.T) {
	s := New("test", t.TempDir())
	// No hook, no registry → not available / not registered path.
	req := httptest.NewRequest(http.MethodPost, "/api/agents/missing/send",
		strings.NewReader(`{"text":"hi"}`))
	req.SetPathValue("name", "missing")
	rr := httptest.NewRecorder()
	s.handleAgentSend(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestHandleAgentSendBusyConflict(t *testing.T) {
	s := New("test", t.TempDir())
	s.SetAgentSendHook(func(name, text string) (string, error) {
		return "", fmtBusy()
	})
	req := httptest.NewRequest(http.MethodPost, "/api/agents/jevons-po/send",
		strings.NewReader(`{"text":"kickoff"}`))
	req.SetPathValue("name", "jevons-po")
	rr := httptest.NewRecorder()
	s.handleAgentSend(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func fmtBusy() error {
	return errBusy{}
}

type errBusy struct{}

func (errBusy) Error() string { return "prompt already in flight" }

func TestAgentSendRouteRegistered(t *testing.T) {
	s := New("test", t.TempDir())
	var got string
	s.SetAgentSendHook(func(name, text string) (string, error) {
		got = name + ":" + text
		return "sent", nil
	})
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	res, err := http.Post(ts.URL+"/api/agents/jevons-po/send",
		"application/json",
		strings.NewReader(`{"text":"Start work on 🎯T182"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", res.StatusCode)
	}
	if !strings.HasPrefix(got, "jevons-po:") || !strings.Contains(got, "T182") {
		t.Fatalf("got=%q", got)
	}
}

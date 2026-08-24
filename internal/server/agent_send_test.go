// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"bytes"
	"encoding/json"
	"fmt"
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

func TestHandleAgentSendOverseerDownNacks(t *testing.T) {
	s := New("test", t.TempDir())
	s.overseerName = "jevons"
	req := httptest.NewRequest(http.MethodPost, "/api/agents/jevons/send",
		strings.NewReader(`{"text":"should nack"}`))
	req.SetPathValue("name", "jevons")
	rr := httptest.NewRecorder()
	s.handleAgentSend(rr, req)
	if rr.Code == http.StatusOK {
		t.Fatalf("down overseer reported delivered: %s", rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "overseer not running") {
		t.Fatalf("nack must name the down overseer: %s", rr.Body.String())
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
	// Without product hook queue: bare busy still maps to 409 (loud, not silent).
	// Production wires MCP DeliverAgentMessage → status "queued" (see QueuedOK).
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

// 🎯T275: product hook returns queued (busy) → HTTP 200, not silent drop.
func TestHandleAgentSendQueuedOK(t *testing.T) {
	s := New("test", t.TempDir())
	s.SetAgentSendHook(func(name, text string) (string, error) {
		if name != "jv-t275-worker" || text != "hello worker" {
			t.Fatalf("hook args name=%q text=%q", name, text)
		}
		return "queued", nil
	})
	req := httptest.NewRequest(http.MethodPost, "/api/agents/jv-t275-worker/send",
		strings.NewReader(`{"text":"hello worker"}`))
	req.SetPathValue("name", "jv-t275-worker")
	rr := httptest.NewRecorder()
	s.handleAgentSend(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp agentSendResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "queued" || resp.Name != "jv-t275-worker" {
		t.Fatalf("resp=%+v", resp)
	}
	if !strings.Contains(resp.Message, "queued") {
		t.Fatalf("message should mention queued: %q", resp.Message)
	}
}

// 🎯T237: provider failure is classified — not bare Internal error on the HTTP path.
func TestHandleAgentSendClassifiesInternalError(t *testing.T) {
	s := New("test", t.TempDir())
	s.SetAgentSendHook(func(name, text string) (string, error) {
		return "", fmt.Errorf("Internal error")
	})
	req := httptest.NewRequest(http.MethodPost, "/api/agents/jevons-po/send",
		strings.NewReader(`{"text":"kickoff"}`))
	req.SetPathValue("name", "jevons-po")
	rr := httptest.NewRecorder()
	s.handleAgentSend(rr, req)
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["failure_class"] != "backend_unavailable" {
		t.Fatalf("failure_class=%q body=%v", body["failure_class"], body)
	}
	if body["error"] == "Internal error" || !strings.Contains(body["error"], "backend_unavailable") {
		t.Fatalf("owner error still bare: %q", body["error"])
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

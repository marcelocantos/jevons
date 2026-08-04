// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

// 🎯T237: hermetic fixtures map representative error strings/codes → classes.
func TestClassifyTextFixtures(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want agenterr.Class
	}{
		// none
		{"", agenterr.ClassNone},
		{"   ", agenterr.ClassNone},
		{"Continuing mission work on T236", agenterr.ClassNone},
		{"done — SHA abc + tests green", agenterr.ClassNone},
		{"I'll implement the classifier next.", agenterr.ClassNone},
		{"go test ./internal/agenterr PASS", agenterr.ClassNone},
		// busy → none
		{"grok acp: prompt already in flight", agenterr.ClassNone},
		{"task abc is busy", agenterr.ClassNone},
		{"session busy; try again", agenterr.ClassNone},

		// backend_unavailable (Grok outage post-mortem class)
		{"Internal error", agenterr.ClassBackendUnavailable},
		{"INTERNAL ERROR", agenterr.ClassBackendUnavailable},
		{"internal error", agenterr.ClassBackendUnavailable},
		{"Internal", agenterr.ClassBackendUnavailable},
		{"acp session/prompt: Internal error", agenterr.ClassBackendUnavailable},
		{"service unavailable", agenterr.ClassBackendUnavailable},
		{"connection refused", agenterr.ClassBackendUnavailable},
		{"dial tcp: connection refused", agenterr.ClassBackendUnavailable},
		{"502 Bad Gateway", agenterr.ClassBackendUnavailable},
		{"503 Service Unavailable", agenterr.ClassBackendUnavailable},
		{"gateway timeout 504", agenterr.ClassBackendUnavailable},
		{"request timed out", agenterr.ClassBackendUnavailable},
		{"grok acp: connection closed waiting for session/prompt", agenterr.ClassBackendUnavailable},
		{"i/o timeout", agenterr.ClassBackendUnavailable},
		{"upstream overloaded", agenterr.ClassBackendUnavailable},

		// rate_limit
		{"rate limit exceeded", agenterr.ClassRateLimit},
		{"HTTP 429 Too Many Requests", agenterr.ClassRateLimit},
		{"429 Too Many Requests", agenterr.ClassRateLimit},
		{"quota exceeded for model", agenterr.ClassRateLimit},
		{"resource_exhausted: throttle", agenterr.ClassRateLimit},

		// auth
		{"unauthorized: invalid api key", agenterr.ClassAuth},
		{"401 Unauthorized", agenterr.ClassAuth},
		{"invalid API key", agenterr.ClassAuth},
		{"not signed in — run grok auth", agenterr.ClassAuth},
		{"authentication failed", agenterr.ClassAuth},
		{"403 Forbidden", agenterr.ClassAuth},

		// client_bug
		{"grok acp: no session", agenterr.ClassClientBug},
		{"grok acp: client closed", agenterr.ClassClientBug},
		{"grok acp: no transport", agenterr.ClassClientBug},
		{"invalid request: unknown method", agenterr.ClassClientBug},
		{"agent \"x\" is not registered", agenterr.ClassClientBug},
		{"name and text are required", agenterr.ClassClientBug},
		{"acp session/load abc: unknown session — existing conversation; refusing to mint a replacement session", agenterr.ClassClientBug},
		{"unknown agent provider \"xyz\"", agenterr.ClassClientBug},
		{"agent registry not available", agenterr.ClassClientBug},

		// unknown failure-shaped
		{"something failed mysteriously", agenterr.ClassUnknown},
		{"something failed unexpectedly", agenterr.ClassUnknown},
		{"unexpected error from harness", agenterr.ClassUnknown},
	}
	for _, tc := range cases {
		got := agenterr.ClassifyText(tc.in)
		if got != tc.want {
			t.Errorf("ClassifyText(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestClassifyErrorNilBusyAndInternal(t *testing.T) {
	t.Parallel()
	if agenterr.Classify(nil) != agenterr.ClassNone {
		t.Fatal("nil err → none")
	}
	if agenterr.Classify(fmt.Errorf("grok acp: prompt already in flight")) != agenterr.ClassNone {
		t.Fatal("busy must not be a failure class")
	}
	c := agenterr.Classify(fmt.Errorf("Internal error"))
	if c != agenterr.ClassBackendUnavailable {
		t.Fatalf("got %q", c)
	}
	c = agenterr.Classify(fmt.Errorf("acp session/prompt: Internal error"))
	if c != agenterr.ClassBackendUnavailable {
		t.Fatalf("got %q", c)
	}
}

func TestTransientBackend(t *testing.T) {
	t.Parallel()
	if !agenterr.TransientBackend(agenterr.ClassBackendUnavailable) {
		t.Fatal("backend should be transient")
	}
	if !agenterr.TransientBackend(agenterr.ClassRateLimit) {
		t.Fatal("rate_limit should be transient")
	}
	if !agenterr.TransientBackend(agenterr.ClassUnknown) {
		t.Fatal("unknown treated as re-pressure-worthy residual")
	}
	if agenterr.TransientBackend(agenterr.ClassClientBug) {
		t.Fatal("client_bug must not busy-loop as recovering cloud")
	}
	if agenterr.TransientBackend(agenterr.ClassAuth) {
		t.Fatal("auth must not auto-recover loop")
	}
	if agenterr.TransientBackend(agenterr.ClassNone) {
		t.Fatal("none is not transient")
	}
}

func TestOwnerCopyNotBareInternalError(t *testing.T) {
	t.Parallel()
	classes := []agenterr.Class{
		agenterr.ClassBackendUnavailable,
		agenterr.ClassRateLimit,
		agenterr.ClassAuth,
		agenterr.ClassClientBug,
		agenterr.ClassUnknown,
	}
	for _, c := range classes {
		copy := agenterr.OwnerCopy(c, "Internal error")
		if strings.TrimSpace(copy) == "Internal error" {
			t.Fatalf("%s OwnerCopy is bare Internal error", c)
		}
		if !strings.Contains(copy, c.String()) {
			t.Fatalf("%s OwnerCopy missing class code: %q", c, copy)
		}
		if c == agenterr.ClassBackendUnavailable && !strings.Contains(strings.ToLower(copy), "backend") {
			t.Fatalf("backend copy should mention backend: %q", copy)
		}
	}
}

func TestClassifyAndFormat(t *testing.T) {
	t.Parallel()
	class, msg := agenterr.ClassifyAndFormat(fmt.Errorf("Internal error"))
	if class != agenterr.ClassBackendUnavailable {
		t.Fatalf("class=%q", class)
	}
	if msg == "Internal error" || !strings.Contains(msg, "backend_unavailable") {
		t.Fatalf("msg=%q", msg)
	}
	class, msg = agenterr.ClassifyAndFormat(fmt.Errorf("prompt already in flight"))
	if class != agenterr.ClassNone || !strings.Contains(msg, "in flight") {
		t.Fatalf("busy: class=%q msg=%q", class, msg)
	}
}

func TestFailureFields(t *testing.T) {
	t.Parallel()
	f := agenterr.FailureFields(agenterr.ClassRateLimit, "429", map[string]any{"agent": "jv-t237"})
	if f["failure_class"] != "rate_limit" {
		t.Fatalf("%v", f)
	}
	if f["transient"] != true {
		t.Fatalf("rate_limit should be transient: %v", f)
	}
	if f["agent"] != "jv-t237" {
		t.Fatalf("%v", f)
	}
}

func TestIsFailure(t *testing.T) {
	t.Parallel()
	if agenterr.IsFailure(agenterr.ClassNone) {
		t.Fatal("none is not failure")
	}
	if !agenterr.IsFailure(agenterr.ClassUnknown) {
		t.Fatal("unknown is failure")
	}
}

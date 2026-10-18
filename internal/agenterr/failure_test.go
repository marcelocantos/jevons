// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package agenterr_test

import (
	"errors"
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
		{"codex app-server: turn already in flight", agenterr.ClassNone}, // real codex busy (🎯T766.4 replaced an invented phrase)

		// A sentence that only quotes a class is not that class.
		// "error" / "failed" still land in the generic unknown bucket.
		{"Internal error", agenterr.ClassUnknown},
		{"INTERNAL ERROR", agenterr.ClassUnknown},
		{"internal error", agenterr.ClassUnknown},
		{"Internal", agenterr.ClassNone},
		{"acp session/prompt: Internal error", agenterr.ClassUnknown},
		{"service unavailable", agenterr.ClassNone},
		{"connection refused", agenterr.ClassNone},
		{"dial tcp: connection refused", agenterr.ClassNone},
		{"502 Bad Gateway", agenterr.ClassNone},
		{"503 Service Unavailable", agenterr.ClassNone},
		{"gateway timeout 504", agenterr.ClassNone},
		{"request timed out", agenterr.ClassNone},
		{"grok acp: connection closed waiting for session/prompt", agenterr.ClassNone},
		{"i/o timeout", agenterr.ClassNone},
		{"upstream overloaded", agenterr.ClassNone},
		{"rate limit exceeded", agenterr.ClassNone},
		{"HTTP 429 Too Many Requests", agenterr.ClassNone},
		{"429 Too Many Requests", agenterr.ClassNone},
		{"quota exceeded for model", agenterr.ClassNone},
		{"resource_exhausted: throttle", agenterr.ClassNone},
		{"unauthorized: invalid api key", agenterr.ClassNone},
		{"401 Unauthorized", agenterr.ClassNone},
		{"invalid API key", agenterr.ClassNone},
		{"not signed in — run grok auth", agenterr.ClassNone},
		{"authentication failed", agenterr.ClassUnknown},
		{"403 Forbidden", agenterr.ClassNone},
		{"grok acp: no session", agenterr.ClassNone},
		{"grok acp: client closed", agenterr.ClassNone},
		{"grok acp: no transport", agenterr.ClassNone},
		{"invalid request: unknown method", agenterr.ClassNone},
		{"agent \"x\" is not registered", agenterr.ClassNone},
		{"name and text are required", agenterr.ClassNone},
		{"acp session/load abc: unknown session — existing conversation; refusing to mint a replacement session", agenterr.ClassNone},
		{"unknown agent provider \"xyz\"", agenterr.ClassNone},
		{"agent registry not available", agenterr.ClassNone},

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
		t.Fatal("rendered busy text is not a failure class")
	}
	if agenterr.Classify(causeStub{cause: agenterr.CauseTurnInFlight, text: "401 unauthorized"}) != agenterr.ClassNone {
		t.Fatal("typed busy was classified from its text")
	}
	if got := agenterr.Classify(fmt.Errorf("401 unauthorized")); got == agenterr.ClassAuth {
		t.Fatalf("untyped 401 classified as %q", got)
	}
	if got := agenterr.Classify(fmt.Errorf("Internal error")); got != agenterr.ClassUnknown {
		t.Fatalf("untyped internal error = %q, want unknown", got)
	}
}

func TestTypedProviderCauseClassifiesWrappedAndBare(t *testing.T) {
	t.Parallel()
	bare := errors.New("401 unauthorized")
	if got := agenterr.Classify(bare); got == agenterr.ClassAuth {
		t.Fatal("errors.New of an auth sentence is not ClassAuth")
	}
	caused := agenterr.WithCause(agenterr.CauseAuth, bare)
	if got := agenterr.Classify(caused); got != agenterr.ClassAuth {
		t.Fatalf("CauseAuth = %q", got)
	}
	if got := agenterr.Classify(fmt.Errorf("send: %w", caused)); got != agenterr.ClassAuth {
		t.Fatalf("wrapped CauseAuth = %q", got)
	}
	backend := agenterr.WithCause(agenterr.CauseBackendUnavailable, errors.New("Internal error"))
	if got := agenterr.Classify(backend); got != agenterr.ClassBackendUnavailable {
		t.Fatalf("CauseBackendUnavailable = %q", got)
	}
	unrelated := agenterr.WithCause(agenterr.CauseBackendUnavailable, errors.New("the fleet is fine"))
	if got := agenterr.Classify(unrelated); got != agenterr.ClassBackendUnavailable {
		t.Fatalf("unrelated text = %q", got)
	}
	if got := agenterr.Classify(fmt.Errorf("wrap: %w", unrelated)); got != agenterr.ClassBackendUnavailable {
		t.Fatalf("wrapped unrelated = %q", got)
	}
	if got := agenterr.Classify(agenterr.WithCause(agenterr.CauseRateLimit, errors.New("slow down"))); got != agenterr.ClassRateLimit {
		t.Fatalf("CauseRateLimit = %q", got)
	}
	if got := agenterr.Classify(agenterr.WithCause(agenterr.CauseSpendWall, errors.New("slow down"))); got != agenterr.ClassSpendWall {
		t.Fatalf("CauseSpendWall = %q", got)
	}
	if got := agenterr.Classify(agenterr.WithCause(agenterr.CauseAccountWall, errors.New("revoked"))); got != agenterr.ClassAccountWall {
		t.Fatalf("CauseAccountWall = %q", got)
	}
	if got := agenterr.ClassFromCause("You've hit your monthly spend limit"); got != agenterr.ClassNone {
		t.Fatalf("sentence cause = %q", got)
	}
	if got := agenterr.Classify(agenterr.WithCause(agenterr.CauseClientBug, errors.New("no session"))); got != agenterr.ClassClientBug {
		t.Fatalf("CauseClientBug = %q", got)
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
	if agenterr.TransientBackend(agenterr.ClassSpendWall) || agenterr.TransientBackend(agenterr.ClassAccountWall) {
		t.Fatal("spend and account walls must not auto-recover")
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
		agenterr.ClassSpendWall,
		agenterr.ClassAccountWall,
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
	if class != agenterr.ClassUnknown {
		t.Fatalf("class=%q", class)
	}
	if msg == "Internal error" || !strings.Contains(msg, "unknown") {
		t.Fatalf("msg=%q", msg)
	}
	class, msg = agenterr.ClassifyAndFormat(agenterr.WithCause(agenterr.CauseBackendUnavailable, fmt.Errorf("Internal error")))
	if class != agenterr.ClassBackendUnavailable || !strings.Contains(msg, "backend_unavailable") {
		t.Fatalf("caused class=%q msg=%q", class, msg)
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

func TestClassifyMessageAlias(t *testing.T) {
	t.Parallel()
	if agenterr.ClassifyMessage("Internal error") != agenterr.ClassifyText("Internal error") {
		t.Fatal("ClassifyMessage must match ClassifyText")
	}
	if agenterr.ClassifyMessage("Internal error") == agenterr.ClassBackendUnavailable {
		t.Fatal("Internal error prose is not backend_unavailable")
	}
	c := agenterr.ClassRateLimit
	if !c.IsTransient() || !c.IsFailure() {
		t.Fatal("method forms for T236")
	}
	if agenterr.ClassAuth.IsTransient() {
		t.Fatal("auth not transient")
	}
}

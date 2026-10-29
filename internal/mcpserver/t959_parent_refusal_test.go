// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

const t959Worker = "jv-t959-refusal"
const t959Parent = "jevons-po"

// 🎯T959: a provider-refused turn is labelled refused on the parent_report
// path; the 401 prose is not delivered as ordinary worker progress.

func TestT959ProviderRefusalNotForwardedAsWorkerReport(t *testing.T) {
	parent := &fakeSender{alive: true}
	s, inbox := chainServer(t, map[string]*fakeSender{
		t959Parent: parent,
		t959Worker: &fakeSender{alive: true},
	})
	s.registry = newLineageRegistry(t, map[string]string{
		t959Parent: "jevons",
		t959Worker: t959Parent,
	})
	s.idleActivity = &IdleActivityTracker{}

	raw := `provider refused the turn: 401 {"type":"error","error":{"type":"authentication_error","message":"OAuth access token has been revoked"}}`
	sink := s.agentEventSink(t959Worker)
	sink(claudia.Event{
		Type: "assistant", Text: raw, StopReason: "end_turn",
		IsError: true, Reason: agenterr.CauseAuth,
	})

	if len(parent.sent) != 1 {
		t.Fatalf("parent deliveries=%d want 1 refused parent_report; got %v", len(parent.sent), parent.sent)
	}
	if strings.Contains(parent.sent[0], "OAuth access token has been revoked") {
		t.Fatalf("parent received raw 401 prose: %q", parent.sent[0])
	}
	if !strings.Contains(parent.sent[0], "provider refused the turn") {
		t.Fatalf("parent copy missing refused label: %q", parent.sent[0])
	}
	if len(inbox.texts) != 0 {
		t.Fatalf("overseer should not receive 401 prose: %v", inbox.texts)
	}
}

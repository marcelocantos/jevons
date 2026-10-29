// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agenterr"
)

// 🎯T845 hermetic: a fixture seat whose provider returns model-not-supported
// on every turn is marked failed, refuses send, and blocks migrate to that model.
func TestT845ProviderErrorTurnMarksSeatFailedAndBlocksSend(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(dir + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.idleActivity = NewIdleActivityTracker()
	const name = "jv-t845-fixture"
	const model = "gpt-5-codex"
	const msg = `The 'gpt-5-codex' model is not supported when using Codex with a ChatGPT account.`
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s1", Provider: claudia.ProviderCodex,
		Model: model, Purpose: claudia.PurposeWork, Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}
	if !s.applyProviderRequestTurnFailure(name, true, agenterr.ClassClientBug, msg, "") {
		t.Fatal("first errored turn should latch failed")
	}
	class, last, failed := s.ProviderRequestFailure(name)
	if !failed || class != agenterr.ClassClientBug || !strings.Contains(last, "not supported") {
		t.Fatalf("failure state: class=%s last=%q failed=%v", class, last, failed)
	}
	if err := s.deliverRefuseIfProviderRequestFailed(name); err == nil || !strings.Contains(err.Error(), "failed") {
		t.Fatalf("send refusal: %v", err)
	}
	if err := s.ValidateDestinationModel("codex", model, false); err == nil {
		t.Fatalf("migrate preflight: %v", err)
	}
}

func TestT845ModelNotSupportedHeuristic(t *testing.T) {
	if !modelNotSupported("invalid_request_error: model is not supported") {
		t.Fatal("expected match")
	}
	if modelNotSupported("hello") {
		t.Fatal("unexpected match")
	}
}

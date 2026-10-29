// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"errors"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agenterr"
)

func TestT1100ConversationRefusalDisplayAndReauthGate(t *testing.T) {
	const name = "jv-t1100-missing"
	refusal := errors.New("existing conversation required for this seat and session; refusing to mint a replacement session")
	RecordRehydrateFailure(name, refusal)
	t.Cleanup(func() { RecordRehydrateFailure(name, nil) })

	def := claudia.AgentDef{Name: name, Provider: "anthropic"}
	if RehydrateAuthRecoverable(def) {
		t.Fatal("conversation refusal must not be auth-recoverable")
	}
	if !RecordedConversationResumeRefusal(name) {
		t.Fatal("expected recorded conversation resume refusal")
	}
	got := RehydrateHealth(def)
	if !strings.Contains(got, ConversationMissingDisplay) {
		t.Fatalf("RehydrateHealth=%q", got)
	}
}

func TestT1100TypedAuthFailureRemainsRecoverable(t *testing.T) {
	const name = "jv-t1100-auth"
	RecordRehydrateFailure(name, agenterr.WithCause(agenterr.CauseAuth, errors.New("invalid_grant")))
	t.Cleanup(func() { RecordRehydrateFailure(name, nil) })
	def := claudia.AgentDef{Name: name, Provider: "anthropic"}
	if !RehydrateAuthRecoverable(def) {
		t.Fatal("typed auth failure should stay auth-recoverable")
	}
}

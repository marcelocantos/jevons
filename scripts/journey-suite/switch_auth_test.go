// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestAuthRefreshRefused(t *testing.T) {
	refused := errors.New("broker protocol: agent_failed: omp: anthropic refresh refused: CLAUDIA_OMP_NO_REFRESH is set")
	if !authRefreshRefused(refused, "") {
		t.Fatal("refresh refused was not recognised")
	}
	if !authRefreshRefused(errors.New("switch"), "CLAUDIA_OMP_NO_REFRESH is set") {
		t.Fatal("the tool text was not recognised")
	}
	if authRefreshRefused(errors.New("switch cursor → claude: plan dest empty"), "") {
		t.Fatal("an empty destination was treated as auth unavailable")
	}
}

func TestSeatPreserved(t *testing.T) {
	before := claudia.AgentDef{Provider: claudia.ProviderCodex, SessionID: "sess-1"}
	if err := seatPreserved(before, before); err != nil {
		t.Fatal(err)
	}
	samePlan := before
	samePlan.Provider = "openai-codex"
	if err := seatPreserved(before, samePlan); err != nil {
		t.Fatal(err)
	}
	if err := seatPreserved(before, claudia.AgentDef{}); err == nil || !strings.Contains(err.Error(), "gone") {
		t.Fatalf("missing seat: %v", err)
	}
	moved := before
	moved.Provider = claudia.ProviderClaude
	if err := seatPreserved(before, moved); err == nil || !strings.Contains(err.Error(), "provider changed") {
		t.Fatalf("moved provider: %v", err)
	}
	resessioned := before
	resessioned.SessionID = "sess-2"
	if err := seatPreserved(before, resessioned); err == nil || !strings.Contains(err.Error(), "session changed") {
		t.Fatalf("replaced session: %v", err)
	}
}

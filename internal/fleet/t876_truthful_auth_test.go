// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"errors"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestPreferRehydrateFailureKeepsInvalidGrantOverKeychainStartup(t *testing.T) {
	prior := "anthropic refresh failed: invalid_grant"
	next := "omp: keychain was not read at startup"
	if got := preferRehydrateFailure(prior, next); got != prior {
		t.Fatalf("preferRehydrateFailure(%q, %q) = %q, want the invalid_grant", prior, next, got)
	}
}

func TestPreferRehydrateFailureTakesSpecificOverGenericPrior(t *testing.T) {
	prior := "omp: keychain was not read at startup"
	next := "omp: xai-oauth refresh failed: invalid_grant"
	if got := preferRehydrateFailure(prior, next); got != next {
		t.Fatalf("preferRehydrateFailure(%q, %q) = %q, want the invalid_grant", prior, next, got)
	}
}

func TestPreferRehydrateFailureReplacesNonAuthWithAuth(t *testing.T) {
	prior := "sandbox policy refused"
	next := "omp: keychain was not read at startup"
	if got := preferRehydrateFailure(prior, next); got != next {
		t.Fatalf("a host auth failure still replaces an unrelated prior: got %q", got)
	}
}

func TestNoteRehydrateKeepsTruthfulAuthFailure(t *testing.T) {
	const name = "t876-seat"
	t.Cleanup(func() { rehydrateFailures.Delete(name) })

	noteRehydrate(name, errors.New("anthropic refresh failed: invalid_grant"))
	noteRehydrate(name, errors.New("omp: keychain was not read at startup"))

	got := RehydrateHealth(claudia.AgentDef{Name: name, Provider: "anthropic"})
	if !strings.Contains(got, "invalid_grant") {
		t.Fatalf("RehydrateHealth after keychain overwrite = %q, want invalid_grant kept", got)
	}
	if strings.Contains(got, "keychain was not read") {
		t.Fatalf("RehydrateHealth still shows the generic keychain message: %q", got)
	}
	if !reauthableShape(got) {
		t.Fatalf("truthful failure must still look reauthable, got %q", got)
	}
}

// reauthableShape mirrors server.reauthablePlanFailure's "broken:" prefix
// without importing server (cycle).
func reauthableShape(health string) bool {
	return strings.HasPrefix(health, "broken:")
}

func TestNoteRehydrateStillClearsOnSuccess(t *testing.T) {
	const name = "t876-clear"
	t.Cleanup(func() { rehydrateFailures.Delete(name) })

	noteRehydrate(name, errors.New("anthropic refresh failed: invalid_grant"))
	noteRehydrate(name, nil)
	if got := RehydrateHealth(claudia.AgentDef{Name: name}); got != "resumable" {
		t.Fatalf("after success RehydrateHealth=%q, want resumable", got)
	}
}

func TestNoteRehydrateRecordsFreshKeychainWhenNoPrior(t *testing.T) {
	const name = "t876-first-keychain"
	t.Cleanup(func() { rehydrateFailures.Delete(name) })

	noteRehydrate(name, errors.New("omp: keychain was not read at startup"))
	got := RehydrateHealth(claudia.AgentDef{Name: name})
	if !strings.Contains(got, "keychain was not read") {
		t.Fatalf("first keychain failure should record: got %q", got)
	}
}

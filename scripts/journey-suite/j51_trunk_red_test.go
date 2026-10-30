// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/worktree"
)

// A fake integrate refusal publishes the same typed result as cmd/integrate.
// The live journey's wait loop must observe it without waiting out its budget.
func TestT1334FakeIntegrateTrunkRedFailsWaitBeforeDeadline(t *testing.T) {
	fakeIntegrateResult := `{"kind":"trunk_red","id":"incident-123","tests":["pkg/calc/TestBroken"]}`
	var fault worktree.TrunkFault
	if err := json.Unmarshal([]byte(fakeIntegrateResult), &fault); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	deadline := started.Add(90 * time.Minute)
	polls := 0
	stopped := 0
	err := t1151WaitForCompletion(deadline, time.Millisecond, func() (*worktree.TrunkFault, error) {
		polls++
		if polls == 2 {
			return &fault, nil
		}
		return nil, nil
	}, func() (bool, error) { return false, nil }, func() error { stopped++; return nil })
	if err == nil || !strings.Contains(err.Error(), fault.ID) || !strings.Contains(err.Error(), fault.Tests[0]) {
		t.Fatalf("want incident id and trunk tests, got %v", err)
	}
	if polls != 2 || stopped != 1 || time.Since(started) > time.Second {
		t.Fatalf("did not fail promptly: polls=%d stopped=%d elapsed=%s", polls, stopped, time.Since(started))
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

func migrateAttempts(t *testing.T, refusal, body string) int {
	t.Helper()
	prev := overseerInterruptSettle
	overseerInterruptSettle = time.Millisecond
	t.Cleanup(func() { overseerInterruptSettle = prev })

	s := New("test", t.TempDir())
	s.SetOverseerName("jevons")
	reg, err := claudia.NewRegistry(t.TempDir() + "/agents.json")
	if err != nil {
		t.Fatal(err)
	}
	s.SetRegistry(reg)
	mig := &fakeOverseerMigrator{prepErr: errors.New(refusal)}
	s.SetOverseerMigrator(mig)

	rec := httptest.NewRecorder()
	s.handleOverseerMigrate(rec, httptest.NewRequest(http.MethodPost,
		"/api/overseer/migrate", strings.NewReader(body)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("refused migration → %d, want 409", rec.Code)
	}
	mig.mu.Lock()
	defer mig.mu.Unlock()
	return len(mig.prepared)
}

// Forty-six forced overseer migrations were refused over twelve minutes on
// 2026-09-22 with "turn in flight": a fleet that reports to the overseer
// every minute leaves no gap between its turns. Forcing interrupts the turn
// and asks again.
func TestForcedOverseerMigrationInterruptsATurnInFlight(t *testing.T) {
	const inFlight = "broker protocol: agent_failed: Migrate: turn in flight; wait for the current response or Interrupt first"

	if n := migrateAttempts(t, inFlight, `{"provider":"claude","force":true}`); n != 2 {
		t.Fatalf("forced migration asked %d times, want a second ask after the interrupt", n)
	}
	// The controls: an unforced migration waits its turn, and force does not
	// turn every refusal into an interrupt.
	if n := migrateAttempts(t, inFlight, `{"provider":"claude"}`); n != 1 {
		t.Fatalf("unforced migration asked %d times, want 1", n)
	}
	if n := migrateAttempts(t, `migrate "jevons": already on claude`, `{"provider":"claude","force":true}`); n != 1 {
		t.Fatalf("a refusal that is not a turn in flight was retried %d times", n)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"errors"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/handover"
)

// A Migrate that landed in claudia but was never recorded here left the
// registry naming the old provider, and every retry refused: the overseer
// sat on Cursor in the broker and on codex in agents.json (2026-09-21).
func TestRetriedMigrateRecordsAMoveClaudiaAlreadyMade(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa6461"
	f, _, _ := migrateFixture(t, oldSession, true)
	f.liveMigrate = func(*claudia.MigrateArgs) error {
		return errors.New("broker protocol: agent_failed: Migrate: same provider claude; use SetModel")
	}

	pending, err := f.PrepareMigration("jevons-po", claudia.ProviderClaude, false)
	if err != nil {
		t.Fatalf("retry of a migration claudia already made was refused: %v", err)
	}
	if pending.Remap != handover.RemapClaudiaMigrate || pending.To != string(claudia.ProviderClaude) {
		t.Fatalf("pending = %+v", pending)
	}
	def := f.reg.Def("jevons-po")
	if def == nil || def.Provider != claudia.ProviderClaude {
		t.Fatalf("registry row still names the old provider: %+v", def)
	}
	if def.SessionID == oldSession {
		t.Fatal("registry row kept the predecessor's session id")
	}
}

// The control: any other refusal is still a refusal, and records nothing.
func TestMigrateRefusedForAnotherReasonRecordsNothing(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa6461"
	f, _, _ := migrateFixture(t, oldSession, true)
	before := *f.reg.Def("jevons-po")
	f.liveMigrate = func(*claudia.MigrateArgs) error {
		return errors.New("broker protocol: agent_failed: Migrate: turn in flight; wait for the current response or Interrupt first")
	}

	if _, err := f.PrepareMigration("jevons-po", claudia.ProviderClaude, false); err == nil {
		t.Fatal("a turn-in-flight refusal was treated as a completed migration")
	}
	if after := f.reg.Def("jevons-po"); after.Provider != before.Provider || after.SessionID != before.SessionID {
		t.Fatalf("refused migration changed the row: %+v", after)
	}
}

// jevons-po refused a forced migrate for as long as anyone kept asking on
// 2026-09-22: its workers report to it every minute and each report starts a
// turn. Force asks once more after the seat has been told to stop; an unforced
// migrate still waits its turn.
func TestForcedMigrateAsksAgainAfterATurnInFlight(t *testing.T) {
	const oldSession = "019fd13d-e500-7913-b96c-981e50aa6461"
	prev := migrateInterruptSettle
	migrateInterruptSettle = 0
	t.Cleanup(func() { migrateInterruptSettle = prev })

	asks := func(force bool) int {
		f, _, _ := migrateFixture(t, oldSession, true)
		n := 0
		f.liveMigrate = func(*claudia.MigrateArgs) error {
			n++
			return errors.New("Migrate: turn in flight; wait for the current response or Interrupt first")
		}
		if _, err := f.PrepareMigration("jevons-po", claudia.ProviderClaude, force); err == nil {
			t.Fatal("a migrate that was refused twice was reported as done")
		}
		return n
	}
	if n := asks(false); n != 1 {
		t.Fatalf("unforced migrate asked %d times, want 1", n)
	}
	if n := asks(true); n != 2 {
		t.Fatalf("forced migrate asked %d times, want 2", n)
	}
}

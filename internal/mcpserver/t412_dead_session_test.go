// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T412 — a registry status of "running" means a live conversation exists.
//
// The fixture is the 2026-08-10 incident: six agents registered running with
// phase=idle while none of their session ids had a JSONL anywhere on disk.
// The 🎯T409 recovery had minted replacement sessions that never
// materialized, agent_list said running, the sentinel prescribed the same
// repair every tick, and the impatience ladder reported actions=7 errors=0
// while nudging the void.
//
// The dead seat's inputs: process alive (a pane exists), no process-local
// turn (the daemon restarted, or the turn never began), Materialized=true —
// the durable flag that alone made 🎯T305 answer running — and a session id
// whose records were located and are absent.

// deadSeatDef registers name in reg as the incident shape: Materialized over
// a session id with no transcript under a HOME the test owns.
func deadSeatDef(t *testing.T, reg *claudia.Registry, name, sessionID string) claudia.AgentDef {
	t.Helper()
	def := claudia.AgentDef{
		Name:         name,
		WorkDir:      t.TempDir(),
		Provider:     claudia.ProviderClaude,
		SessionID:    sessionID,
		Materialized: true,
	}
	if reg != nil {
		if err := reg.Register(def); err != nil {
			t.Fatal(err)
		}
	}
	return def
}

// The whole target in one assertion, through the production derivation
// handleAgentList uses (🎯T419): a live seat whose claimed conversation does
// not exist on disk is dead_unmaterialized, never running.
func TestT412UnmaterializedSeatIsNotRunning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := New(t.TempDir(), nil, nil)
	def := deadSeatDef(t, nil, "jv-t372-auto", "844f9956-dead-seat")

	got := s.agentPhase(def, true)
	if got == AgentStatusRunning {
		t.Fatal("a live seat with no conversation on disk was reported running: " +
			"the lie 🎯T412 exists to stop")
	}
	if got != AgentStatusDeadUnmaterialized {
		t.Fatalf("phase for an unmaterialized seat: %s want %s", got, AgentStatusDeadUnmaterialized)
	}
}

// The control that proves the oracle discriminates: the identical inputs with
// the transcript actually on disk read running. Without this, the green above
// proves only that something returns dead for everything.
func TestT412MaterializedSeatStillRunning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := New(t.TempDir(), nil, nil)
	def := deadSeatDef(t, nil, "jv-t370-auto", "a322f0b6-live-seat")

	path := claudia.SessionJSONLPath(def.SessionID, def.WorkDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"type":"user","message":{"role":"user","content":"[Jevons fleet standing brief] Execute 🎯T370."}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := s.agentPhase(def, true); got != AgentStatusRunning {
		t.Fatalf("phase for a live seat with a real transcript: %s want %s", got, AgentStatusRunning)
	}
}

// The demotion never fires on a claim it cannot check, and never fires on a
// claim backed by a confirmed turn.
func TestT412DemotionBounds(t *testing.T) {
	// A confirmed in-process turn keeps running: a session minted this
	// instant legitimately has no JSONL yet (the mint window).
	if got := ClassifyAgentPhase(true, true, true, SessionEvidenceAbsent); got != AgentStatusRunning {
		t.Fatalf("turnBegan over absent evidence: %s want %s (mint window must stay running)",
			got, AgentStatusRunning)
	}
	// Evidence Unknown (a grok/codex seat, or an unreadable store) keeps
	// running: a failure to observe never manufactures a death (🎯T422).
	if got := ClassifyAgentPhase(true, false, true, SessionEvidenceUnknown); got != AgentStatusRunning {
		t.Fatalf("unknown evidence: %s want %s (no death from a failed look)", got, AgentStatusRunning)
	}
	// A dead process stays stopped — the new label is about live seats.
	if got := ClassifyAgentPhase(false, false, true, SessionEvidenceAbsent); got != AgentStatusStopped {
		t.Fatalf("dead process: %s want %s", got, AgentStatusStopped)
	}
	// Non-Claude providers read Unknown, so the production path cannot demote
	// them on a file jevons was never expected to find.
	if ev := ReadSessionEvidence(claudia.ProviderGrok, "some-session", t.TempDir()); ev != SessionEvidenceUnknown {
		t.Fatalf("grok session evidence: %s want unknown", ev)
	}
}

// The pre-fix derivation, on the incident's own inputs, answers running —
// in-band proof that the fixture measures the defect (🎯T419 control), and a
// live invariant that the 🎯T305 base answer is unchanged.
func TestT412PreFixDerivationIsRedOnTheSameFixture(t *testing.T) {
	if got := ClassifyAgentListStatus(true, false, true); got != AgentStatusRunning {
		t.Fatalf("pre-fix derivation on the incident inputs: %s — want %s, "+
			"the false claim 🎯T412 exists to stop", got, AgentStatusRunning)
	}
}

// Clause 2: the ladder's actuator refuses a nudge into the void, so a tick
// that touches N unmaterialized agents reports them as failures — never
// actions=N errors=0. RePressure is the sink the tick's Actuate fires.
func TestT412LadderRefusesRePressureToDeadSeat(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	deadSeatDef(t, reg, "jv-t374-script-abort-root", "0c8a2875-dead-seat")

	sink := &rePressureSink{server: s}
	err = sink.RePressure("jv-t374-script-abort-root", "T374")
	if err == nil {
		t.Fatal("repressure into an unmaterialized seat returned success: " +
			"the actions=7 errors=0 lie of 2026-08-10")
	}
	if !strings.Contains(err.Error(), AgentStatusDeadUnmaterialized) {
		t.Fatalf("repressure refusal does not name the state: %v", err)
	}
}

// Clause 3's reporting half: a row the 🎯T409 recovery has just rotated
// (Materialized=false, fresh session id, nothing on disk) is never_briefed —
// visibly awaiting its brief — not running and not dead. The rotation itself
// must not manufacture either a live conversation or a corpse.
func TestT412RotatedRowIsNeverBriefedNotRunning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := New(t.TempDir(), nil, nil)
	def := claudia.AgentDef{
		Name:      "jv-t375-consistent-cockpit-snapshot",
		WorkDir:   t.TempDir(),
		Provider:  claudia.ProviderClaude,
		SessionID: "232f204d-fresh-mint",
		// Materialized=false: RehydratedDef's contract after a 🎯T409 rotation.
	}
	if got := s.agentPhase(def, true); got != AgentStatusNeverBriefed {
		t.Fatalf("freshly rotated row: %s want %s", got, AgentStatusNeverBriefed)
	}
}

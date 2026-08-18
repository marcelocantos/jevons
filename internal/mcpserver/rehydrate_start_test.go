// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleet"
)

// 🎯T313: the agent_start seam for a lost session.
//
// handleAgentStart itself spawns a real Claude TUI, so the hermetic
// assertion is on the handoff either side of that call: after the
// rehydrate, the Config that registry.Launch would build must no longer
// demand a resume. launchConfigFromDef is that handoff (🎯T215) — if it
// still reports requireResume, Launch refuses again and the dead-end is
// intact regardless of what the rotation did to the row.

func TestAgentStartRehydratesLostSessionBeforeLaunch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)
	s.SetDefaultProvider(string(claudia.ProviderGrok))

	workdir := t.TempDir()
	const name = "jv-t313-worker"

	def, _, _, err := s.stitchAgentStart(name, workdir, "opus",
		string(claudia.ProviderClaude), "", "jevons-po", claudia.PurposeWork, "T313", "")
	if err != nil {
		t.Fatalf("stitchAgentStart: %v", err)
	}
	// Simulate the state a daemon restart leaves behind: claudia marks
	// the row Materialized the instant the process spawns, so a worker
	// whose first turn never submitted has Materialized=true and no
	// transcript on disk.
	def.Materialized = true
	if err := reg.Register(*def); err != nil {
		t.Fatal(err)
	}
	lostID := def.SessionID

	// Today's behaviour, asserted so the regression is visible: Launch
	// would demand a resume of a transcript that does not exist.
	if _, _, requireResume := launchConfigFromDef(reg.Def(name)); !requireResume {
		t.Fatal("fixture wrong: row does not demand a resume")
	}
	if !fleet.SessionLost(reg.Def(name)) {
		t.Fatal("fixture wrong: session not judged lost")
	}

	lost, ok, err := fleet.RehydrateLostSessionIn(reg, name)
	if err != nil || !ok {
		t.Fatalf("rehydrate: ok=%v err=%v", ok, err)
	}

	// The point of the wiring: the very next Launch mints instead of
	// refusing, and it does so under the same identity.
	provider, sessionID, requireResume := launchConfigFromDef(reg.Def(name))
	if requireResume {
		t.Fatal("still RequireResume after rehydrate — Launch would refuse again")
	}
	if sessionID == lostID || sessionID != lost.NewSession {
		t.Fatalf("session not rotated: %s (lost %s, reported %s)", sessionID, lostID, lost.NewSession)
	}
	if provider != claudia.ProviderClaude {
		t.Fatalf("provider lost in rehydrate: %q", provider)
	}
	after := reg.Def(name)
	if after.Parent != "jevons-po" || after.Purpose != claudia.PurposeWork ||
		after.TargetID != "T313" || after.Model != "opus" {
		t.Fatalf("lineage/target/model lost: %+v", after)
	}
}

// The caller must be told, in the tool result, that the agent came back
// blank — a start that reads like an ordinary resume would have the PO
// assume the brief is still loaded (🎯T313 acceptance 2).
func TestPrefixRehydrateLeadsWithTheLoss(t *testing.T) {
	lost := fleet.LostSession{
		Name: "jv-t313-worker", OldSession: "cd641cad-1a42-47de-a037-1643add32e94",
		NewSession: "new-id", JSONLPath: "/nope.jsonl",
		Parent: "jevons-po", Purpose: claudia.PurposeWork,
		Provider: claudia.ProviderClaude, TargetID: "T313",
	}
	out := prefixRehydrate(lost.Describe(), `Agent "jv-t313-worker" started (session: new-id)`)

	if !strings.HasPrefix(out, "Rehydrated") {
		t.Fatalf("loss not led with: %s", out)
	}
	if !strings.Contains(out, lost.OldSession) {
		t.Fatalf("lost session id absent: %s", out)
	}
	if !strings.Contains(out, "re-send") {
		t.Fatalf("re-brief instruction absent: %s", out)
	}
	if !strings.Contains(out, "started") {
		t.Fatalf("start result dropped: %s", out)
	}

	// A healthy start must stay exactly as it was.
	if got := prefixRehydrate("", "plain"); got != "plain" {
		t.Fatalf("healthy start decorated: %q", got)
	}
}

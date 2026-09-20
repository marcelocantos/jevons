// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

// t738HouseStyleGoal is a spawn brief for one bound target (T717) whose
// house-rules paragraph cites unrelated prior art. Every brief in this repo
// looks like this, which is why 🎯T738 fired on the common case: under the
// old all-ids scan at least one cited target is always open, so no such
// brief could ever evidence completion.
const t738HouseStyleGoal = `You are jv-t717-replay-digest on 🎯T717.
House rules: commit only your own paths (🎯T377); shared hot files are
compare-and-swap (🎯T376); a [killed] is the harness timeout (🎯T712).
Prior art for the digest shape: 🎯T710.`

// t738Ledger writes a bullseye.yaml with the given id→status pairs and
// returns its directory, for use as a seat's WorkDir.
func t738Ledger(t *testing.T, rows map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	yaml := "targets:\n"
	for id, st := range rows {
		yaml += "  " + id + ":\n    name: " + id + "\n    status: " + st + "\n"
	}
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func t738Server(t *testing.T, def claudia.AgentDef) *Server {
	t.Helper()
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(def); err != nil {
		t.Fatal(err)
	}
	return &Server{registry: reg}
}

// TestT738BoundTargetAchieveClosesGoalDespiteIncidentalIDs is the row's
// first acceptance clause at the daemon seam: T717 achieved, T710 still
// open in a house-rule sentence, Goal closes.
func TestT738BoundTargetAchieveClosesGoalDespiteIncidentalIDs(t *testing.T) {
	t.Parallel()
	dir := t738Ledger(t, map[string]string{
		"T717": "achieved",
		"T710": "identified",
		"T377": "achieved",
		"T376": "achieved",
		"T712": "identified",
	})
	s := t738Server(t, claudia.AgentDef{
		Name: "jv-t717-replay-digest", Purpose: claudia.PurposeWork,
		SessionID: "s1", TargetID: "T717", WorkDir: dir, Goal: t738HouseStyleGoal,
	})

	// A turn that says nothing about its status at all: the close must come
	// from the ledger, not from a marker.
	s.clearSessionGoalIfComplete("jv-t717-replay-digest", "Reading the row now.")

	if got := s.registry.Def("jv-t717-replay-digest").Goal; got != "" {
		t.Fatalf("Goal must be cleared once the bound target is achieved; got %q", got)
	}
	if effectiveSessionGoal(s.registry.Def("jv-t717-replay-digest")) != "" {
		t.Fatal("Launch must not reopen Continue for a closed mission")
	}
}

// TestT738OpenBoundTargetKeepsGoal is the other direction: achieved prior
// art all around an unfinished mission must not close it.
func TestT738OpenBoundTargetKeepsGoal(t *testing.T) {
	t.Parallel()
	dir := t738Ledger(t, map[string]string{
		"T717": "converging",
		"T710": "achieved",
		"T377": "achieved",
		"T376": "achieved",
		"T712": "achieved",
	})
	s := t738Server(t, claudia.AgentDef{
		Name: "w", Purpose: claudia.PurposeWork,
		SessionID: "s1", TargetID: "T717", WorkDir: dir, Goal: t738HouseStyleGoal,
	})

	s.clearSessionGoalIfComplete("w", "Still implementing.")

	if s.registry.Def("w").Goal == "" {
		t.Fatal("Goal must stay open while the bound target is converging")
	}
	if effectiveSessionGoal(s.registry.Def("w")) == "" {
		t.Fatal("Launch must still carry the Goal for an open mission")
	}
}

// TestT738GoalStatusClosesWithEmptyPromptResult is the row's second
// acceptance clause, driven through the real per-seat event sink.
//
// A Grok ACP turn publishes its text as agent_message_chunk events and then
// terminates with a JSON-RPC prompt result carrying no Text at all
// (grok_acp publishPromptResult). claudia's own goalTurn can therefore be
// empty when ParseGoalStatus runs there. The daemon does not depend on it:
// agentEventSink concatenates the chunks itself and flushes on the terminal
// stop, so the displayed text — exactly "GOAL_STATUS: complete" — is what
// closes the mission. The ledger is deliberately unhelpful here (the bound
// target is still identified) so only the marker can close it.
//
// A marker turn also reaps the seat (🎯T165), so "mission still open" is
// read as a row that is both present and still carrying its Goal. The
// control run below is what makes that sharp: the same event shape with
// ordinary text must leave the seat registered with its Goal intact, so a
// close here cannot be the sink closing or reaping indiscriminately.
func TestT738GoalStatusClosesWithEmptyPromptResult(t *testing.T) {
	t.Parallel()

	run := func(t *testing.T, turnText string) *claudia.AgentDef {
		t.Helper()
		dir := t738Ledger(t, map[string]string{"T717": "identified", "T710": "identified"})
		s := t738Server(t, claudia.AgentDef{
			Name: "w", Purpose: claudia.PurposeWork,
			SessionID: "s1", TargetID: "T717", WorkDir: dir, Goal: t738HouseStyleGoal,
		})
		sink := s.agentEventSink("w")
		// agent_message_chunk: the displayed assistant text.
		sink(claudia.Event{
			Type: "assistant", SessionID: "s1", TurnID: "1",
			Text: turnText, PreviewUpdate: claudia.PreviewUpdateAppend,
		})
		// publishPromptResult: terminal, and empty.
		sink(claudia.Event{
			Type: "assistant", SessionID: "s1", TurnID: "1",
			Text: "", StopReason: "end_turn",
		})
		return s.registry.Def("w")
	}

	if def := run(t, claudia.GoalStatusComplete); def != nil && def.Goal != "" {
		t.Fatalf("GOAL_STATUS: complete must close the mission even with an empty prompt result; Goal=%q", def.Goal)
	}

	control := run(t, "Reading the row; no verdict yet.")
	if control == nil {
		t.Fatal("control: an ordinary turn must not end the seat")
	}
	if control.Goal == "" {
		t.Fatal("control: an ordinary turn must not close the Goal")
	}
}

// TestT738UnregisteredSeatClosesGoal covers the leftover pane. Once the
// registry row is gone claudia's askOwnerGoalCheck has no owner to ask and
// reads that as "not complete", so the seat was fed Continue forever. An
// unregistered seat has no mission; the close path must not return early.
func TestT738UnregisteredSeatClosesGoal(t *testing.T) {
	t.Parallel()
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg}
	// No panic, no early-return crash, and nothing invented for a name the
	// registry has never heard of.
	s.clearSessionGoalIfComplete("jv-t717-replay-digest", claudia.GoalStatusComplete)
	s.closeSeatGoalBeforeRemoval("jv-t717-replay-digest", "reaped_as_finished_work")
	if reg.Def("jv-t717-replay-digest") != nil {
		t.Fatal("close path must not resurrect a removed seat")
	}
}

// TestT738CloseSeatGoalBeforeRemovalClearsGoal: the reap closes the Goal
// while the owner grant still exists, so the answer no longer depends on
// the owner still being there to give it.
func TestT738CloseSeatGoalBeforeRemovalClearsGoal(t *testing.T) {
	t.Parallel()
	dir := t738Ledger(t, map[string]string{"T717": "identified"})
	s := t738Server(t, claudia.AgentDef{
		Name: "w", Purpose: claudia.PurposeWork,
		SessionID: "s1", TargetID: "T717", WorkDir: dir, Goal: t738HouseStyleGoal,
	})

	s.closeSeatGoalBeforeRemoval("w", "reaped_as_finished_work")

	if got := s.registry.Def("w").Goal; got != "" {
		t.Fatalf("reap must clear durable Goal before the row goes; got %q", got)
	}
}

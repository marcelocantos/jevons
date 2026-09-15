// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
)

// The report a worker writes when the PreToolUse hook asks it to stop: it
// names the ceiling and ends the turn without claiming completion.
const t663HookCheckpointReport = `Checkpoint: depth-ceiling checkpoint after 8 tool calls.
Ending this turn here. Next step: wire the remaining case in agent_send.go and run the gate.`

func t663Server(t *testing.T, name string) (*Server, chan [2]string) {
	t.Helper()
	reg := t439Registry(t, name)
	s := &Server{registry: reg}
	resumed := make(chan [2]string, 2)
	s.SetTurnDepthResumer(func(n, prompt string) { resumed <- [2]string{n, prompt} })
	return s, resumed
}

// 🎯T663 acceptance 1 and 3: the hook-shaped checkpoint is resumed, marked
// source=hook, and the seat stays registered.
func TestT663HookCheckpointIsResumed(t *testing.T) {
	const name = "jv-t663-hook"
	if !IsDepthCeilingCheckpoint(t663HookCheckpointReport) {
		t.Fatal("fixture no longer names the depth ceiling")
	}
	if got := ClassifyReportAsk(t663HookCheckpointReport); got != AskCheckpoint {
		t.Fatalf("fixture classifies as %s, want checkpoint", got)
	}
	s, resumed := t663Server(t, name)

	s.maybeReapDoneWorkAgent(name, t663HookCheckpointReport)

	select {
	case got := <-resumed:
		if got[0] != name {
			t.Fatalf("resumed %q, want %q", got[0], name)
		}
		if !strings.Contains(got[1], "🎯T392.4") || !strings.Contains(got[1], "source=hook") {
			t.Fatalf("resume prompt = %q", got[1])
		}
	default:
		t.Fatal("hook-driven checkpoint was not resumed — the seat would sit idle until a rehydrate")
	}
	if s.registry.Def(name) == nil {
		t.Fatal("the checkpointed seat left the fleet")
	}
	// 🎯T663 acceptance 2: between checkpoint and resume the seat reads as
	// checkpointed; once the resume becomes a turn it does not.
	if !s.checkpointResumePendingFor(name) {
		t.Fatal("seat is not marked checkpointed while its resume is owed")
	}
	if got := s.agentPhase(*s.registry.Def(name), true); got != AgentStatusCheckpointed {
		t.Fatalf("agent_list status = %q, want %q", got, AgentStatusCheckpointed)
	}
	s.markAgentTurnBegan(name)
	if s.checkpointResumePendingFor(name) {
		t.Fatal("resume became a turn but the seat still reads as checkpointed")
	}
}

// The daemon's own counter asked: today's path (🎯T471 keep, resume via
// observeTurnDepth) is unchanged and the hook path must not resume twice.
func TestT663DaemonRequestedCheckpointIsNotDoubleResumed(t *testing.T) {
	const name = "jv-t663-daemon"
	s, resumed := t663Server(t, name)
	s.noteCheckpointEnded(name)

	s.maybeReapDoneWorkAgent(name, t663HookCheckpointReport)

	select {
	case got := <-resumed:
		t.Fatalf("hook path resumed a daemon-requested checkpoint: %v", got)
	default:
	}
	if s.registry.Def(name) == nil {
		t.Fatal("🎯T471: the checkpointed seat left the fleet")
	}
}

// A checkpoint that is waiting on the overseer, not on the ceiling, is not
// resumed by the daemon — resuming it would talk over the question.
func TestT663PlainCheckpointIsNotResumed(t *testing.T) {
	const name = "jv-t663-plain"
	s, resumed := t663Server(t, name)
	report := "Checkpoint: the migration touches two schemas. Waiting on the overseer to choose which one owns the column before I continue."
	if IsDepthCeilingCheckpoint(report) {
		t.Fatal("fixture must not name the ceiling")
	}

	s.maybeReapDoneWorkAgent(name, report)

	select {
	case got := <-resumed:
		t.Fatalf("plain checkpoint was resumed: %v", got)
	default:
	}
	if s.registry.Def(name) == nil {
		t.Fatal("plain checkpoint left the fleet")
	}
}

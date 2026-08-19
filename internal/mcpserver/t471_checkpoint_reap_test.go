// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/turndepth"
)

// 🎯T471: a worker that checkpoints at the depth ceiling stays registered
// and resumable; stop+Remove fires only on a terminal completion claim.
//
// The acceptance oracle drives three fixture turn-endings through the real
// agentEventSink reap path:
//  1. a done claim (no ceiling) → deregisters
//  2. a T392.4 depth-ceiling turn (even with ambiguous "Done." prose) → stays
//  3. a plain turn-end with open mission → stays
//
// Case 2 is RED against the pre-fix tree: without the checkpointEnded latch,
// EndTurn clears Requested before maybeReapDoneWorkAgent runs, so an
// ambiguous bare-done report after a ceiling ask was reaped as finished_work
// and the scheduled resume found no seat (🎯T466 pile).

const t471AmbiguousAfterCeiling = "Done. Ready for the next turn."

const t471PlainOpenMission = "Still wiring the decoder; next I'll tackle the encoder."

func t471SinkServer(t *testing.T, name string) (*Server, *claudia.Registry) {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s-t471",
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
		Materialized: true, Provider: "grok", TargetID: "T471",
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		registry:    reg,
		notifyJevon: func(string) {}, // hermetic: no overseer seam
	}
	s.SetTurnDepthPolicy(turndepth.Policy{Ceiling: turndepth.MinCeiling, Grace: -1})
	s.SetTurnDepthResumer(func(string, string) {}) // swallow resume in hermetic
	return s, reg
}

func t471DriveCeiling(t *testing.T, s *Server, name string) {
	t.Helper()
	for _, ev := range t3924Tool(turndepth.MinCeiling) {
		s.observeTurnDepth(name, ev)
	}
	st := s.turnDepthSnapshot(t, name)
	if !st.Requested {
		t.Fatal("precondition: depth ceiling must have asked for a checkpoint")
	}
}

// TestT471TurnEndingsOnlyDoneClaimDeregisters is the acceptance oracle.
func TestT471TurnEndingsOnlyDoneClaimDeregisters(t *testing.T) {
	cases := []struct {
		name       string
		ceiling    bool
		report     string
		wantReaped bool
	}{
		{
			name:       "done claim deregisters",
			ceiling:    false,
			report:     "Done. SHA abcdef0123456. hermetic TestT471 PASS",
			wantReaped: true,
		},
		{
			name: "T392.4 checkpoint turn keeps registered",
			// Ambiguous bare "Done." that WOULD reap without the latch —
			// proves RED against the pre-fix tree, not only against a
			// checkpoint-shaped phrase the T497 classifier already saves.
			ceiling:    true,
			report:     t471AmbiguousAfterCeiling,
			wantReaped: false,
		},
		{
			name:       "plain turn-end with open mission keeps registered",
			ceiling:    false,
			report:     t471PlainOpenMission,
			wantReaped: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const agent = "jv-t471-checkpoint-reap"
			s, reg := t471SinkServer(t, agent)
			if tc.ceiling {
				t471DriveCeiling(t, s, agent)
			}
			sink := s.agentEventSink(agent)
			sink(claudia.Event{
				Type:       "assistant",
				Text:       tc.report,
				StopReason: "end_turn",
			})
			got := reg.Def(agent) == nil
			if got != tc.wantReaped {
				t.Fatalf("deregistered=%v want %v (report %q)", got, tc.wantReaped, tc.report)
			}
			if !tc.wantReaped && reg.Def(agent) == nil {
				t.Fatal("agent must remain registered and resumable by name")
			}
		})
	}
}

// TestT471AmbiguousDoneReapsWithoutCeiling is the over-broadness control:
// the same ambiguous report without a depth-ceiling ask still reaps under
// 🎯T195, so the latch does not disable finished-work hygiene.
func TestT471AmbiguousDoneReapsWithoutCeiling(t *testing.T) {
	const agent = "jv-t471-checkpoint-reap"
	s, reg := t471SinkServer(t, agent)
	if !LooksLikeFinishedWorkReport(t471AmbiguousAfterCeiling) {
		t.Fatal("control fixture must look like finished work without the latch")
	}
	sink := s.agentEventSink(agent)
	sink(claudia.Event{
		Type:       "assistant",
		Text:       t471AmbiguousAfterCeiling,
		StopReason: "end_turn",
	})
	if reg.Def(agent) != nil {
		t.Fatal("ambiguous bare done without a ceiling ask must still reap (T195)")
	}
}

// TestT471CheckpointFixtureThroughSink pins the real T392.4 checkpoint
// report shape on the sink path (compose T497 classifier + T471 latch).
func TestT471CheckpointFixtureThroughSink(t *testing.T) {
	report := loadCheckpointReport(t)
	const agent = "jv-t471-checkpoint-reap"
	s, reg := t471SinkServer(t, agent)
	t471DriveCeiling(t, s, agent)
	sink := s.agentEventSink(agent)
	sink(claudia.Event{
		Type:       "assistant",
		Text:       report,
		StopReason: "end_turn",
	})
	if reg.Def(agent) == nil {
		t.Fatal("depth-ceiling checkpoint fixture must keep the agent registered")
	}
	if !strings.Contains(strings.ToLower(report), "checkpoint") {
		t.Fatal("fixture lost its checkpoint declaration")
	}
}

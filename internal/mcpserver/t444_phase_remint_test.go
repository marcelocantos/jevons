// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"
)

// 🎯T444 — the registry's phase derivation across a backend re-mint.
//
// The fixture is the live incident: an agent whose session id has ROTATED, so
// the process-local turn mark is gone and AgentDef.Materialized is still the
// false it was minted with, while the new session's transcript sits on disk
// holding a brief and the agent's replies.
//
// Pre-fix these are red at exactly one place each: the derivation returned
// never_briefed for that agent, which is a positive claim that it has received
// nothing — the label that tells a supervisor to brief it, on top of a live
// turn (🎯T416).

// remintedSession writes a session transcript for sessionID under a HOME this
// test owns, at the path claudia computes for workDir, and returns workDir.
// This is the post-re-mint state on disk: new session id, real conversation.
func remintedSession(t *testing.T, sessionID string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	workDir := t.TempDir()

	path := claudia.SessionJSONLPath(sessionID, workDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	lines := strings.Join([]string{
		`{"type":"user","message":{"role":"user","content":"[Jevons fleet standing brief] Execute 🎯T444."}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Re-running the gates on the final tree before committing"}]}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	return workDir
}

// The whole target in one assertion: a live seat whose session records exist is
// not reported as never briefed, whatever the stale inputs say.
func TestT444ReMintedSessionIsNotNeverBriefed(t *testing.T) {
	workDir := remintedSession(t, "dfb91d71-remint")

	ev := ReadSessionEvidence(claudia.ProviderClaude, "dfb91d71-remint", workDir)
	if ev != SessionEvidencePresent {
		t.Fatalf("session evidence for a re-minted session with a transcript: %s want present", ev)
	}
	// alive, no process-local turn (the daemon or the seat was restarted),
	// Materialized false (promoted only at Launch, against the OLD session id).
	got := ClassifyAgentPhase(true, false, false, ev)
	if got == AgentStatusNeverBriefed {
		t.Fatal("a mid-turn agent with a 2-record transcript was reported never_briefed: " +
			"the label that invites a duplicate brief onto a live turn (🎯T416)")
	}
	if got != AgentStatusRunning {
		t.Fatalf("phase after re-mint: %s want running", got)
	}
}

// The control for the assertion above, in band rather than in a commit message:
// the pre-fix derivation, given the very same post-re-mint inputs, answers
// never_briefed. Without this the green above proves only that some function
// returns "running" — with it, the green is a measurement of the thing that was
// wrong. It is also a live invariant: 🎯T305's answer is unchanged, so if this
// ever stops being never_briefed the fix has been made redundant by something
// else and TestT444ReMintedSessionIsNotNeverBriefed has quietly stopped testing.
func TestT444PreFixDerivationIsRedOnTheSameFixture(t *testing.T) {
	if got := ClassifyAgentListStatus(true, false, false); got != AgentStatusNeverBriefed {
		t.Fatalf("pre-fix derivation on the re-mint inputs: %s — want %s, "+
			"the false claim 🎯T444 exists to stop", got, AgentStatusNeverBriefed)
	}
}

// never_briefed keeps its meaning where it is an observation: 🎯T305 and 🎯T387
// still have their signal, and this fix must not spend it.
func TestT444AbsentTranscriptStillNeverBriefed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	workDir := t.TempDir()

	ev := ReadSessionEvidence(claudia.ProviderClaude, "never-minted", workDir)
	if ev != SessionEvidenceAbsent {
		t.Fatalf("session evidence with no transcript on disk: %s want absent", ev)
	}
	if got := ClassifyAgentPhase(true, false, false, ev); got != AgentStatusNeverBriefed {
		t.Fatalf("live zero-turn seat with no session: %s want never_briefed", got)
	}
}

// The third state. A seat whose records this daemon cannot locate is reported
// as unknown, not as a claim about what it has received.
func TestT444UnlocatableRecordsAreUnknownNotNeverBriefed(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	workDir := t.TempDir()

	cases := []struct {
		what      string
		provider  claudia.Provider
		sessionID string
		workDir   string
	}{
		{"no session id", claudia.ProviderClaude, "", workDir},
		{"no workdir", claudia.ProviderClaude, "s1", ""},
		{"provider with no transcript convention", claudia.ProviderGrok, "s1", workDir},
	}
	for _, tc := range cases {
		t.Run(tc.what, func(t *testing.T) {
			ev := ReadSessionEvidence(tc.provider, tc.sessionID, tc.workDir)
			if ev != SessionEvidenceUnknown {
				t.Fatalf("evidence: %s want unknown", ev)
			}
			got := ClassifyAgentPhase(true, false, false, ev)
			if got == AgentStatusNeverBriefed {
				t.Fatal("a phase nobody could read must not be reported as never_briefed")
			}
			if got != AgentStatusPhaseUnknown {
				t.Fatalf("phase: %s want %s", got, AgentStatusPhaseUnknown)
			}
		})
	}
}

// Evidence is consulted ONLY where the 🎯T305 answer had run out of things it
// knew, with one intentional demotion from 🎯T412: Materialized=true over a
// located-and-absent transcript is dead_unmaterialized, not running. A stopped
// seat stays stopped, and a confirmed in-process turn stays running, for every
// evidence value.
func TestT444EvidenceDoesNotOverrideDecidedPhases(t *testing.T) {
	for _, ev := range []SessionEvidence{SessionEvidenceUnknown, SessionEvidenceAbsent, SessionEvidencePresent} {
		if got := ClassifyAgentPhase(false, false, false, ev); got != AgentStatusStopped {
			t.Fatalf("dead seat with evidence=%s: %s want stopped", ev, got)
		}
		if got := ClassifyAgentPhase(true, true, false, ev); got != AgentStatusRunning {
			t.Fatalf("process-local turn with evidence=%s: %s want running", ev, got)
		}
	}
	// Materialized keeps running when evidence cannot prove absence (🎯T422:
	// unknown never manufactures a death) or when the transcript is present.
	for _, ev := range []SessionEvidence{SessionEvidenceUnknown, SessionEvidencePresent} {
		if got := ClassifyAgentPhase(true, false, true, ev); got != AgentStatusRunning {
			t.Fatalf("materialized with evidence=%s: %s want running", ev, got)
		}
	}
	// 🎯T412 carve-out: the durable Materialized flag alone over an absent
	// transcript is a dead seat — this is the one decided T305 answer evidence
	// is allowed to demote.
	if got := ClassifyAgentPhase(true, false, true, SessionEvidenceAbsent); got != AgentStatusDeadUnmaterialized {
		t.Fatalf("materialized with evidence=absent: %s want %s", got, AgentStatusDeadUnmaterialized)
	}
}

// The row an overseer actually reads. agent_list renders the derived phase for
// a registry whose def carries a re-minted session id.
func TestT444AgentListRowAfterReMint(t *testing.T) {
	const name = "bs-t68-reachability-oracle"
	const session = "dfb91d71-remint"
	workDir := remintedSession(t, session)

	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: workDir, SessionID: session,
		Purpose: claudia.PurposeWork, Parent: "bullseye-po",
		Provider: claudia.ProviderClaude,
		// Materialized false: minted before the transcript existed, never
		// re-checked. This is the live registry's own state for 30 of 48 rows.
	}); err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir(), nil, nil)
	s.SetRegistry(reg)

	defs := reg.List()
	if len(defs) != 1 {
		t.Fatalf("registry holds %d defs, want 1", len(defs))
	}
	def := defs[0]
	if def.Materialized {
		t.Fatal("fixture invalid: the def must carry the stale Materialized=false")
	}
	// The registry holds no live process here, so drive the derivation with the
	// aliveness the incident had. handleAgentList reads the same two values.
	if got := s.agentPhase(def, true); got != AgentStatusRunning {
		t.Fatalf("agent_list phase for a re-minted live agent: %s want running", got)
	}

	// And the rendered surface still names the agent, with no never_briefed on
	// the row (the seat is not alive here, so the phase is honestly stopped).
	res, err := s.handleAgentList(nil, mcp.CallToolRequest{})
	if err != nil {
		t.Fatal(err)
	}
	text := toolText(res)
	if !strings.Contains(text, name) {
		t.Fatalf("agent_list omitted %q: %s", name, text)
	}
	if strings.Contains(text, AgentStatusNeverBriefed) {
		t.Fatalf("agent_list claimed never_briefed after a re-mint: %s", text)
	}
}

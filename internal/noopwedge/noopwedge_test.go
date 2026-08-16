// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package noopwedge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 🎯T402 acceptance clause 5: fixture turns for a work order answered with a
// bare ack (flagged), a status check answered [silent] (not flagged), and a
// productive agent whose report history mixes no-ops with real reports (not
// flagged) — plus an over-broadness mutation.

func ack(text string) Turn    { return Turn{Text: text} }
func report(text string) Turn { return Turn{Text: text} }
func work(text string) Turn   { return Turn{Text: text, ToolCalls: 3} }
func mixed(text string) Turn  { return Turn{Text: text, ToolCalls: 1} }
func quiet() Turn             { return Turn{} }

func TestClassifyFixtures(t *testing.T) {
	tests := []struct {
		name string
		ev   Evidence
		want Verdict
	}{{
		// FLAGGED: a work order answered with a bare acknowledgement and
		// nothing else. This is the whole fault 🎯T402 names.
		name: "work order answered with a bare ack and stopped",
		ev:   Evidence{Turns: []Turn{ack("No response requested.")}},
		want: NoOpOnly,
	}, {
		// NOT FLAGGED: a status check answered [silent] with a payload. The
		// owner does not see it (🎯T238), but the agent reported real state.
		name: "status check answered [silent] with a payload",
		ev: Evidence{Turns: []Turn{
			report("[silent] continued jv-t212; 🎯T222 already working."),
		}},
		want: Working,
	}, {
		// NOT FLAGGED, and the reason 🎯T402's obvious detector is wrong: a
		// productive agent's history MIXES no-ops with real reports. This is
		// the shape of all 179 corpus sessions that contain the ack.
		name: "productive agent mixing no-ops with real reports",
		ev: Evidence{Turns: []Turn{
			work("Reading the target and the live fixture."),
			ack("No response requested."),
			mixed("Now the hermetic test:"),
			ack("No response requested."),
			report("Landed at 6330243f; go test ./internal/turnev green."),
		}},
		want: Working,
	}, {
		// NOT FLAGGED: shape-suspect, but the ledger shows commits. Stage 2
		// overrides shape — the repository is the oracle, not the prose.
		name: "shape-suspect but commits exist",
		ev: Evidence{
			Turns:   []Turn{ack("No response requested."), ack("Acknowledged.")},
			Commits: 2,
		},
		want: Working,
	}, {
		// NOT FLAGGED: shape-suspect, no commits, but uncommitted paths.
		// Stage 3 — work in the tree is still work.
		name: "shape-suspect but tree paths touched",
		ev: Evidence{
			Turns:     []Turn{ack("No response requested.")},
			TreePaths: 4,
		},
		want: Working,
	}, {
		// NOT this package's call: never took a turn at all. That is the
		// 🎯T387 / 🎯T416 phantom-seat class with its own evidence rules.
		name: "no turns at all is unobserved, not wedged",
		ev:   Evidence{},
		want: Unobserved,
	}, {
		// FLAGGED: several turns, every one content-free, nothing on disk.
		// The interleaved silent turn neither rescues nor condemns it.
		name: "every turn content-free with nothing on disk",
		ev: Evidence{
			Turns: []Turn{ack("Acknowledged."), quiet(), ack("Noted."), ack("Will do.")},
		},
		want: NoOpOnly,
	}, {
		// NOT FLAGGED, and not Working either: turns that produced nothing at
		// all are interrupted, not answered. 376 sessions in the corpus look
		// like this and none of them is the fault 🎯T402 names — that class
		// belongs to 🎯T387 / 🎯T416, which have their own evidence rules.
		name: "turns that produced nothing at all are unobserved",
		ev:   Evidence{Turns: []Turn{quiet(), quiet()}},
		want: Unobserved,
	}, {
		// NOT FLAGGED: bare "ok" is the natural answer to a yes/no question,
		// and this package cannot see the question. 241 corpus sessions
		// consist of exactly this. Excluded from the vocabulary on purpose.
		name: "bare ok is not a decline to act",
		ev:   Evidence{Turns: []Turn{ack("ok")}},
		want: Working,
	}, {
		// NOT FLAGGED: an ack that carries a payload after it is a report.
		name: "ack prefix followed by substance is a report",
		ev: Evidence{Turns: []Turn{
			ack("No response requested — but the build is broken, see below."),
		}},
		want: Working,
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Classify(tc.ev); got != tc.want {
				t.Fatalf("Classify() = %v, want %v (detail: %s)",
					got, tc.want, Detail(tc.ev))
			}
		})
	}
}

// TestOverBroadMutation is the mutation the target asks for: a detector that
// keys on the string ALONE — "any turn is a bare ack" instead of "every turn
// is" — must be shown to reap healthy workers. If this test ever passes with
// the over-broad predicate substituted in, the discriminator has regressed to
// the version that would have flagged all 179 corpus sessions.
func TestOverBroadMutation(t *testing.T) {
	healthy := []Turn{
		work("Reading the target."),
		ack("No response requested."),
		report("Landed at 6330243f; oracle green."),
	}

	// The mutant: flag when ANY turn is a bare ack.
	anyAck := func(turns []Turn) bool {
		for _, tn := range turns {
			if tn.ToolCalls == 0 && IsBareAck(tn.Text) {
				return true
			}
		}
		return false
	}
	if !anyAck(healthy) {
		t.Fatal("fixture is not a valid mutation probe: the mutant must flag it")
	}
	if ShapeNoOp(healthy) {
		t.Fatal("ShapeNoOp flagged a productive agent — the string-alone regression")
	}
	if got := Classify(Evidence{Turns: healthy}); got != Working {
		t.Fatalf("Classify(productive) = %v, want Working", got)
	}
}

// TestShapeNoOpIsTheCheapGate pins the two-stage contract: ShapeNoOp alone
// decides Working for anything productive, so callers never spend a
// subprocess gathering commits or tree paths for those agents.
func TestShapeNoOpIsTheCheapGate(t *testing.T) {
	if ShapeNoOp(nil) {
		t.Fatal("ShapeNoOp(nil) must be false — an empty history is not a wedge")
	}
	if !ShapeNoOp([]Turn{ack("No response requested.")}) {
		t.Fatal("ShapeNoOp missed the sole-output bare ack")
	}
	if ShapeNoOp([]Turn{{ToolCalls: 1}}) {
		t.Fatal("a turn that called a tool is never a no-op, whatever it said")
	}
}

func TestIsBareAck(t *testing.T) {
	acks := []string{
		"No response requested.", "no response requested",
		"Acknowledged.", "ACK", "Understood.", "Noted.", "Nothing to report.",
		"Will do.", "[silent]", "  [silent]  ",
	}
	for _, s := range acks {
		if !IsBareAck(s) {
			t.Errorf("IsBareAck(%q) = false, want true", s)
		}
	}
	notAcks := []string{
		"Done. Landed at abc1234; go test ./internal/noopwedge green.",
		"[silent] continued jv-t212; 🎯T222 already working.",
		"No response requested, but I could not build: undefined: fleetlog.",
		"I'll read the tail of the predecessor's transcript first.",
		strings.Repeat("ok ", 40),
		// Excluded on corpus evidence — see the note on bareAcks.
		"", "   \n ", "OK", "okay", "Ok.", "roger", "[silent] ok",
	}
	for _, s := range notAcks {
		if IsBareAck(s) {
			t.Errorf("IsBareAck(%q) = true, want false", s)
		}
	}
}

func TestTurnsFromFile(t *testing.T) {
	dir := t.TempDir()

	// Claude-shaped: nested message envelopes, a tool_result user message
	// that must NOT open a turn, and harness bookkeeping lines that must be
	// skipped entirely.
	claude := filepath.Join(dir, "claude.jsonl")
	writeLines(t, claude,
		`{"type":"mode"}`,
		`{"type":"user","message":{"role":"user","content":"Continue from where you left off."}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Now the hermetic test:"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{}}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"All green."}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"[event] daemon restarted"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"No response requested."}]}}`,
	)
	turns, err := TurnsFromFile(claude)
	if err != nil {
		t.Fatalf("TurnsFromFile: %v", err)
	}
	if len(turns) != 2 {
		t.Fatalf("got %d turns, want 2 (tool_result must not open a turn): %+v", len(turns), turns)
	}
	if turns[0].ToolCalls != 1 || !strings.Contains(turns[0].Text, "All green.") {
		t.Fatalf("turn 0 lost its tool call or its trailing text: %+v", turns[0])
	}
	if turns[1].ToolCalls != 0 || turns[1].Text != "No response requested." {
		t.Fatalf("turn 1 = %+v, want the bare ack alone", turns[1])
	}
	if ShapeNoOp(turns) {
		t.Fatal("a session with a real turn must not be shape-suspect")
	}

	// Grok-shaped: role and content at the top level.
	grok := filepath.Join(dir, "grok.jsonl")
	writeLines(t, grok,
		`{"role":"user","content":"[event] worker idle"}`,
		`{"role":"assistant","content":[{"type":"text","text":"Acknowledged."}]}`,
	)
	turns, err = TurnsFromFile(grok)
	if err != nil {
		t.Fatalf("TurnsFromFile(grok): %v", err)
	}
	if len(turns) != 1 || !ShapeNoOp(turns) {
		t.Fatalf("grok dialect: got %+v, want one shape-suspect turn", turns)
	}

	// A session file that does not exist has never begun a turn: no turns, no
	// error, and Unobserved rather than a wedge (🎯T387 owns that class).
	turns, err = TurnsFromFile(filepath.Join(dir, "absent.jsonl"))
	if err != nil {
		t.Fatalf("missing transcript must not error: %v", err)
	}
	if got := Classify(Evidence{Turns: turns}); got != Unobserved {
		t.Fatalf("missing transcript classified %v, want Unobserved", got)
	}
}

func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeRegistry(t *testing.T, stateDir, name, session string) {
	t.Helper()
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `[{"name":"` + name + `","session_id":"` + session + `"}]`
	if err := os.WriteFile(filepath.Join(stateDir, "agents.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// THE property the owner asked for: the notification trigger is this
// code, not the agent's judgement. Exhausting the loop must produce an
// owner-visible note without any daemon, any agent, or any cooperation
// from the thing that failed.
func TestExhaustionNotifiesTheOwnerWithoutTheDaemon(t *testing.T) {
	state := t.TempDir()
	if code := exhausted(state, "jv-t999-stuck", "iteration cap (5) reached"); code == 0 {
		t.Error("giving up should not report success")
	}

	body, err := os.ReadFile(filepath.Join(state, "chatlog", "jevons.jsonl"))
	if err != nil {
		t.Fatalf("no owner note was written: %v", err)
	}
	var note struct {
		Type   string            `json:"type"`
		Text   string            `json:"text"`
		Notice map[string]string `json:"notice"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(body))), &note); err != nil {
		t.Fatalf("owner note is not valid JSONL: %v\n%s", err, body)
	}
	if note.Type != "agent_note" {
		t.Errorf("type=%q want agent_note so the cockpit renders it", note.Type)
	}
	if note.Notice["kind"] != "recovery-gave-up" || note.Notice["subject"] != "jv-t999-stuck" {
		t.Errorf("notice metadata wrong: %+v", note.Notice)
	}
	for _, want := range []string{"gave up", "iteration cap", "still stuck", "needs a human"} {
		if !strings.Contains(note.Text, want) {
			t.Errorf("owner note missing %q:\n%s", want, note.Text)
		}
	}
	// The marker is for a live daemon to reconcile; nothing may depend on it.
	if _, err := os.Stat(filepath.Join(state, "recovery", "jv-t999-stuck.gaveup.json")); err != nil {
		t.Errorf("marker not written: %v", err)
	}
}

// Resolution is evidence, not assessment. Declaring victory early leaves
// a broken fleet under a resolved notice, and nothing else will look.
func TestResolvedRequiresARecentTurn(t *testing.T) {
	state := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	sid := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	writeRegistry(t, state, "jv-x", sid)

	// No transcript: the phantom-session condition, a fault not a recovery.
	if ok, why := Resolved(state, "jv-x", time.Now()); ok {
		t.Errorf("resolved with no transcript (%s)", why)
	}

	proj := filepath.Join(home, ".claude", "projects", "-tmp-x")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	tr := filepath.Join(proj, sid+".jsonl")
	if err := os.WriteFile(tr, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Fresh transcript: running and producing.
	if ok, _ := Resolved(state, "jv-x", time.Now()); !ok {
		t.Error("a turn written just now should count as resolved")
	}
	// Stale transcript from before the incident must NOT read as recovery.
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(tr, old, old); err != nil {
		t.Fatal(err)
	}
	if ok, why := Resolved(state, "jv-x", time.Now()); ok {
		t.Errorf("a two-hour-old transcript read as resolved (%s)", why)
	}
	// An unknown agent is not resolved either.
	if ok, _ := Resolved(state, "nobody", time.Now()); ok {
		t.Error("an agent with no registry row cannot be resolved")
	}
}

// The brief must not send a detached process at tools that die with the
// daemon, and must carry the do-not-repair instruction.
func TestBriefTargetsPrimitivesNotMCP(t *testing.T) {
	// Normalised: the assertions are about what the brief SAYS, not how
	// it wraps. A line break inside a sentence should not fail a test
	// about instructions.
	b := strings.Join(strings.Fields(Brief("jv-x", "/tmp/state", "/repo")), " ")
	for _, want := range []string{
		"DO NOT restart, kill, or otherwise unstick",
		"destroys the evidence",
		"Do NOT rely on jevons MCP tools",
		"/tmp/state/agents.json",
		"daily-jevonsd.log",
		"honest \"unknown\"",
	} {
		if !strings.Contains(b, want) {
			t.Errorf("brief missing %q", want)
		}
	}
	// It is allowed to restart the daemon — that is what detachment buys.
	if !strings.Contains(b, "restart-daily-jevonsd.sh") {
		t.Error("brief does not tell it that restarting the daemon is permitted")
	}
}

func TestRunRequiresStuckAgent(t *testing.T) {
	if code := run([]string{}); code != 2 {
		t.Errorf("exit=%d want 2 when -stuck is missing", code)
	}
}

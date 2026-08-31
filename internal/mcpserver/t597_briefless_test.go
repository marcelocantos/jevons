// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agentreport"
)

// 🎯T597 tapes. The 2026-08-31 incident: after a daemon restart, the registry
// held session ids that differed from the spawn results, jevons_transcript_read
// answered a bare "transcript not found", the PO applied the 🎯T416 born-stuck
// instrument (registry-named session with no file ⇒ never begun a turn) and
// re-sent full opening briefs to two seats that were mid-implementation.

const t597SpawnBrief = "```jevons\n" +
	"jevons: kind spawn-brief\n" +
	"jevons: phase implement\n" +
	"jevons: target T999\n" +
	"```\n" +
	"🎯T999: do the thing. Read bullseye_get T999 for context.\n"

// t597Fixture builds a Server with one registered seat whose session id has
// no transcript file (as after a restart remint), a workdir the test controls,
// and the full hermetic send fabric (fakeSender + turn witness).
func t597Fixture(t *testing.T, name string) (*Server, *fakeSender, string) {
	t.Helper()
	workdir := t.TempDir()

	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: workdir, SessionID: "sess-reminted-at-restart",
		Materialized: true, Provider: "grok", Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}

	sender := &fakeSender{alive: true}
	s, _ := chainServer(t, map[string]*fakeSender{name: sender})
	s.registry = reg
	s.fleetBriefed = map[string]bool{name: true}
	// Daemon "restarted" an hour ago; the seat predates the restart (no
	// recorded mint), so activity is measured from boot.
	s.bootAt = time.Now().Add(-time.Hour)
	s.transcript = &TranscriptOps{
		Read: func(sessionID string) ([]map[string]any, error) {
			return nil, fmt.Errorf("transcript not found for session %q", sessionID)
		},
		Locate: func(sessionID string) (string, []string) {
			return "", []string{"/state/claude/projects/*/" + sessionID + ".jsonl"}
		},
	}
	return s, sender, workdir
}

func t597Send(t *testing.T, s *Server, name, text string, force bool) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{}
	args := map[string]any{"name": name, "text": text, "actor": "jevons-po"}
	if force {
		args["force_rebrief"] = true
	}
	req.Params.Arguments = args
	res, err := s.handleAgentSend(context.Background(), req)
	if err != nil {
		t.Fatalf("handleAgentSend: %v", err)
	}
	return res
}

// Tape A: the session id changed at restart while the agent kept working —
// its workdir shows a fresh touch. transcript_read must answer ACTIVE (not an
// error that reads as born-stuck), name the searched path, and a spawn-brief
// re-send must be refused with the evidence and the override.
func TestT597ActiveSeatIsNotBornStuckAndRebriefRefused(t *testing.T) {
	const name = "jv-t586-sentinel-capacity"
	s, sender, workdir := t597Fixture(t, name)

	// The seat's work: a file touched now, well after daemon boot.
	if err := os.WriteFile(filepath.Join(workdir, "pofanout.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := callTranscriptRead(t, s, name)
	text := toolText(res)
	if res.IsError {
		t.Fatalf("ACTIVE seat must not come back as an error-shaped not-found:\n%s", text)
	}
	for _, want := range []string{"ACTIVE", "pofanout.go", "sess-rem", "/state/claude/projects/", "do NOT"} {
		if !strings.Contains(text, want) {
			t.Errorf("transcript_read verdict missing %q:\n%s", want, text)
		}
	}

	// The spawn-brief re-send is refused, naming evidence and override.
	sendRes := t597Send(t, s, name, t597SpawnBrief, false)
	if !sendRes.IsError {
		t.Fatalf("spawn-brief to an active seat must be refused: %s", toolText(sendRes))
	}
	refusal := toolText(sendRes)
	for _, want := range []string{"re-brief refused", "🎯T597", "pofanout.go", "force_rebrief=true"} {
		if !strings.Contains(refusal, want) {
			t.Errorf("refusal missing %q:\n%s", want, refusal)
		}
	}
	if len(sender.sent) != 0 {
		t.Fatalf("refused re-brief must not reach the seat: %v", sender.sent)
	}

	// The named override delivers.
	forced := t597Send(t, s, name, t597SpawnBrief, true)
	if forced.IsError {
		t.Fatalf("force_rebrief=true must deliver: %s", toolText(forced))
	}
	if len(sender.sent) != 1 {
		t.Fatalf("forced re-brief must reach the seat once: %v", sender.sent)
	}
}

// A stored report is seat activity too — the other half of the acceptance's
// evidence clause, without any workdir touch.
func TestT597StoredReportCountsAsActivity(t *testing.T) {
	const name = "jv-t592-chatlog-turns"
	s, sender, _ := t597Fixture(t, name)

	stateDir := t.TempDir()
	if _, err := agentreport.Save(stateDir, name, "checkpoint: mechanism established", time.Now()); err != nil {
		t.Fatal(err)
	}
	s.SetAgentReportDir(stateDir)

	res := callTranscriptRead(t, s, name)
	if res.IsError || !strings.Contains(toolText(res), "ACTIVE") {
		t.Fatalf("stored report must classify ACTIVE:\n%s", toolText(res))
	}

	sendRes := t597Send(t, s, name, t597SpawnBrief, false)
	if !sendRes.IsError || !strings.Contains(toolText(sendRes), "stored report") {
		t.Fatalf("refusal must cite the stored report: err=%v %s", sendRes.IsError, toolText(sendRes))
	}
	if len(sender.sent) != 0 {
		t.Fatalf("refused re-brief must not reach the seat: %v", sender.sent)
	}
}

// Tape B: a genuinely unbriefed seat — no reports, no touched files, no
// transcript — still classifies born-stuck-consistent, and the brief lands.
func TestT597UnbriefedSeatAcceptsBrief(t *testing.T) {
	const name = "jv-t999-fresh"
	s, sender, _ := t597Fixture(t, name)
	// Workdir is an empty TempDir: directories don't count, so no touches.
	// No report store is wired. bootAt is an hour back.

	res := callTranscriptRead(t, s, name)
	text := toolText(res)
	if !res.IsError {
		t.Fatalf("a seat with no evidence keeps the error-shaped not-found:\n%s", text)
	}
	for _, want := range []string{"born-stuck", "no stored reports", "/state/claude/projects/"} {
		if !strings.Contains(text, want) {
			t.Errorf("not-found verdict missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Seat activity: ACTIVE") {
		t.Fatalf("no evidence must not read ACTIVE:\n%s", text)
	}

	sendRes := t597Send(t, s, name, t597SpawnBrief, false)
	if sendRes.IsError {
		t.Fatalf("brief to a genuinely unbriefed seat must be accepted: %s", toolText(sendRes))
	}
	if len(sender.sent) != 1 || !strings.Contains(sender.sent[0], "kind spawn-brief") {
		t.Fatalf("brief must reach the seat: %v", sender.sent)
	}
}

// Stale activity does not block: evidence older than RecentActivityWindow
// leaves the re-brief decision with the caller.
func TestT597StaleActivityDoesNotRefuse(t *testing.T) {
	const name = "jv-t998-stale"
	s, sender, workdir := t597Fixture(t, name)

	old := time.Now().Add(-2 * RecentActivityWindow)
	p := filepath.Join(workdir, "old.go")
	if err := os.WriteFile(p, []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	// Touched after boot (so ACTIVE for transcript_read) but not recent.
	s.bootAt = time.Now().Add(-3 * RecentActivityWindow)

	sendRes := t597Send(t, s, name, t597SpawnBrief, false)
	if sendRes.IsError {
		t.Fatalf("stale activity must not refuse: %s", toolText(sendRes))
	}
	if len(sender.sent) != 1 {
		t.Fatalf("brief must reach the seat: %v", sender.sent)
	}
}

func TestT597IsFullRebrief(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"spawn-brief envelope", t597SpawnBrief, true},
		{"big opening prose", "[Who you are — from the fleet registry]\nYou are jv-t1-x.\n" + strings.Repeat("Mission context. ", 100), true},
		{"big prose without markers", strings.Repeat("status update line. ", 100), false},
		{"small mention of spawn-brief", "your last spawn-brief said T10 — status?", false},
		{"ordinary direct", "please commit and report", false},
	}
	for _, c := range cases {
		if got, _ := IsFullRebrief(c.text); got != c.want {
			t.Errorf("%s: IsFullRebrief=%v want %v", c.name, got, c.want)
		}
	}
}

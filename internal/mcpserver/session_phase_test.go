// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/turnev"
)

func TestT423ClassifyAgentSessionPhaseReadsCurrentSession(t *testing.T) {
	dir := t.TempDir()
	sid := "01234567-89ab-4def-8123-456789abcdef"
	proj := filepath.Join(dir, "projects", "bucket")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"user","message":{"role":"user","content":"go"}}` + "\n" +
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}}` + "\n"
	path := filepath.Join(proj, sid+".jsonl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	d := claudia.AgentDef{Name: "jv-t423", SessionID: sid, WorkDir: dir}
	roots := discovery.Roots{ClaudeProjects: filepath.Join(dir, "projects")}
	if got := ClassifyAgentSessionPhase(d, roots); got != turnev.PhaseIdle {
		t.Fatalf("ended session classified %s, want idle (path=%s)", got, AgentTranscriptPath(d, roots))
	}
	// A stale id is unknown, not idle.
	d.SessionID = "00000000-0000-4000-8000-000000000000"
	if got := ClassifyAgentSessionPhase(d, roots); got != turnev.PhaseUnknown {
		t.Fatalf("stale id classified %s, want unknown", got)
	}
}

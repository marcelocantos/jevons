// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/transcript"
)

func TestInspectHydratesFromSidecarSpool(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("JEVONS_SPOOL_DIR", dir)
	const (
		name = "jv-t866-inspect"
		sid  = "00000000-0000-4000-8000-000000000866"
	)
	if err := os.WriteFile(filepath.Join(dir, "events-2026-09-25.log"), []byte(
		`{"ts":"2026-09-25T00:00:00.000Z","seat":"jv-t866-inspect","type":"text","text":"hello from sidecar spool"}`+"\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	vendorRoot := filepath.Join(dir, "claude-projects")
	vendor := filepath.Join(vendorRoot, "work-repo", sid+".jsonl")
	if err := os.MkdirAll(filepath.Dir(vendor), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vendor, []byte(
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"vendor jsonl must not paint"}]}}`+"\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}

	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: sid, Provider: claudia.ProviderCursor,
		Purpose: claudia.PurposeWork, Parent: "jevons-po",
	}); err != nil {
		t.Fatal(err)
	}
	s := New("test", dir)
	s.SetRegistry(reg)
	s.SetTranscriptReader(transcript.NewReaderRoots(discovery.Roots{ClaudeProjects: vendorRoot}))

	joined := strings.Join(replayRoleRows(inspectReplay(t, s, name)), " | ")
	if !strings.Contains(joined, "hello from sidecar spool") {
		t.Fatalf("inspect missed the dated spool: %s", joined)
	}
	if strings.Contains(joined, "vendor jsonl must not paint") {
		t.Fatalf("inspect painted vendor JSONL for a sidecar seat: %s", joined)
	}
}

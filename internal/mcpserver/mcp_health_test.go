// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/mcphealth"
)

// writeClaudeConfig writes a user-scope Claude config carrying the given
// servers and points HOME at it, so mcpRegistrationsFor reads the fixture.
func writeClaudeConfig(t *testing.T, servers map[string]any) {
	t.Helper()
	home := t.TempDir()
	doc := map[string]any{"mcpServers": servers}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("HOME", home)
	mcpHealthResetForTest()
	t.Cleanup(mcpHealthResetForTest)
}

func liveURL(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	return fmt.Sprintf("http://%s/mcp", ln.Addr().String())
}

func closedURL(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return fmt.Sprintf("http://%s/mcp", addr)
}

// 🎯T379 acceptance 1: the daemon reads the registrations an agent will
// actually inherit and says something when one is dead. This is the wiring
// half — mcphealth can classify correctly and still be worthless if nothing
// in the product calls it.
func TestMCPHealthNoteReportsDeadUserScopeRegistration(t *testing.T) {
	writeClaudeConfig(t, map[string]any{
		"jevonsmcp":         map[string]any{"type": "http", "url": liveURL(t)},
		"jevonsmcp-journey": map[string]any{"type": "http", "url": closedURL(t)},
		"sawmill":           map[string]any{"command": "/opt/homebrew/bin/mcpbridge"},
	})

	note := mcpHealthNote(claudia.ProviderClaude)
	if note == "" {
		t.Fatal("a dead user-scope registration must produce a note, got silence")
	}
	if !strings.Contains(note, "jevonsmcp-journey") {
		t.Errorf("note must name the dead server: %q", note)
	}
	if strings.Contains(note, "sawmill") {
		t.Errorf("stdio server has no endpoint and must not be reported: %q", note)
	}
}

// The over-broadness half: a config in which every registration resolves
// must produce no note at all. Without this, "report everything as dead"
// passes the test above and the signal is worthless.
func TestMCPHealthNoteSilentWhenAllRegistrationsResolve(t *testing.T) {
	writeClaudeConfig(t, map[string]any{
		"jevonsmcp": map[string]any{"type": "http", "url": liveURL(t)},
		"mnemo":     map[string]any{"type": "http", "url": liveURL(t)},
		"sawmill":   map[string]any{"command": "/opt/homebrew/bin/mcpbridge"},
	})

	if note := mcpHealthNote(claudia.ProviderClaude); note != "" {
		t.Errorf("healthy config must produce no note, got %q", note)
	}
}

// Grok keeps its user-scope list in TOML, which this package does not parse.
// That is a declared residual, and it must read as "no opinion" rather than
// "all healthy" — so a Grok agent is never given false assurance.
func TestGrokIsDeclaredResidualNotFalseHealthy(t *testing.T) {
	writeClaudeConfig(t, map[string]any{
		"jevonsmcp-journey": map[string]any{"type": "http", "url": closedURL(t)},
	})

	if _, ok := mcpRegistrationsFor(claudia.ProviderGrok); ok {
		t.Error("grok registrations are not readable; must report ok=false")
	}
	if note := mcpHealthNote(claudia.ProviderGrok); note != "" {
		t.Errorf("grok path has no data and must stay silent, got %q", note)
	}
}

// Wiring ratchet: agent start must actually consult the health check.
//
// The tests above prove mcpHealthNote classifies correctly, but every one of
// them would still pass if the call were deleted from handleAgentStart —
// which is precisely the pre-fix state, where the detection existed nowhere
// and nothing looked. Driving handleAgentStart itself would need a real
// provider launch, so this asserts the call site from source instead. It is
// a ratchet, not a behavioural test, and it is labelled as one.
func TestAgentStartConsultsMCPHealth(t *testing.T) {
	src, err := os.ReadFile("agents.go")
	if err != nil {
		t.Fatalf("read agents.go: %v", err)
	}
	start := strings.Index(string(src), "func (s *Server) handleAgentStart(")
	if start < 0 {
		t.Fatal("handleAgentStart not found — update this ratchet")
	}
	body := string(src)[start:]
	if end := strings.Index(body, "\nfunc "); end > 0 {
		body = body[:end]
	}
	if !strings.Contains(body, "noteAgentMCPHealth") {
		t.Error("handleAgentStart must call noteAgentMCPHealth: an agent that " +
			"inherits a dead MCP registration has to be told so at start (🎯T379)")
	}
}

// The note text an agent_start result carries has to be self-explanatory to
// whoever spawned the agent: which server, which URL, and why it is dead.
func TestDeadNoteCarriesActionableDetail(t *testing.T) {
	url := closedURL(t)
	note := mcpHealthNoteFromRegs([]mcphealth.Registration{
		{Name: "jevonsmcp-journey", Transport: "http", URL: url},
	})
	if note == "" {
		t.Fatal("want a note for a closed port")
	}
	for _, want := range []string{"jevonsmcp-journey", url, "nothing is listening"} {
		if !strings.Contains(note, want) {
			t.Errorf("note %q missing %q", note, want)
		}
	}
}

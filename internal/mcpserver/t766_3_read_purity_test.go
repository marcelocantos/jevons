// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/mark3labs/mcp-go/mcp"
)

// T766.3 exercises the real Registry and Server routes. Stop the returned
// stub handles rather than Registry.Stop: this leaves a genuine dead handle
// in the registry, with both recovery plans (Launch and Remove) eligible.
func TestT766_3ListSendAndSamplesDoNotRecoverOtherSeats(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	starts := map[string]int{}
	reg.SetLaunchers(&claudia.RegistryLaunchers{Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
		starts[cfg.Name]++
		return claudia.StartStub(ctx, cfg, nil)
	}})
	for _, def := range []claudia.AgentDef{
		{Name: "other-auto", AutoStart: true, Purpose: claudia.PurposeAside},
		{Name: "other-work", Purpose: claudia.PurposeWork},
	} {
		def.WorkDir, def.Provider, def.SessionID, def.TermLogPath = dir, claudia.ProviderClaude, "session-"+def.Name, "-"
		if err := reg.Register(def); err != nil {
			t.Fatal(err)
		}
		proc, err := reg.Launch(def.Name)
		if err != nil {
			t.Fatal(err)
		}
		proc.Stop() // dead handle, not a deliberate registry stop
	}
	t.Cleanup(reg.StopAll)
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	if _, err := s.handleAgentList(context.Background(), mcp.CallToolRequest{}); err != nil {
		t.Fatal(err)
	}
	if starts["other-auto"] != 1 || reg.Def("other-work") == nil {
		t.Fatalf("list actuated unrelated seats: starts=%v work=%v", starts, reg.Def("other-work"))
	}
	s.sampleSentinel(SentinelLoopArgs{Server: s, DryRun: true}, time.Now())
	s.sampleStaffOps(0)
	if starts["other-auto"] != 1 || reg.Def("other-work") == nil {
		t.Fatalf("sample actuated unrelated seats: starts=%v work=%v", starts, reg.Def("other-work"))
	}
	// A failed named delivery may report not-running, but cannot sweep either
	// unrelated seat. The requested seat itself is still subject to its own
	// intent and admission checks in ensureAgentProcess.
	if _, _, err := s.ensureAgentProcess("missing-requested"); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("named delivery result: %v", err)
	}
	if starts["other-auto"] != 1 || reg.Def("other-work") == nil {
		t.Fatalf("named delivery actuated unrelated seats: starts=%v work=%v", starts, reg.Def("other-work"))
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/claudia"
)

// 🎯T1096: the frontier loop re-mints a killed auto worker under the same
// deterministic name. On 2026-10-21 jv-t1104-auto was killed and its re-mints
// failed with "load seat identity mismatch": the sidecar still held the
// killed seat's session. Claudia now frees the name on a registry removal and
// replaces a stale sidecar seat on a fresh start — but only a start that is
// fresh: a new session, no required resume. This pins Jevons' half of that
// contract through the real kill and spawn call sites: the kill is a
// registry removal (not a stop), and the re-mint registers a new session and
// launches without RequireResume. A re-mint that kept the killed session, or
// demanded its conversation, would be refused by Claudia by design.
func TestT1096KilledAutoWorkerRemintsFresh(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	var mu sync.Mutex
	var launches []claudia.Config
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			mu.Lock()
			launches = append(launches, cfg)
			mu.Unlock()
			return claudia.StartStub(ctx, cfg, nil)
		},
	})
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	t.Cleanup(reg.StopAll)

	name := FrontierWorkerName("T1104")
	workdir := t.TempDir()
	if err := s.spawnFrontierWorker(name, workdir, "jevons-po", "T1104", ""); err != nil {
		t.Fatalf("first spawn: %v", err)
	}
	killed := reg.Def(name)
	if killed == nil || killed.SessionID == "" {
		t.Fatalf("first spawn registered %+v", killed)
	}
	killedSession := killed.SessionID

	res, err := s.handleAgentKill(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{"name": name, "actor": name, "force": true},
	}})
	if err != nil || res.IsError {
		t.Fatalf("kill: %v %+v", err, res)
	}
	if reg.Def(name) != nil {
		t.Fatal("kill left the definition registered: it stopped the seat instead of removing it, so Claudia never frees the name")
	}

	if err := s.spawnFrontierWorker(name, workdir, "jevons-po", "T1104", ""); err != nil {
		t.Fatalf("re-mint of a killed name: %v", err)
	}
	remint := reg.Def(name)
	if remint == nil || remint.SessionID == "" || remint.SessionID == killedSession {
		t.Fatalf("re-mint session = %+v, want a fresh session (killed one was %q)", remint, killedSession)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(launches) != 2 {
		t.Fatalf("launches = %d, want the first spawn and the re-mint", len(launches))
	}
	if got := launches[1]; got.Name != name || got.SessionID != remint.SessionID || got.RequireResume {
		t.Fatalf("re-mint launch = name %q session %q require_resume %v; want a fresh, non-resume start of %q on %q",
			got.Name, got.SessionID, got.RequireResume, name, remint.SessionID)
	}
}

// Acceptance clause 2: kill, change provider, start the same name again.
func TestT1096KilledNameStartsWithDifferentProvider(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	var launches []claudia.Config
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(ctx context.Context, cfg claudia.Config) (*claudia.Agent, error) {
			launches = append(launches, cfg)
			return claudia.StartStub(ctx, cfg, nil)
		},
	})
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	t.Cleanup(reg.StopAll)

	const name = "jv-t39015-po"
	workdir := t.TempDir()
	first, _, _, err := s.stitchAgentStart(
		name, workdir, "", string(claudia.ProviderGrok), "", "jevons-po", claudia.PurposeWork, "T1096", "",
	)
	if err != nil {
		t.Fatalf("first start: %v", err)
	}
	if _, err := reg.Launch(name); err != nil {
		t.Fatalf("first launch: %v", err)
	}
	if first.Provider != "xai-oauth" {
		t.Fatalf("first provider = %q, want xai-oauth (grok mint)", first.Provider)
	}

	res, err := s.handleAgentKill(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{
		Arguments: map[string]any{"name": name, "actor": name, "force": true},
	}})
	if err != nil || res.IsError {
		t.Fatalf("kill: %v %+v", err, res)
	}
	if reg.Def(name) != nil {
		t.Fatal("kill must deregister so the name can start on another provider")
	}

	again, existed, _, err := s.stitchAgentStart(
		name, workdir, "", "claude", "", "jevons-po", claudia.PurposeWork, "T1096", "",
	)
	if err != nil {
		t.Fatalf("re-start after kill: %v", err)
	}
	if existed {
		t.Fatal("re-start after kill must register a fresh row, not resume the old one")
	}
	if again.Provider != "anthropic" {
		t.Fatalf("re-start provider = %q, want anthropic (claude mint)", again.Provider)
	}
	if _, err := reg.Launch(name); err != nil {
		t.Fatalf("re-start launch: %v", err)
	}
	if len(launches) != 2 || launches[1].Provider != "anthropic" {
		t.Fatalf("launches = %+v, want xai-oauth then anthropic", launches)
	}
}

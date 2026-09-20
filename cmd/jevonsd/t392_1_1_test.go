// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/ctxcap"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/handover"
	"github.com/marcelocantos/jevons/internal/server"
	"github.com/marcelocantos/jevons/internal/upgrade"
)

// 🎯T392.1.1 — governor pass over an agent at the 2026-08-15 incident
// number (105336 tokens) must not remint. Observation may remain; acting
// on context size by rotate() may not.
func TestGovernorPassAt105336LeavesSessionUnchanged(t *testing.T) {
	const (
		name      = "jv-t392.1.1-spend"
		sessionID = "019fd13d-e500-7913-b96c-981e50aa2e99"
		tokens    = int64(105_336)
	)
	dir := t.TempDir()
	workDir := filepath.Join(dir, "work")
	grokSessions := filepath.Join(dir, "grok-sessions")
	writeGrokUsage(t, grokSessions, workDir, sessionID, tokens)

	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: workDir, SessionID: sessionID,
		Provider: claudia.ProviderGrok, Materialized: true, AutoStart: false,
	}); err != nil {
		t.Fatal(err)
	}
	reg.SetDirect(true)
	reg.SetLaunchers(&claudia.RegistryLaunchers{
		Start: func(_ context.Context, _ claudia.Config) (*claudia.Agent, error) {
			return claudia.NewStubAgent(nil), nil
		},
	})
	if _, err := reg.Launch(name); err != nil {
		t.Fatalf("stub launch: %v", err)
	}
	t.Cleanup(func() { reg.Stop(name) })
	if reg.Get(name) == nil {
		t.Fatal("stub launch left Get() nil — pass would skip the seat")
	}

	roots := discovery.Roots{GrokSessions: grokSessions}
	obs := ctxcap.Observer{Roots: roots}
	got := obs.Observe(ctxcap.AgentRef{
		Name: name, Provider: "grok", WorkDir: workDir, SessionID: sessionID,
	})
	if !got.HasContext || got.Context != tokens {
		t.Fatalf("fixture context=%d has=%v want %d/true", got.Context, got.HasContext, tokens)
	}

	rotations := handover.NewRotationStore(filepath.Join(dir, "rotations"))
	handovers := handover.NewStore(filepath.Join(dir, "handover"))
	adapter := fleet.NewClaudia(reg)
	adapter.SetSessionRoots(roots)
	adapter.SetHandoverStore(handovers)
	adapter.SetRotationStore(rotations)
	srv := server.New("test", dir)

	cfg := config.Config{
		ContextCeilingTokens:  100_000,
		ContextCeilingEnabled: true, // the withdrawn remint path, opted back in
	}
	pol := ctxcap.Policy{
		Ceiling:  cfg.ContextCeilingTokens,
		Disabled: !cfg.ContextCeilingEnforced(),
	}
	var reported []string
	report := func(agent, _, text string) {
		reported = append(reported, agent+":"+text)
	}
	before := upgrade.SessionSnapshot(reg)
	var latch sync.Map
	contextCeilingPass(cfg, pol, obs, reg, adapter, srv, map[string]time.Time{}, rotations, report, &latch)

	after := upgrade.SessionSnapshot(reg)
	if drift := upgrade.SessionDrift(before, after); len(drift) != 0 {
		t.Fatalf("governor pass reminted: %v", drift)
	}
	if _, ok, err := rotations.Get(name); err != nil || ok {
		t.Fatalf("governor pass wrote a rotation: ok=%v err=%v", ok, err)
	}
	if _, ok, err := handovers.Get(name); err != nil || ok {
		t.Fatalf("governor pass wrote a handover: ok=%v err=%v", ok, err)
	}
	if _, ok, err := adapter.SeedSuccessor(name); err != nil || ok {
		t.Fatalf("SeedSuccessor after pass: ok=%v err=%v", ok, err)
	}
	if len(reported) == 0 {
		t.Fatal("enforced pass over 105336 did not report unworkable — fixture never evaluated")
	}

	// Mutation aliveness: a compact-rotate of this seat must be visible
	// to the same checks. If SessionDrift goes deaf, this oracle is dead.
	mutated := upgrade.SessionSnapshot(reg)
	mutated[name] = uuid.NewString()
	if drift := upgrade.SessionDrift(before, mutated); len(drift) == 0 {
		t.Fatal("SessionDrift did not see a compact rotate — the 🎯T392.1.1 oracle is dead")
	}
	if err := rotations.Put(handover.Rotation{Agent: name, Kind: handover.KindCompact}); err != nil {
		t.Fatal(err)
	}
	gotRot, ok, err := rotations.Get(name)
	if err != nil || !ok || gotRot.Kind != handover.KindCompact {
		t.Fatalf("mutation control: KindCompact write not readable: ok=%v err=%v %+v", ok, err, gotRot)
	}
}

func writeGrokUsage(t *testing.T, grokRoot, workDir, sessionID string, inputTokens int64) {
	t.Helper()
	dir := filepath.Join(grokRoot, discovery.EncodeCWDBucket(workDir), sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := fmt.Sprintf(`{"timestamp":1786154000,"method":"_x.ai/session/update","params":{"sessionId":%q,"update":{"sessionUpdate":"turn_completed","prompt_id":"p","stop_reason":"end_turn","usage":{"inputTokens":%d,"outputTokens":10,"cachedReadTokens":0,"modelCalls":1,"costUsdTicks":1,"modelUsage":{"grok-4.5-build":{"totalTokens":%d}}}}}}`,
		sessionID, inputTokens, inputTokens)
	if err := os.WriteFile(filepath.Join(dir, "updates.jsonl"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

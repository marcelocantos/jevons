// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/handover"
)

// 🎯T625.9: a Grok seat's session lives under claudia's grok-homes tree, not
// cfg.SessionsDir. Migrate must find it through the daemon's shared roots.
func TestT625_9MigrateFindsGrokHomesSession(t *testing.T) {
	const sid = "019fd13d-e500-7913-b96c-981e50aa7625"
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	homes := discovery.ClaudiaGrokHomesRoot()
	bucket := filepath.Join(discovery.ClaudiaGrokHomeSessionsDir(homes, sid), discovery.EncodeCWDBucket("/work/repo"), sid)
	if err := os.MkdirAll(bucket, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"method":"session/update","params":{"update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"hello"}}}}` + "\n"
	if err := os.WriteFile(filepath.Join(bucket, "updates.jsonl"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	build := func(roots discovery.Roots) *Claudia {
		reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := reg.Register(claudia.AgentDef{
			Name: "jevons-po", WorkDir: "/work/repo", SessionID: sid,
			Provider: claudia.ProviderGrok, Materialized: true, Purpose: claudia.PurposeWork,
		}); err != nil {
			t.Fatal(err)
		}
		f := NewClaudia(reg)
		f.SetSessionRoots(roots)
		f.SetHandoverStore(handover.NewStore(filepath.Join(t.TempDir(), "handover")))
		f.migrationTransfer = func(claudia.MigrationTransferArgs) (claudia.MigrationTransferResult, error) {
			return claudia.MigrationTransferResult{Brief: "- user: hello"}, nil
		}
		return f
	}

	// Control: bare roots (the pre-fix wiring) cannot see the session.
	bare := build(discovery.Roots{GrokSessions: filepath.Join(dir, "grok-sessions")})
	if _, err := bare.PrepareMigration("jevons-po", claudia.ProviderClaude, false); err == nil {
		t.Fatal("bare roots unexpectedly found a grok-homes session")
	}

	f := build(discovery.DaemonRoots(filepath.Join(dir, "grok-sessions"), filepath.Join(dir, "claude")))
	if _, err := f.PrepareMigration("jevons-po", claudia.ProviderClaude, false); err != nil {
		t.Fatalf("migrate with daemon roots: %v", err)
	}
}

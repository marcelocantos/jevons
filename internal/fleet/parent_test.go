// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/thread"
)

// 🎯T111.3: registry dual-write carries Parent from the thread record.
func TestRegisterParentFromThread(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	th := &thread.Thread{
		ID:      "spawned-po",
		WorkDir: t.TempDir(),
		Parent:  "jevons",
		Purpose: thread.PurposeAside,
	}
	if err := reg.Register(claudia.AgentDef{
		Name:      th.ID,
		WorkDir:   th.WorkDir,
		SessionID: "placeholder",
		AutoStart: true,
		Parent:    th.Parent,
		Purpose:   th.Purpose,
		Provider:  "grok",
	}); err != nil {
		t.Fatal(err)
	}
	def := reg.Def(th.ID)
	if def == nil || def.Parent != "jevons" {
		t.Fatalf("parent=%v want jevons", def)
	}

	// Backfill empty parent (Launch path for legacy rows).
	th2 := &thread.Thread{ID: "legacy", WorkDir: t.TempDir(), Parent: "jevons"}
	if err := reg.Register(claudia.AgentDef{
		Name: th2.ID, WorkDir: th2.WorkDir, SessionID: "s2", Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	if def := reg.Def(th2.ID); def != nil && def.Parent == "" && th2.Parent != "" {
		def.Parent = th2.Parent
		_ = reg.Register(*def)
	}
	if got := reg.Def(th2.ID).Parent; got != "jevons" {
		t.Fatalf("backfill parent=%q", got)
	}

	// Exists implements Participants for agent-only push.
	f := NewClaudia(reg)
	if !f.Exists("spawned-po") {
		t.Fatal("Exists should be true for registered agent")
	}
	if f.Exists("missing") {
		t.Fatal("Exists should be false for unknown")
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/handover"
)

func TestBounceReloadKeepsSessionIDs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agents.json")
	reg, err := claudia.NewRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	const overseerID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	const workerID = "11111111-2222-3333-4444-555555555555"
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", WorkDir: dir, SessionID: overseerID,
		Provider: claudia.ProviderGrok, Materialized: true, AutoStart: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", WorkDir: dir, SessionID: workerID,
		Provider: claudia.ProviderGrok, Materialized: true, AutoStart: true,
	}); err != nil {
		t.Fatal(err)
	}
	before := SessionSnapshot(reg)

	// Simulated bounce: a new process opens the same persist file.
	reloaded, err := SessionSnapshotFromFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if drift := SessionDrift(before, reloaded); len(drift) != 0 {
		t.Fatalf("reload drifted session ids: %v", drift)
	}

	// Mutation: Launch that mints a fresh session on an unchanged provider
	// must go RED — this is the ghost-fleet failure T40.2 exists to catch.
	minted := map[string]string{}
	for name := range before {
		minted[name] = uuid.NewString()
	}
	if drift := SessionDrift(before, minted); len(drift) == 0 {
		t.Fatal("SessionDrift did not see a mint — the bounce oracle is dead")
	}
	if names := SessionDriftNames(before, minted); len(names) != len(before) {
		t.Fatalf("SessionDriftNames = %v, want every reminted row", names)
	}

	// Bounce is not a migrate: no handover record is created for these rows.
	store := handover.NewStore(filepath.Join(dir, "handover"))
	if _, ok, err := store.Get("jevons"); err != nil || ok {
		t.Fatalf("bounce left a handover for jevons: ok=%v err=%v", ok, err)
	}
}

func TestReattachFleetNilSafe(t *testing.T) {
	ReattachFleet(nil)
}

func TestReattachFleetReapsCursorThenPrefersAdopt(t *testing.T) {
	var orphans int
	oldOrphan := reapOrphanCursorACP
	t.Cleanup(func() { reapOrphanCursorACP = oldOrphan })
	reapOrphanCursorACP = func() []int {
		orphans++
		return nil
	}

	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons", WorkDir: t.TempDir(), SessionID: "sid-no-store",
		Provider: claudia.ProviderCursor, AutoStart: false,
	}); err != nil {
		t.Fatal(err)
	}
	// AutoStart false so PreferAdopt does not spawn a real cursor-agent.
	ReattachFleet(reg)
	if orphans != 1 {
		t.Fatalf("orphan reap calls = %d", orphans)
	}
}

func TestSessionSnapshotFromFileMissing(t *testing.T) {
	if _, err := SessionSnapshotFromFile(filepath.Join(t.TempDir(), "nope.json")); err != nil {
		// NewRegistry creates an empty file — a missing path is not an error
		// if the directory is writable. Drive the shipped loader either way.
		t.Log(err)
	}
	path := filepath.Join(t.TempDir(), "agents.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := SessionSnapshotFromFile(path); err == nil {
		t.Fatal("corrupt agents.json parsed")
	}
}

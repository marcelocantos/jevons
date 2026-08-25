// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/statedb"
)

func TestProjectAgentsReplaceOnNotify(t *testing.T) {
	db, err := statedb.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", Parent: "jevons", Purpose: "work", TargetID: "T548",
		SessionID: "sess-t548",
	}); err != nil {
		t.Fatal(err)
	}
	s := New("test", dir)
	s.SetStateDB(db)
	s.SetRegistry(reg)
	got, err := db.GetAgent("jevons-po")
	if err != nil || got == nil || got.TargetID != "T548" {
		t.Fatalf("projected=%+v err=%v", got, err)
	}
	if err := reg.Remove("jevons-po"); err != nil {
		t.Fatal(err)
	}
	s.NotifyAgentsChanged()
	if gone, _ := db.GetAgent("jevons-po"); gone != nil {
		t.Fatal("reaped agent still in statedb")
	}
}

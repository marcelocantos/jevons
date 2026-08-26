// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/statedb"
)

func TestSpawnReapListRefreshProjectsAgents(t *testing.T) {
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
	s := New("test", dir)
	s.SetStateDB(db)
	s.SetRegistry(reg)
	if list, _ := db.ListAgents(); len(list) != 0 {
		t.Fatalf("empty registry projected %v", list)
	}

	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t548.4-spawn", Parent: "jevons-po", Purpose: "work",
		TargetID: "T548.4", SessionID: "sess-spawn",
	}); err != nil {
		t.Fatal(err)
	}
	s.NotifyAgentsChanged()
	listed, err := db.ListAgents()
	if err != nil || len(listed) != 1 || listed[0].Name != "jv-t548.4-spawn" || listed[0].TargetID != "T548.4" {
		t.Fatalf("spawn projection list=%+v err=%v", listed, err)
	}

	if err := reg.Remove("jv-t548.4-spawn"); err != nil {
		t.Fatal(err)
	}
	s.NotifyAgentsChanged()
	if listed, _ = db.ListAgents(); len(listed) != 0 {
		t.Fatalf("reap left %v", listed)
	}
}

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

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleetintent"
)

func TestT1121APIAgentsCarriesPanelClass(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, d := range []claudia.AgentDef{
		{Name: "live", WorkDir: dir, SessionID: "s-live", Purpose: claudia.PurposeWork},
		{Name: "po-dead", WorkDir: dir, SessionID: "s-po", Purpose: claudia.PurposeWork, AutoStart: true},
		{Name: "reaped", WorkDir: dir, SessionID: "s-reaped", Purpose: claudia.PurposeWork},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	store, err := fleetintent.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetAgent("reaped", fleetintent.Reaped, "test", "done", time.Now()); err != nil {
		t.Fatal(err)
	}
	s := New("test", t.TempDir())
	s.SetRegistry(reg)
	s.SetFleetIntentSource(store.Snapshot)
	rows := s.decoratePanelClass([]agentInfo{
		{Name: "live", Running: true},
		{Name: "po-dead"},
		{Name: "reaped"},
	})
	if rows[0].Class != "live" || !rows[0].Visible {
		t.Fatalf("live row = class %q visible %v", rows[0].Class, rows[0].Visible)
	}
	if rows[1].Class != "fault" || !rows[1].Visible {
		t.Fatalf("fault row = class %q visible %v", rows[1].Class, rows[1].Visible)
	}
	if rows[2].Class != "history" || rows[2].Visible {
		t.Fatalf("history row = class %q visible %v", rows[2].Class, rows[2].Visible)
	}
}

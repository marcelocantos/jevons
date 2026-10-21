// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package seatreg

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestParkNonFleetAutoStartLeavesTheFleet(t *testing.T) {
	dir := t.TempDir()
	grants, err := New(filepath.Join(dir, GrantsFile))
	if err != nil {
		t.Fatal(err)
	}
	fleet, err := New(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatal(err)
	}
	for _, def := range []claudia.AgentDef{
		{Name: "jevons", Provider: claudia.ProviderGrok, AutoStart: true, SessionID: "fleet"},
		{Name: "leftover", Provider: claudia.ProviderClaude, AutoStart: true, SessionID: "old"},
	} {
		if err := grants.Register(def); err != nil {
			t.Fatal(err)
		}
	}
	if err := fleet.Register(claudia.AgentDef{Name: "jevons", Provider: claudia.Provider("xai-oauth"), SessionID: "fleet"}); err != nil {
		t.Fatal(err)
	}
	n, err := ParkNonFleetAutoStart(grants, fleet)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("parked %d, want leftover only", n)
	}
	if got := grants.Def("jevons"); got == nil || !got.AutoStart {
		t.Fatalf("fleet grant parked: %+v", got)
	}
	if got := grants.Def("leftover"); got == nil || got.AutoStart {
		t.Fatalf("leftover still AutoStart: %+v", got)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package brokerreg

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestAbsentCursorAliveSkipsRegisteredAndNonCursor(t *testing.T) {
	reg := mustReg(t)
	if err := reg.Register(claudia.AgentDef{Name: "jv-t1174-ok", SessionID: "s1", Provider: claudia.ProviderCursor}); err != nil {
		t.Fatal(err)
	}
	broker := []claudia.BrokerSeat{
		{Name: "jv-t1174-ok", Provider: claudia.ProviderCursor, Alive: true},
		{Name: "jv-compact-dead", Provider: claudia.ProviderCursor, Alive: true},
		{Name: "grok-only", Provider: claudia.ProviderGrok, Alive: true},
		{Name: "cursor-down", Provider: claudia.ProviderCursor},
	}
	got := AbsentCursorAlive(RegisteredSet(reg), broker)
	want := []claudia.BrokerSeat{{Name: "jv-compact-dead", Provider: claudia.ProviderCursor, Alive: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestFormatAgentListSectionEmpty(t *testing.T) {
	if FormatAgentListSection(nil) != "" {
		t.Fatal("expected empty section")
	}
}

func TestFormatAgentListSectionNamesSeat(t *testing.T) {
	out := FormatAgentListSection([]claudia.BrokerSeat{
		{Name: "jv-compact-1207c272", Provider: claudia.ProviderCursor, Alive: true},
	})
	if !strings.Contains(out, "jv-compact-1207c272") || !strings.Contains(out, "T1174") {
		t.Fatalf("section = %q", out)
	}
}

func mustReg(t *testing.T) *claudia.Registry {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

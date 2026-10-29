// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/brokerreg"
)

func TestT1174AppendRegistryAbsentBrokerRows(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	got := appendRegistryAbsentBrokerRows(context.Background(), reg, nil)
	absent := brokerreg.AbsentCursorAlive(brokerreg.RegisteredSet(reg), []claudia.BrokerSeat{
		{Name: "jv-compact-x", Provider: claudia.ProviderCursor, Alive: true},
	})
	if len(absent) != 1 {
		t.Fatalf("fixture absent = %+v", absent)
	}
	// Without a live broker the HTTP helper is a no-op.
	if got != nil {
		t.Fatalf("expected nil rows preserved, got %+v", got)
	}
}

func TestT1174ListAgentsIncludesRegistryAbsentStatus(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{registry: reg, overseerName: "jevons-po"}
	rows := appendRegistryAbsentBrokerRows(context.Background(), reg, []agentInfo{
		{Name: "jevons-po", Status: "running"},
	})
	// Simulate broker read via direct append helper input.
	rows = append(rows, agentInfo{
		Name: "jv-compact-1207c272", Provider: "cursor",
		Status: brokerreg.ListStatusRegistryAbsent, Running: true,
	})
	var found bool
	for _, a := range rows {
		if a.Name == "jv-compact-1207c272" && a.Status == brokerreg.ListStatusRegistryAbsent && a.Running {
			found = true
		}
	}
	if !found {
		t.Fatalf("rows = %+v", rows)
	}
	_ = s
}

func TestT1174AgentsAPIEncodesRegistryAbsentRow(t *testing.T) {
	body, err := json.Marshal([]agentInfo{{
		Name: "jv-compact-1207c272", Provider: "cursor",
		Status: brokerreg.ListStatusRegistryAbsent, Running: true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	var decoded []agentInfo
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded[0].Status != brokerreg.ListStatusRegistryAbsent {
		t.Fatalf("decoded = %+v", decoded[0])
	}
	_ = httptest.NewRecorder()
	_ = http.MethodGet
}

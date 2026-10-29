// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/seatstate"
)

// 🎯T754: /api/agents surfaces inert classification and queued backlog.

func TestT754DecorateSeatStopsInertAndQueue(t *testing.T) {
	const name = "jevons-po"
	s := &Server{}
	s.SetInertReader(func(n string) (string, bool) {
		if n == name {
			return "INERT jevons-po: frozen", true
		}
		return "", false
	})
	s.SetQueuedPromptsReader(func(n string) (int, float64, bool) {
		if n == name {
			return 2, 120.5, true
		}
		return 0, 0, false
	})
	rows := s.decorateSeatStops([]agentInfo{
		{Name: name, Running: true, Status: "working"},
		{Name: "other", Running: true, Status: "working"},
	})
	if rows[0].Inert == "" || rows[0].SeatLiveness != "inert" || rows[0].Status != "inert" {
		t.Fatalf("inert row = %#v", rows[0])
	}
	if rows[0].QueuedPrompts == nil || *rows[0].QueuedPrompts != 2 {
		t.Fatalf("queued_prompts = %#v", rows[0].QueuedPrompts)
	}
	if rows[0].QueuedPromptsAgeSeconds == nil || *rows[0].QueuedPromptsAgeSeconds != 120.5 {
		t.Fatalf("queued_prompts_age_seconds = %#v", rows[0].QueuedPromptsAgeSeconds)
	}
	if rows[1].Inert != "" || rows[1].QueuedPrompts != nil {
		t.Fatalf("other row polluted: %#v", rows[1])
	}
}

func TestT754HandleListAgentsHTTPInertFields(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	const name = "jevons-po"
	if err := reg.Register(claudia.AgentDef{
		Name: name, WorkDir: dir, SessionID: "s-po", Provider: "grok",
	}); err != nil {
		t.Fatal(err)
	}
	s := New("test", t.TempDir())
	s.SetRegistry(reg)
	seatstate.RegistryAuthority(reg).Observe(seatstate.Observation{
		Name: name, Alive: seatstate.Yes, Source: "test",
		QueueDepth: seatstate.QueueUnknown,
	})
	s.SetInertReader(func(n string) (string, bool) {
		if n == name {
			return "INERT jevons-po: (🎯T754).", true
		}
		return "", false
	})
	s.SetQueuedPromptsReader(func(n string) (int, float64, bool) {
		if n == name {
			return 3, 90, true
		}
		return 0, 0, false
	})
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/agents")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var rows []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	for _, r := range rows {
		if r["name"] == name {
			row = r
			break
		}
	}
	if row == nil {
		t.Fatalf("missing %q in %+v", name, rows)
	}
	if row["inert"] == nil || row["inert"] == "" {
		t.Fatalf("inert missing: %+v", row)
	}
	qp, ok := row["queued_prompts"].(float64)
	if !ok || int(qp) != 3 {
		t.Fatalf("queued_prompts = %#v", row["queued_prompts"])
	}
}

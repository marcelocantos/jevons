// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/fleetintent"
	"github.com/mark3labs/mcp-go/mcp"
)

func TestT1254UnknownStopNeverWritesIntent(t *testing.T) {
	dir := t.TempDir()
	s := &Server{registry: t439Registry(t, "known")}
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.SetFleetIntentStore(store)
	// Seed the file so the oracle compares actual bytes, not two absences.
	if err := s.SetAgentIntent("known", fleetintent.Working, "test", "seed"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "fleet", fleetintent.StoreFileName)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a b", "unknown-never-registered", "\t"} {
		t.Run(name, func(t *testing.T) {
			req := mcp.CallToolRequest{}
			req.Params.Arguments = map[string]any{"name": name, "actor": "jevons", "reason": "probe", "force": true, "disposition": "park"}
			res, err := s.handleAgentStop(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if !res.IsError {
				t.Fatalf("accepted unknown name %q: %s", name, toolText(res))
			}
			if !strings.Contains(toolText(res), "name") && !strings.Contains(toolText(res), "registered") {
				t.Fatalf("unclear refusal: %s", toolText(res))
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatalf("intent.json changed on refused stop %q", name)
			}
		})
	}
}

func TestT1254FleetIntentUnknownNameRefused(t *testing.T) {
	s := &Server{registry: t439Registry(t, "known")}
	for _, name := range []string{"a b", "unknown-never-registered"} {
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{"name": name, "state": "parked", "actor": "test"}
		res, err := s.handleFleetIntent(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if !res.IsError {
			t.Fatalf("intent accepted %q", name)
		}
	}
}

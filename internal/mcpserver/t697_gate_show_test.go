// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/gate"
)

// 🎯T697 MCP half: jevons_gate_show resolves a known record and returns
// distinguishable not-found for an unknown id. Scoped test — do not fold
// this into a whole-package mcpserver run while 🎯T712 is open.
func TestT697GateShowRoundTripAndNotFound(t *testing.T) {
	root := t.TempDir()
	t.Setenv(gate.StoreDirEnv, root)
	store, err := gate.OpenStore(root)
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	rec := &gate.Record{
		ID:          "a173ddfb",
		Name:        "make-test-go-clean",
		Command:     []string{"make", "test-go-clean"},
		ExitStatus:  0,
		StatusKnown: true,
		Verdict:     gate.VerdictGreen,
		Started:     time.Now().Add(-time.Second),
		Ended:       time.Now(),
		Tree: &gate.TreeProvenance{
			Commit: "8297ae6b1d9e4f2a",
			Clean:  true,
		},
	}
	if err := store.Save(rec); err != nil {
		t.Fatalf("Save: %v", err)
	}

	s := New(t.TempDir(), nil, nil)
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"id": rec.ID}
	res, err := s.handleGateShow(context.Background(), req)
	if err != nil {
		t.Fatalf("handleGateShow: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatalf("known id errored: %+v", res)
	}
	text := resultText(t, res)
	if !strings.Contains(text, rec.Attestation()) {
		t.Fatalf("missing attestation in %q", text)
	}
	// JSON view is in the payload so a supervisor can read command/status/tree
	// without parsing the summary prose.
	idx := strings.Index(text, "{")
	if idx < 0 {
		t.Fatalf("no JSON view in %q", text)
	}
	var view gate.View
	if err := json.Unmarshal([]byte(text[idx:]), &view); err != nil {
		t.Fatalf("decode view: %v\n%s", err, text[idx:])
	}
	if !view.Found || view.Status != "0" || view.Verdict != gate.VerdictGreen {
		t.Fatalf("view=%+v", view)
	}
	if len(view.Command) != 2 || view.Command[1] != "test-go-clean" {
		t.Fatalf("command=%#v", view.Command)
	}
	if view.Tree == nil || !view.Tree.Clean || view.Tree.Commit != rec.Tree.Commit {
		t.Fatalf("tree=%+v", view.Tree)
	}

	missReq := mcp.CallToolRequest{}
	missReq.Params.Arguments = map[string]any{"id": "01d9c909"}
	miss, err := s.handleGateShow(context.Background(), missReq)
	if err != nil {
		t.Fatalf("unknown id call: %v", err)
	}
	if miss == nil || !miss.IsError {
		t.Fatalf("unknown id must be an error, got %+v", miss)
	}
	missText := resultText(t, miss)
	if !strings.Contains(missText, gate.ErrNotFound) {
		t.Fatalf("unknown id error %q does not name not-found", missText)
	}
	if strings.Contains(missText, "GREEN") && strings.Contains(missText, "exit=0") {
		t.Fatalf("unknown id looks like a pass: %q", missText)
	}
}

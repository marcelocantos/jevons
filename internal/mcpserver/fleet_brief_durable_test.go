// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// The standing brief rides the first send of a seat session, not the first
// send of a daemon process: a restart re-injected ~43 KB into each seat's
// next send (2026-10-27). Driven through handleAgentSend on two servers that
// share a state dir, as a restart does.
func TestFleetBriefOncePerSessionAcrossDaemonRestart(t *testing.T) {
	stateDir := t.TempDir()
	reg := newLineageRegistry(t, map[string]string{"jevons-po": "jevons", "jv-t10": "jevons-po"})
	send := func() string {
		po := &fakeSender{alive: true}
		s, _ := chainServer(t, map[string]*fakeSender{"jevons-po": po})
		s.registry = reg
		s.stateDir = stateDir
		req := mcp.CallToolRequest{}
		req.Params.Arguments = map[string]any{"name": "jevons-po", "text": "status?", "actor": "jevons"}
		res, err := s.handleAgentSend(context.Background(), req)
		if err != nil || res.IsError {
			t.Fatalf("send: err=%v res=%s", err, toolText(res))
		}
		if len(po.sent) != 1 {
			t.Fatalf("po got %d sends", len(po.sent))
		}
		return po.sent[0]
	}
	if first := send(); !strings.Contains(first, FleetStandingBrief[:40]) {
		t.Fatalf("first send of the session should carry the standing brief:\n%.300s", first)
	}
	if again := send(); strings.Contains(again, FleetStandingBrief[:40]) {
		t.Fatalf("restart re-injected the standing brief into the same session:\n%.300s", again)
	}

	d := reg.Def("jevons-po")
	d.SessionID = "fresh-session"
	if err := reg.Register(*d); err != nil {
		t.Fatal(err)
	}
	if fresh := send(); !strings.Contains(fresh, FleetStandingBrief[:40]) {
		t.Fatalf("a new session must be briefed again:\n%.300s", fresh)
	}
}

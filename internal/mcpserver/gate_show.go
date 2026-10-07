// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/gate"
)

// jevons_gate_show is the supervisor read-back for a cited GATE id (🎯T697).
// bin/gate show is only in-band for a session that holds a shell in the repo;
// this tool is the same lookup through the daemon, with no filesystem access
// to ~/.jevons/gates and no permission to execute bin/gate.
func (s *Server) registerGateShowTool() {
	s.addTool(
		mcp.NewTool("jevons_gate_show",
			mcp.WithDescription("Resolve a cited GATE id to the recorded command, exit status, verdict and tree state (🎯T697). Supervisors use this instead of bin/gate show or reading ~/.jevons/gates. An unknown id returns a distinguishable not-found, never a pass."),
			mcp.WithString("id", mcp.Required(), mcp.Description("Gate record id from a GATE … id=<id> attestation line")),
			mcp.WithString("workdir", mcp.Description("Optional: the citing agent's own workdir. When given, the result says whether the record's measured commit is a commit of that repository (scope: own | FOREIGN | unknown) — a gate from another repo is not evidence for work here (🎯T1027).")),
		),
		s.handleGateShow,
	)
}

func (s *Server) handleGateShow(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	id := strings.TrimSpace(str(req.GetArguments()["id"]))
	if !gate.ValidRecordID(id) {
		return mcp.NewToolResultError(fmt.Sprintf("%s: no gate record for id=%q", gate.ErrNotFound, id)), nil
	}
	store := gateStore()
	if store == nil {
		return mcp.NewToolResultError("gate store unavailable"), nil
	}
	rec, ok := store.Lookup(id)
	if !ok || rec == nil {
		return mcp.NewToolResultError(fmt.Sprintf("%s: no gate record for id=%q", gate.ErrNotFound, id)), nil
	}
	view := gate.ViewOf(rec)
	blob, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	text := rec.Summary()
	// 🎯T1027: a caller that names its own repo gets the scope question
	// answered here, before it cites — the same check the daemon runs on the
	// finish report, so a worker can catch a foreign id itself.
	if workDir := strings.TrimSpace(str(req.GetArguments()["workdir"])); workDir != "" {
		scope := gate.ScopeOf(rec, workDir)
		text += "\n  " + scope.Describe()
		if sb, err := json.Marshal(scope); err == nil {
			blob = append(append(blob, '\n'), []byte(`{"scope": `+string(sb)+`}`)...)
		}
	}
	return mcp.NewToolResultText(text + "\n" + string(blob)), nil
}

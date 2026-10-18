// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/seatpair"
)

// ExecuteSeatPair completes one per-seat tool intent. seatpair.ping answers
// from the effect log. A tool this server registered runs its handler once.
// Any other tool stays uncertain and is not replayed. This is not registered
// as an owner-facing MCP tool.
func (s *Server) ExecuteSeatPair(ctx context.Context, in seatpair.Intent) seatpair.Result {
	if s == nil {
		return seatpair.Result{Outcome: seatpair.OutcomeUncertain, Reason: "no effect"}
	}
	if in.Tool == seatpair.ToolPing {
		n, err := seatpair.NoteEffect(s.workerWD, in.OperationID)
		if err != nil {
			return seatpair.Result{Outcome: seatpair.OutcomeUncertain, Reason: err.Error()}
		}
		return seatpair.Result{Outcome: seatpair.OutcomeAccepted, Reason: "pong", Body: strconv.FormatInt(n, 10)}
	}
	h := s.seatPairHandler(in.Tool)
	if h == nil {
		return seatpair.Result{Outcome: seatpair.OutcomeUncertain, Reason: "unknown tool"}
	}
	var args map[string]any
	if strings.TrimSpace(in.ArgsJSON) != "" && in.ArgsJSON != "{}" {
		if err := json.Unmarshal([]byte(in.ArgsJSON), &args); err != nil {
			return seatpair.Result{Outcome: seatpair.OutcomeUncertain, Reason: err.Error()}
		}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	out, err := h(ctx, mcp.CallToolRequest{Params: mcp.CallToolParams{Name: in.Tool, Arguments: args}})
	if err != nil {
		return seatpair.Result{Outcome: seatpair.OutcomeUncertain, Reason: err.Error()}
	}
	reason := "ok"
	body := ""
	if out != nil {
		if out.IsError {
			reason = "error"
		}
		body = toolResultText(out)
	}
	return seatpair.Result{Outcome: seatpair.OutcomeAccepted, Reason: reason, Body: body}
}

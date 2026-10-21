// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/seatpair"
)

func (s *Server) rememberSeatPairHandler(name string, h func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
	if s == nil || h == nil || strings.TrimSpace(name) == "" {
		return
	}
	s.seatPairMu.Lock()
	defer s.seatPairMu.Unlock()
	if s.seatPairHandlers == nil {
		s.seatPairHandlers = map[string]func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error){}
	}
	s.seatPairHandlers[name] = h
}

func (s *Server) seatPairHandler(name string) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if s == nil {
		return nil
	}
	s.seatPairMu.Lock()
	defer s.seatPairMu.Unlock()
	return s.seatPairHandlers[name]
}

// seatPairCall reports the seat and pair root when this request is a
// daemon-dependent call from a known seat. A call with no seat header, an
// unknown seat, or no state dir stays on the direct handler.
func (s *Server) seatPairCall(ctx context.Context) (seat, root string) {
	if s == nil {
		return "", ""
	}
	seat = strings.TrimSpace(mcpOriginFrom(ctx).Seat)
	if seat == "" || !s.isKnownSeat(seat) {
		return "", ""
	}
	s.mu.Lock()
	dir := s.stateDir
	s.mu.Unlock()
	if strings.TrimSpace(dir) == "" {
		return "", ""
	}
	return seat, filepath.Join(dir, seatpair.DirName)
}

// answerThroughSeatPair commits the intent, then the existing executor
// completes that operation. The handler runs inside the executor, once.
func (s *Server) answerThroughSeatPair(ctx context.Context, root, seat, name string, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if err := seatpair.PublishGeneration(root, seat, seatpair.InitialGeneration); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	gen, ok, err := seatpair.CurrentGeneration(root, seat)
	if err != nil || !ok || gen <= 0 {
		return mcp.NewToolResultError("seat pair: generation unavailable"), nil
	}
	caller, err := seatpair.OpenCaller(root, seat)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	body := "{}"
	if args := req.GetArguments(); len(args) > 0 {
		raw, err := json.Marshal(args)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		body = string(raw)
	}
	id, err := caller.Commit(ctx, &seatpair.CommitArgs{Generation: gen, Tool: name, ArgsJSON: body})
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := seatpair.CatchUpDir(ctx, root, s.ExecuteSeatPair); err != nil {
		if res, waitErr := caller.Wait(ctx, id); waitErr == nil {
			return seatPairToolResult(res), nil
		}
		return mcp.NewToolResultError(err.Error()), nil
	}
	res, err := caller.Wait(ctx, id)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return seatPairToolResult(res), nil
}

func seatPairToolResult(res seatpair.Result) *mcp.CallToolResult {
	if res.Outcome == seatpair.OutcomeAccepted && res.Reason != "error" {
		return mcp.NewToolResultText(res.Body)
	}
	if res.Outcome == seatpair.OutcomeAccepted {
		return mcp.NewToolResultError(res.Body)
	}
	msg := res.Outcome
	if res.Reason != "" {
		msg += ": " + res.Reason
	}
	if res.Body != "" {
		msg += ": " + res.Body
	}
	return mcp.NewToolResultError(msg)
}

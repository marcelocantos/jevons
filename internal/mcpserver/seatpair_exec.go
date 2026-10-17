// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"

	"github.com/marcelocantos/jevons/internal/seatpair"
)

// ExecuteSeatPair completes one per-seat tool intent caught up at startup.
// seatpair.ping answers. Any other tool stays uncertain and is not replayed.
// This is not registered as an owner-facing MCP tool.
func (s *Server) ExecuteSeatPair(_ context.Context, in seatpair.Intent) seatpair.Result {
	if s == nil {
		return seatpair.Result{Outcome: seatpair.OutcomeUncertain, Reason: "no effect"}
	}
	if in.Tool == seatpair.ToolPing {
		return seatpair.Result{Outcome: seatpair.OutcomeAccepted, Reason: "pong"}
	}
	return seatpair.Result{Outcome: seatpair.OutcomeUncertain, Reason: "unknown tool"}
}

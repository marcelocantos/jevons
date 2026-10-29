// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/landhold"
)

// SetLandHoldStore installs the durable land-hold ledger (nil disables).
func (s *Server) SetLandHoldStore(st *landhold.Store) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.landHold = st
}

// OpenLandHold opens <stateDir>/fleet/land-hold.json.
func (s *Server) OpenLandHold(stateDir string) error {
	st, err := landhold.Open(stateDir)
	if err != nil {
		return err
	}
	s.SetLandHoldStore(st)
	return nil
}

func (s *Server) landHoldStore() *landhold.Store {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.landHold
}

func (s *Server) recordLandHoldFromBrief(seat, text, actor string) {
	st := s.landHoldStore()
	if st == nil {
		return
	}
	m := parseIncomingEnvelope(text)
	if m == nil || !m.LandHold.Active() {
		return
	}
	if strings.TrimSpace(actor) == "" {
		actor = "daemon"
	}
	_ = st.SetAgent(seat, landhold.FromEnvelope(m.LandHold), strings.TrimSpace(actor), "spawn-brief hold slot", time.Now())
}

func (s *Server) clearLandHoldSeat(seat string) {
	st := s.landHoldStore()
	if st == nil {
		return
	}
	_ = st.ClearAgent(seat)
}

func (s *Server) handleLandHold(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	st := s.landHoldStore()
	if st == nil {
		return mcp.NewToolResultError("land-hold store not configured"), nil
	}
	args := req.GetArguments()
	actor, _ := args["actor"].(string)
	actor = strings.TrimSpace(actor)
	if actor == "" {
		actor = s.intentActor(ctx, args)
	}
	reason, _ := args["reason"].(string)
	scope, _ := args["scope"].(string)
	scope = strings.TrimSpace(strings.ToLower(scope))
	holdRaw, _ := args["hold"].(string)
	if strings.TrimSpace(holdRaw) == "" {
		holdRaw = "no-merge,no-activation"
	}
	active, haveActive := args["active"].(bool)
	now := time.Now()
	// A call without active reads. Treating the missing bool as false made a
	// read-shaped call clear the fleet hold (2026-10-27).
	if !haveActive {
		seat := ""
		if scope == "agent" {
			seat, _ = args["name"].(string)
			seat = strings.TrimSpace(seat)
		}
		h := st.EffectiveHold(seat)
		if !h.Active() {
			return mcp.NewToolResultText("no land-hold in effect"), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("land-hold in effect: %s (pass active=false to clear)", landhold.FormatHold(h))), nil
	}

	switch scope {
	case "fleet":
		if !active {
			if err := st.SetFleet(landhold.Hold{}, actor, reason, now); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText("fleet land-hold cleared"), nil
		}
		h, err := landhold.ParseHold(holdRaw)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if err := st.SetFleet(h, actor, reason, now); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("fleet land-hold active: %s", landhold.FormatHold(h))), nil
	case "agent":
		name, _ := args["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			return mcp.NewToolResultError("name required for scope=agent"), nil
		}
		if err := s.singleSeatName(name); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if !active {
			if err := st.ClearAgent(name); err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultText(fmt.Sprintf("land-hold cleared for %q", name)), nil
		}
		h, err := landhold.ParseHold(holdRaw)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if err := st.SetAgent(name, h, actor, reason, now); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(fmt.Sprintf("land-hold set for %q: %s", name, landhold.FormatHold(h))), nil
	default:
		return mcp.NewToolResultError("scope must be fleet or agent"), nil
	}
}

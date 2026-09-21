// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/spawnorder"
)

// 🎯T762 — a partly-executed spawn order shows which half it dropped.
//
// An order to a PO is a message; the seats it names become jevons_agent_start
// calls only if the PO makes them. When the PO drops half an order, the
// daemon never sees those starts, so nothing it journals can name them. The
// issuer therefore declares the order (jevons_spawn_order action=declare),
// and every named seat is reconciled against the journalled start attempts
// and the registry: minted, rerouted, refused (with the start error) or
// not_attempted. The ordering PO's /api/agents row carries one line per open
// order, and an incomplete one names the seats that never appeared.

func (s *Server) registerSpawnOrderTools() {
	s.addTool(
		mcp.NewTool("jevons_spawn_order",
			mcp.WithDescription("Declare a spawn order's named seats, then read per seat whether it was minted (🎯T762). action=declare records an order given to parent naming seats; action=status reconciles every seat against journalled jevons_agent_start attempts and the registry — minted / rerouted (other provider) / refused (start error) / not_attempted (no start was ever made: the dropped half); action=close retires an order from the panel. Open orders decorate the parent's /api/agents row as spawn_orders."),
			mcp.WithString("action", mcp.Required(), mcp.Description("declare | status | close")),
			mcp.WithString("parent", mcp.Description("declare: the agent the order was given to (e.g. jevons-po). status: filter to this parent.")),
			mcp.WithString("seats", mcp.Description("declare: named seats as name:provider[:target], comma- or newline-separated, e.g. \"jv-t759-x:grok:T759, jv-t749-y:claude:T749\"")),
			mcp.WithString("id", mcp.Description("Order id (declare: optional, minted from the time; status/close: which order)")),
			mcp.WithString("actor", mcp.Description("Who issued the order")),
			mcp.WithString("note", mcp.Description("declare: free text; close: why it is closed")),
		),
		s.handleSpawnOrder,
	)
}

// spawnOrderStore opens the store under the daemon state dir.
func (s *Server) spawnOrderStore() (*spawnorder.Store, error) {
	if s == nil || strings.TrimSpace(s.stateDir) == "" {
		return nil, fmt.Errorf("spawn orders need a state dir")
	}
	return spawnorder.Open(spawnorder.DefaultPath(s.stateDir))
}

// reconcileSpawnOrders reads the journal's start attempts and the registry
// once and reconciles every order against them.
func (s *Server) reconcileSpawnOrders(orders []spawnorder.Order) []spawnorder.Result {
	attempts := s.spawnOrderStartAttempts(time.Now())
	present := map[string]string{}
	if s.registry != nil {
		for _, def := range s.registry.List() {
			prov := string(def.Provider)
			if prov == "" {
				prov = "claude"
			}
			present[def.Name] = prov
		}
	}
	out := make([]spawnorder.Result, 0, len(orders))
	for _, o := range orders {
		out = append(out, spawnorder.Reconcile(o, attempts, present))
	}
	return out
}

// spawnOrderAttemptsTTL bounds how stale the cached start attempts may be.
// The journal is a full decode of a file that grows all day, and /api/agents
// asks once per row with an open order.
const spawnOrderAttemptsTTL = 30 * time.Second

// spawnOrderStartAttempts returns the journalled start attempts, rescanning
// the journal at most once per spawnOrderAttemptsTTL.
func (s *Server) spawnOrderStartAttempts(now time.Time) []spawnorder.Attempt {
	if s.eventLogTail == nil {
		return nil
	}
	s.spawnOrderMu.Lock()
	defer s.spawnOrderMu.Unlock()
	if !s.spawnOrderReadAt.IsZero() && now.Sub(s.spawnOrderReadAt) < spawnOrderAttemptsTTL {
		return s.spawnOrderAttempts
	}
	events, _, err := s.eventLogTail(eventlog.TailOptions{Limit: 2000, Component: compAgentLifecycle, Decision: "start"})
	if err != nil {
		return s.spawnOrderAttempts
	}
	s.spawnOrderAttempts = spawnorder.AttemptsFromEvents(events)
	s.spawnOrderReadAt = now
	return s.spawnOrderAttempts
}

// SpawnOrderLines is the /api/agents decoration for one parent: a line per
// open (not closed) order given to it, incomplete orders naming their missing
// seats. Empty when the parent has no open orders or no store exists.
func (s *Server) SpawnOrderLines(parent string) []string {
	store, err := s.spawnOrderStore()
	if err != nil {
		return nil
	}
	orders, err := store.Orders()
	if err != nil {
		return []string{"spawn orders unreadable: " + err.Error()}
	}
	var open []spawnorder.Order
	for _, o := range orders {
		if o.Parent == parent && !o.Closed {
			open = append(open, o)
		}
	}
	if len(open) == 0 {
		return nil
	}
	var lines []string
	for _, r := range s.reconcileSpawnOrders(open) {
		lines = append(lines, r.Line())
	}
	return lines
}

func (s *Server) handleSpawnOrder(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	action := strings.ToLower(strings.TrimSpace(str(args["action"])))
	parent := strings.TrimSpace(str(args["parent"]))
	id := strings.TrimSpace(str(args["id"]))
	actor := strings.TrimSpace(str(args["actor"]))
	note := strings.TrimSpace(str(args["note"]))
	store, err := s.spawnOrderStore()
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	now := time.Now()
	switch action {
	case "declare":
		seats, err := spawnorder.ParseSeats(str(args["seats"]))
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		o, err := store.Declare(spawnorder.Order{ID: id, Parent: parent, By: actor, Note: note, Seats: seats}, now)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		names := make([]string, 0, len(o.Seats))
		for _, seat := range o.Seats {
			names = append(names, seat.Name+":"+seat.Provider)
		}
		s.LogEvent("spawn_order", "declare", map[string]any{
			"id": o.ID, "parent": o.Parent, "by": o.By, "seats": strings.Join(names, ","),
		})
		return mcp.NewToolResultText(fmt.Sprintf("declared %s to %s naming %d seat(s): %s. Read it back with action=status id=%s.",
			o.ID, o.Parent, len(o.Seats), strings.Join(names, ", "), o.ID)), nil
	case "close":
		if id == "" {
			return mcp.NewToolResultError("close needs id"), nil
		}
		if err := store.Close(id, note, now); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		s.LogEvent("spawn_order", "close", map[string]any{"id": id, "by": actor, "note": note})
		return mcp.NewToolResultText("closed " + id), nil
	case "status", "":
		orders, err := store.Orders()
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		var pick []spawnorder.Order
		for _, o := range orders {
			if (id == "" || o.ID == id) && (parent == "" || o.Parent == parent) {
				pick = append(pick, o)
			}
		}
		if id != "" && len(pick) == 0 {
			return mcp.NewToolResultError(fmt.Sprintf("no order %q", id)), nil
		}
		results := s.reconcileSpawnOrders(pick)
		var b strings.Builder
		for _, r := range results {
			b.WriteString(r.Line())
			b.WriteString("\n")
		}
		blob, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		b.Write(blob)
		return mcp.NewToolResultText(b.String()), nil
	default:
		return mcp.NewToolResultError(fmt.Sprintf("action %q: use declare, status or close", action)), nil
	}
}

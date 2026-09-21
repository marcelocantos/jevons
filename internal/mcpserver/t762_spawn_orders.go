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
			mcp.WithDescription("Declare a spawn order's named seats, then read per seat whether it was minted (🎯T762). action=declare records an order given to parent naming seats; action=status reconciles every seat against journalled jevons_agent_start attempts; only a start that passed order_id=<this order's id> is attributed — minted / rerouted (other provider) / refused (start error) / not_attempted (no matching daemon start observed in the journal window: the dropped half, as far as the daemon can see) / unknown (a same-name start without the order id, an unreadable or too-short journal, or only an older same-name incarnation); action=close retires an order from the panel. Open orders decorate the parent's /api/agents row as spawn_orders."),
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

// reconcileSpawnOrders reconciles every order against the journal and the
// registry, then writes the observed outcomes back so they outlive the
// journal tail (🎯T762 review finding 3).
func (s *Server) reconcileSpawnOrders(store *spawnorder.Store, orders []spawnorder.Order) ([]spawnorder.Result, error) {
	ev := s.spawnOrderJournalEvidence(time.Now())
	ev.Present = map[string]spawnorder.Incarnation{}
	if s.registry != nil {
		for _, def := range s.registry.List() {
			prov := string(def.Provider)
			if prov == "" {
				prov = "claude"
			}
			ev.Present[def.Name] = spawnorder.Incarnation{Provider: prov, Target: def.TargetID, Session: def.SessionID}
		}
	}
	out := make([]spawnorder.Result, 0, len(orders))
	for _, o := range orders {
		out = append(out, spawnorder.Reconcile(o, ev))
	}
	if err := store.Record(out); err != nil {
		return out, fmt.Errorf("spawn orders: record observed outcomes: %w", err)
	}
	return out, nil
}

// spawnOrderJournalLimit is how many start events one journal read keeps.
const spawnOrderJournalLimit = 2000

// spawnOrderAttemptsTTL bounds how stale the cached journal evidence may be.
// The journal is a full decode of a file that grows all day, and /api/agents
// asks once per row with an open order.
const spawnOrderAttemptsTTL = 30 * time.Second

// spawnOrderJournalEvidence returns the journalled start attempts, rescanning
// the journal at most once per spawnOrderAttemptsTTL. A failed read is
// carried as ReadErr, and a read that hit the limit carries the instant it
// reaches back to, so reconciliation reports unknown instead of reading
// missing evidence as "never attempted".
func (s *Server) spawnOrderJournalEvidence(now time.Time) spawnorder.Evidence {
	if s.eventLogTail == nil {
		return spawnorder.Evidence{ReadErr: "no event journal is wired to this server"}
	}
	s.spawnOrderMu.Lock()
	defer s.spawnOrderMu.Unlock()
	if !s.spawnOrderReadAt.IsZero() && now.Sub(s.spawnOrderReadAt) < spawnOrderAttemptsTTL {
		return s.spawnOrderEvidence
	}
	var ev spawnorder.Evidence
	// Two reads: the daemon's own start events (unattributed) and the
	// order-correlated ones the start observer journals. Coverage is the
	// later of the two, since either falling short hides evidence.
	for _, kind := range []eventlog.TailOptions{
		{Limit: spawnOrderJournalLimit, Component: compAgentLifecycle, Decision: "start"},
		{Limit: spawnOrderJournalLimit, Component: spawnorder.JournalComponent, Decision: spawnorder.JournalStart},
	} {
		events, _, err := s.eventLogTail(kind)
		if err != nil {
			ev.ReadErr = err.Error()
			ev.Attempts = nil
			break
		}
		ev.Attempts = append(ev.Attempts, spawnorder.AttemptsFromEvents(events)...)
		if len(events) >= spawnOrderJournalLimit {
			var oldest time.Time
			for _, e := range events {
				at, perr := time.Parse(time.RFC3339Nano, e.TS)
				if perr == nil && (oldest.IsZero() || at.Before(oldest)) {
					oldest = at
				}
			}
			if oldest.After(ev.CoveredSince) {
				ev.CoveredSince = oldest
			}
		}
	}
	s.spawnOrderEvidence = ev
	s.spawnOrderReadAt = now
	return ev
}

// observeSpawnOrderStart wraps jevons_agent_start: when the caller passes
// order_id (the id jevons_spawn_order declare returned), the outcome is
// journalled as spawn_order.start carrying that id. This is the only start
// evidence reconciliation attributes to an order; a start without it can at
// best read unknown. It lives here rather than in the start handler because
// that file is held for the owner.
func (s *Server) observeSpawnOrderStart(h func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error)) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := req.GetArguments()
		orderID := strings.TrimSpace(str(args["order_id"]))
		res, err := h(ctx, req)
		if orderID == "" {
			return res, err
		}
		name := strings.TrimSpace(str(args["name"]))
		fields := map[string]any{
			"order_id":  orderID,
			"name":      name,
			"target_id": normalizeAgentTargetID(str(args["target_id"])),
			"provider":  strings.TrimSpace(str(args["provider"])),
		}
		switch {
		case err != nil:
			fields["outcome"], fields["err"] = "error", err.Error()
		case res == nil:
			fields["outcome"], fields["err"] = "error", "start returned no result"
		case res.IsError:
			fields["outcome"], fields["err"] = "error", toolResultText(res)
		default:
			fields["outcome"] = "ok"
			if s.registry != nil {
				if def := s.registry.Def(name); def != nil {
					if def.Provider != "" {
						fields["provider"] = string(def.Provider)
					}
					fields["session_id"] = def.SessionID
				}
			}
		}
		s.LogEvent(spawnorder.JournalComponent, spawnorder.JournalStart, fields)
		s.spawnOrderMu.Lock()
		s.spawnOrderReadAt = time.Time{} // the next read must see this start
		s.spawnOrderMu.Unlock()
		return res, err
	}
}

// toolResultText is a result's text content, first 500 bytes.
func toolResultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	out := strings.TrimSpace(b.String())
	if len(out) > 500 {
		out = out[:500]
	}
	return out
}

// SpawnOrderLines is the /api/agents decoration for one parent: a line per
// open (not closed) order given to it, incomplete orders naming their missing
// seats. An unreadable or malformed store is an error, never "no orders".
func (s *Server) SpawnOrderLines(parent string) ([]string, error) {
	store, err := s.spawnOrderStore()
	if err != nil {
		return nil, err
	}
	orders, err := store.Orders()
	if err != nil {
		return nil, err
	}
	var open []spawnorder.Order
	for _, o := range orders {
		if o.Parent == parent && !o.Closed {
			open = append(open, o)
		}
	}
	if len(open) == 0 {
		return nil, nil
	}
	results, err := s.reconcileSpawnOrders(store, open)
	var lines []string
	for _, r := range results {
		lines = append(lines, r.Line())
	}
	return lines, err
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
		return mcp.NewToolResultText(fmt.Sprintf("declared %s to %s naming %d seat(s): %s. Each jevons_agent_start for these seats must pass order_id=%s — only a start carrying it is attributed to the order; one without it reads unknown. Read it back with action=status id=%s.",
			o.ID, o.Parent, len(o.Seats), strings.Join(names, ", "), o.ID, o.ID)), nil
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
		results, err := s.reconcileSpawnOrders(store, pick)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
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

// spawnOrderStartTool exposes the observer's correlation argument on the wire.
func spawnOrderStartTool(t mcp.Tool) mcp.Tool {
	if t.InputSchema.Properties == nil {
		t.InputSchema.Properties = map[string]any{}
	}
	t.InputSchema.Properties["order_id"] = map[string]any{
		"type":        "string",
		"description": "Spawn order id returned by jevons_spawn_order declare; attributes this start outcome to that order.",
	}
	return t
}

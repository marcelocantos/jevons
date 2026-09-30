// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// 🎯T969: a fleet-intent change records who actually made it.
//
// THE INCIDENT, 2026-09-30. The owner paused the fleet. Later the owner's own
// Claude Code session set the intent back to working through this endpoint
// without passing actor, and the handler filled the blank with the overseer's
// name. jevons-po read "by jevons", concluded the overseer had wrongly lifted
// the owner's pause, re-paused the fleet, and stopped five workers with stop
// reasons confessing to an act nobody in the fleet had taken. A default that
// names a particular agent is a fabricated record, and agents act on records.
//
// So an unnamed caller is recorded as what this request can actually prove:
//
//  1. an explicit actor argument, verbatim;
//  2. a registered seat named by the request itself (the SeatHeader);
//  3. the MCP client — its initialize clientInfo name when the same
//     connection sent it with the same User-Agent, else the User-Agent's
//     product token — as "client:<name>", never shaped like a seat name;
//  4. "unattributed".
//
// It never falls back to the overseer's name, and it deliberately does NOT
// guess the seat from "the only agent with a turn in flight" (the heuristic
// mcpCallerOf uses to route cancellation). That guess is right whenever a seat
// calls, and wrong exactly when an outside session calls while one seat
// happens to be mid-turn — which would have recorded the owner's un-pause as
// jevons-po's, the same confession with a different name. Attribution that is
// sometimes silently wrong is worse than attribution that says it is unknown.

// SeatHeader names the calling fleet seat on an MCP HTTP request. A request
// carrying it with a registered name is attributed to that seat. The daemon
// does not yet stamp it onto AgentDef.MCPServers (that changes every seat's
// MCP list and so re-stamps every definition); until it does, seats identify
// themselves with the actor argument.
const SeatHeader = "X-Jevons-Agent"

// UnattributedActor is recorded when nothing about the request names a caller.
const UnattributedActor = "unattributed"

// clientActorPrefix marks an actor identified only as an MCP client program.
// It cannot be mistaken for a seat name: no agent name carries a colon.
const clientActorPrefix = "client:"

// planPolicyActor is the product path that parks seats on plan exhaustion. It
// is the daemon, not the overseer agent, and says so.
const planPolicyActor = "product:plan_policy"

// mcpOrigin is what the HTTP layer knows about the caller of one MCP request.
type mcpOrigin struct {
	Seat   string // SeatHeader value, unverified
	Client string // MCP client program name, or ""
}

type mcpOriginKey struct{}

func withMCPOrigin(ctx context.Context, o mcpOrigin) context.Context {
	return context.WithValue(ctx, mcpOriginKey{}, o)
}

func mcpOriginFrom(ctx context.Context) mcpOrigin {
	if ctx == nil {
		return mcpOrigin{}
	}
	o, _ := ctx.Value(mcpOriginKey{}).(mcpOrigin)
	return o
}

// mcpClientLedgerCap bounds the per-connection clientInfo memory. Connections
// are few (one per MCP client process); the cap only stops a leak.
const mcpClientLedgerCap = 512

// mcpClientLedgerTTL is how long an initialize is trusted for its connection.
const mcpClientLedgerTTL = 6 * time.Hour

// mcpClientLedger remembers the clientInfo each connection sent at initialize.
// The transport is stateless (no Mcp-Session-Id), so the connection's remote
// address is the only thing that ties a later tools/call to that initialize.
// An entry is used only when the User-Agent still matches, so a port reused by
// a different program is not attributed to the previous one.
type mcpClientLedger struct {
	mu      sync.Mutex
	entries map[string]mcpClientEntry
}

type mcpClientEntry struct {
	name string
	ua   string
	at   time.Time
}

func (l *mcpClientLedger) remember(remote, ua, name string, now time.Time) {
	if remote == "" || name == "" {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.entries == nil {
		l.entries = map[string]mcpClientEntry{}
	}
	if len(l.entries) >= mcpClientLedgerCap {
		for k, e := range l.entries {
			if now.Sub(e.at) > mcpClientLedgerTTL {
				delete(l.entries, k)
			}
		}
		for k := range l.entries {
			if len(l.entries) < mcpClientLedgerCap {
				break
			}
			delete(l.entries, k)
		}
	}
	l.entries[remote] = mcpClientEntry{name: name, ua: ua, at: now}
}

func (l *mcpClientLedger) lookup(remote, ua string, now time.Time) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	e, ok := l.entries[remote]
	if !ok || e.ua != ua || now.Sub(e.at) > mcpClientLedgerTTL {
		return ""
	}
	return e.name
}

// originOf reads the caller identity off one HTTP request. body is the
// already-buffered JSON-RPC payload; an initialize is remembered for its
// connection as a side effect.
func (s *Server) originOf(r *http.Request, method string, body []byte) mcpOrigin {
	ua := r.UserAgent()
	now := time.Now()
	if method == "initialize" {
		var m struct {
			Params struct {
				ClientInfo struct {
					Name string `json:"name"`
				} `json:"clientInfo"`
			} `json:"params"`
		}
		if json.Unmarshal(body, &m) == nil {
			s.mcpClients.remember(r.RemoteAddr, ua, clientToken(m.Params.ClientInfo.Name), now)
		}
	}
	client := s.mcpClients.lookup(r.RemoteAddr, ua, now)
	if client == "" {
		client = userAgentProduct(ua)
	}
	return mcpOrigin{
		Seat:   strings.TrimSpace(r.Header.Get(SeatHeader)),
		Client: client,
	}
}

// userAgentProduct is the first product token of a User-Agent:
// "claude-code/2.0.14 (cli)" → "claude-code", "curl/8.7.1" → "curl".
func userAgentProduct(ua string) string {
	tok, _, _ := strings.Cut(strings.TrimSpace(ua), " ")
	tok, _, _ = strings.Cut(tok, "/")
	return clientToken(tok)
}

// clientToken normalises a client name to one lowercase token with no spaces
// or colons, so "client:<token>" stays one unambiguous field.
func clientToken(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', ':':
			return '-'
		}
		return r
	}, name)
	return name
}

// intentActor is who a fleet-intent change is recorded against. See the file
// comment for the order and for why it never guesses.
func (s *Server) intentActor(ctx context.Context, args map[string]any) string {
	if v, ok := args["actor"].(string); ok {
		if a := strings.TrimSpace(v); a != "" {
			return a
		}
	}
	o := mcpOriginFrom(ctx)
	if o.Seat != "" && s.isKnownSeat(o.Seat) {
		return o.Seat
	}
	if o.Client != "" {
		return clientActorPrefix + o.Client
	}
	return UnattributedActor
}

// isKnownSeat reports whether name is a registered seat or the overseer.
func (s *Server) isKnownSeat(name string) bool {
	if s == nil {
		return false
	}
	if name == s.overseerName() {
		return true
	}
	return s.registry != nil && s.registry.Def(name) != nil
}

// actorWasInferred reports whether the caller passed no actor, so the tool
// result can tell it what was recorded in its place.
func actorWasInferred(args map[string]any) bool {
	v, _ := args["actor"].(string)
	return strings.TrimSpace(v) == ""
}

// inferredActorNote tells a caller that omitted actor what the record says.
func inferredActorNote(actor string) string {
	return " No actor was passed, so this change is recorded as " + actor +
		" (🎯T969) — pass actor=<your agent name, or owner> so the record says who decided."
}

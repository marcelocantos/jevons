// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"strings"
)

// 🎯T309.3: ONE deliver-by-name path for every agent in the fleet layer.
//
// deliverByName is the single implementation every message-to-an-agent
// eventually reaches, whichever door the caller came through:
//
//	MCP    jevons_agent_send            → sendToAgent → deliverByName
//	HTTP   POST /api/agents/{name}/send → DeliverAgentMessageAs → deliverByName
//	fleet  worker reply notify, worker-idle, daemon-restarted, fleet health
//	                                     → notify/emit* → deliverByName
//
// Before this slice the fleet layer had a privileged overseer-only wire:
// everything addressed to the overseer went through notifyJevon (a bare
// func(text) injection set from main), while everything addressed to a
// worker went through sendToAgent. Two consequences, both bugs:
//
//   - jevons_agent_send addressed to the overseer resolved through the
//     registry and called proc.Send directly, bypassing the owner chat
//     journal, the owner-visible broadcast, and the queue-on-busy retry.
//     A PO reporting up by name could land in the overseer's session with
//     no owner-visible record — or hit "prompt already in flight" and be
//     dropped, the 🎯T62 failure this daemon has fixed once already.
//   - Which peers an agent could reach depended on which API it could
//     call, not on the fleet hierarchy. Hierarchy is lineage, not API
//     surface: the same call by name serves worker→PO, PO→overseer, and
//     owner→any-agent.
//
// The overseer is now simply the arm of deliverByName that resolves to the
// owner-chat delivery seam (SetOverseerDeliver → server.DeliverToOverseerAs),
// which owns journalling, owner bubbles, and the notify queue. No caller in
// this package sends to the overseer any other way.
//
// SILENT DROPS ARE ERRORS (acceptance 3). Every arm returns an error the
// caller can surface: an unregistered peer, an unwired overseer seam, and a
// failed overseer delivery all come back as errors; a busy peer comes back
// as a "queued" result, never a discarded message.

// SendOrigin marks who is speaking on a deliver-by-name call. It changes
// framing only at the overseer arm (🎯T309.2): an owner turn carries the
// owner marker and paints an owner bubble, an agent turn is an injected
// notification. Fleet agents receive the text either way — their provider
// session has no owner/agent distinction to make.
type SendOrigin string

const (
	// OriginOwner is the owner speaking (owner bubble + owner marker).
	OriginOwner SendOrigin = "owner"
	// OriginAgent is an agent or system notification (no owner bubble).
	OriginAgent SendOrigin = "agent"
)

// OverseerDeliverFunc delivers text to the overseer with origin semantics.
// Wired from main to server.DeliverToOverseerAs so the overseer arm of
// deliverByName reuses the owner-chat journal + notify queue rather than
// re-implementing them.
type OverseerDeliverFunc func(text string, origin SendOrigin) error

// SetOverseerDeliver wires the overseer arm of the single deliver path.
// Without it, an overseer-addressed send falls back to the legacy notify
// injection for agent-origin text and is a loud error for owner-origin text
// (never a silent misdelivery).
func (s *Server) SetOverseerDeliver(fn OverseerDeliverFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.overseerDeliver = fn
}

// senderResolver returns a live sender for a fleet agent, rehydrating a
// registered-but-stopped one. The seam lets hermetic tests drive the whole
// deliver path without launching provider processes (same pattern as
// server.notifySender). Nil — the product path — resolves via the registry.
type senderResolver func(name string) (proc agentSender, rehydrated bool, err error)

// SetSenderResolver overrides fleet-agent process resolution. Test seam.
func (s *Server) SetSenderResolver(fn senderResolver) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resolveSender = fn
}

// deliverByName is THE agent-addressed delivery op of the fleet layer:
// one call reaches ANY agent by name — worker, PO, or overseer — with
// hierarchy carried by lineage rather than by which API the caller holds.
// Delivery from the owner surface (and daemon-internal system notes); an
// in-fleet sender uses deliverByNameAs so its lineage is on the record.
func (s *Server) deliverByName(name, text string, origin SendOrigin, interrupt bool) (agentSendResult, error) {
	return s.deliverByNameAs(ActorOwnerSurface, name, text, origin, interrupt)
}

// deliverByNameAs is the same path with the sender named, so authorization is
// decided by lineage/policy here (🎯T309.3 acceptance 2) rather than by which
// API the caller could reach. See deliver_policy.go for the rules.
func (s *Server) deliverByNameAs(actor, name, text string, origin SendOrigin, interrupt bool) (agentSendResult, error) {
	name = strings.TrimSpace(name)
	actor = strings.TrimSpace(actor)
	if name == "" || strings.TrimSpace(text) == "" {
		return agentSendResult{}, fmt.Errorf("name and text are required")
	}

	rel, err := AuthorizeDeliver(s.registry, actor, name, origin, s.isOverseerAgent)
	if err != nil {
		slog.Warn("agent_send",
			"component", "agent_send",
			"name", name,
			"actor", actor,
			"relation", string(rel),
			"origin", string(origin),
			"status", "denied",
			"err", err.Error(),
		)
		return agentSendResult{}, err
	}

	if s.isOverseerAgent(name) {
		return s.deliverToOverseer(name, text, origin)
	}

	s.mu.Lock()
	resolve := s.resolveSender
	s.mu.Unlock()

	if resolve == nil {
		if s.registry == nil {
			return agentSendResult{}, fmt.Errorf("agent registry not available")
		}
		resolve = func(n string) (agentSender, bool, error) {
			proc, rehydrated, err := s.ensureAgentProcess(n)
			if err != nil {
				return nil, false, err
			}
			return proc, rehydrated, nil
		}
	}

	proc, rehydrated, err := resolve(name)
	if err != nil {
		return agentSendResult{}, err
	}
	return deliverToSender(s, name, text, interrupt, proc, rehydrated)
}

// deliverToOverseer is the overseer arm of the single path. Delivery itself
// belongs to the owner-chat layer (journal, owner bubble, notify queue), so
// this resolves the seam and reports the outcome in the same shape a fleet
// send reports — the caller cannot tell which arm ran.
func (s *Server) deliverToOverseer(name, text string, origin SendOrigin) (agentSendResult, error) {
	s.mu.Lock()
	deliver := s.overseerDeliver
	legacy := s.notifyJevon
	s.mu.Unlock()

	if deliver == nil && legacy != nil {
		if origin == OriginOwner {
			// The legacy injection cannot carry owner framing. Refusing is
			// the honest outcome: an owner turn delivered as an agent note
			// would misattribute the owner's own words.
			return agentSendResult{}, fmt.Errorf(
				"overseer %q cannot take an owner turn: no origin-carrying delivery seam wired", name)
		}
		deliver = func(t string, _ SendOrigin) error { legacy(t); return nil }
	}
	if deliver == nil {
		// 🎯T61/T62: an unreachable overseer is an error the caller sees and
		// can retry, never a logged-and-forgotten drop.
		return agentSendResult{}, fmt.Errorf("overseer %q is not reachable: no delivery seam wired", name)
	}

	if err := deliver(text, origin); err != nil {
		slog.Warn("agent_send",
			"component", "agent_send",
			"name", name,
			"origin", string(origin),
			"status", "failed",
			"err", err.Error(),
		)
		return agentSendResult{}, fmt.Errorf("send to overseer %q failed: %w", name, err)
	}

	res := agentSendResult{
		Status:  "sent",
		Message: fmt.Sprintf("Message delivered to overseer %q.", name),
	}
	slog.Info("agent_send",
		"component", "agent_send",
		"name", name,
		"origin", string(origin),
		"status", res.Status,
		"queued", 0,
		"rehydrated", false,
	)
	return res, nil
}

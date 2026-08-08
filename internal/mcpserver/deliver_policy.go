// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"

	"github.com/marcelocantos/claudia"
)

// 🎯T309.3 acceptance 2: who may say what to whom is decided HERE, on the one
// deliver path, rather than falling out of which API a caller happens to hold.
//
// Before the unification, reachability was the authorization: an agent could
// talk to the overseer only if it held the notifyJevon wire, and to a worker
// only if it could call MCP. That is not a policy — it is an accident of
// plumbing, and it is why "can a PO message the overseer by name?" had no
// answer independent of the caller's surface.
//
// The policy this fleet actually wants is permissive along lineage and strict
// about identity:
//
//   - REPORT UP is always allowed. A worker reaching its PO, and a PO reaching
//     the overseer, is the spine of the fleet; nothing may block it. The
//     overseer is addressable by every agent.
//   - DIRECT DOWN is always allowed. An ancestor may address any descendant —
//     the same lineage rule canKill uses, minus the destructiveness.
//   - PEER traffic is allowed on purpose. 🎯T309 acceptance 3 asks that any
//     fleet agent be able to message any other ("usual hierarchy root→PO→worker,
//     not restricted"); sibling workers coordinating a shared workdir is
//     ordinary, not an escalation. DeliverRelation still names the relationship
//     so a future policy can tighten it without re-deriving lineage.
//   - ORIGIN is not caller-selectable. OriginOwner asserts "the owner said
//     this": it carries the owner marker and paints an owner bubble. Only the
//     owner-facing surface may assert it. A fleet agent that could set it would
//     be able to put words in the owner's mouth inside the overseer's session,
//     which the pre-unification split prevented only by accident (the fleet
//     path had no way to express origin at all).
//
// The last rule is the one with teeth today, and the one the unification would
// otherwise have loosened.
//
// RESIDUAL — per-caller identity. MCP tool calls from every agent arrive over
// one shared transport, so jevons_agent_send cannot yet name its caller the
// way jevons_agent_kill does (which takes an explicit actor). Lineage denial
// is therefore decidable but not yet enforceable per-caller: AuthorizeDeliver
// is called with a known actor from internal fleet callers and with
// ActorOwnerSurface from the owner-facing paths. Until agent_send carries an
// actor, an agent could address a peer it has no lineage to — which today's
// policy permits anyway, so nothing is currently mis-permitted by the gap.

// ActorOwnerSurface is the actor for delivery originating outside the fleet:
// the owner's HTTP/UI surface and daemon-internal system notes. It is the only
// actor permitted to assert OriginOwner.
const ActorOwnerSurface = ""

// DeliverRelation names the lineage relationship between a sender and a
// recipient, so policy decisions read as hierarchy rather than string tests.
type DeliverRelation string

const (
	// RelationOwnerSurface is the owner (or the daemon itself) addressing an agent.
	RelationOwnerSurface DeliverRelation = "owner_surface"
	// RelationSelf is an agent addressing itself.
	RelationSelf DeliverRelation = "self"
	// RelationReportUp is a descendant addressing an ancestor (worker→PO→overseer).
	RelationReportUp DeliverRelation = "report_up"
	// RelationDirectDown is an ancestor addressing a descendant.
	RelationDirectDown DeliverRelation = "direct_down"
	// RelationPeer is cross-tree traffic between agents with no lineage between them.
	RelationPeer DeliverRelation = "peer"
)

// ClassifyDeliver reports the lineage relationship from actor to target.
// Pure: registry reads only. An unregistered actor that is not the owner
// surface classifies as RelationPeer — it has no lineage to speak of.
func ClassifyDeliver(reg *claudia.Registry, actor, target string, isOverseer func(string) bool) DeliverRelation {
	if actor == ActorOwnerSurface {
		return RelationOwnerSurface
	}
	if actor == target {
		return RelationSelf
	}
	// The overseer is the root: ancestor of every agent, descendant of none.
	if isOverseer != nil {
		if isOverseer(actor) {
			return RelationDirectDown
		}
		if isOverseer(target) {
			return RelationReportUp
		}
	}
	if reg == nil {
		return RelationPeer
	}
	if reg.IsAncestor(actor, target) {
		return RelationDirectDown
	}
	if reg.IsAncestor(target, actor) {
		return RelationReportUp
	}
	return RelationPeer
}

// AuthorizeDeliver decides whether actor may deliver to target with origin.
// Returns the relationship (for logging) and an error when denied.
//
// Every lineage relationship is permitted — the fleet talks freely, by design.
// What is refused is an identity claim: only the owner surface may speak as
// the owner.
func AuthorizeDeliver(reg *claudia.Registry, actor, target string, origin SendOrigin, isOverseer func(string) bool) (DeliverRelation, error) {
	rel := ClassifyDeliver(reg, actor, target, isOverseer)
	if origin == OriginOwner && rel != RelationOwnerSurface {
		return rel, fmt.Errorf(
			"denied: %q may not send as the owner — owner-origin turns carry the owner marker and paint an owner bubble, and only the owner surface may assert them (send without origin=owner to deliver as an agent)",
			actor,
		)
	}
	return rel, nil
}

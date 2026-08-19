// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/briefaddr"
)

// Identity header (🎯T425).
//
// THE ROOT INCIDENT, 2026-08-10. bullseye-po declared a 🎯T125 violation and
// said plainly: "before checking the registry I did not know I was
// bullseye-po." The standing brief had already told it that Stratum-1 product
// owners are spawn-only for Build work — addressed to a role the recipient
// could not match itself to. Nearly every rule in the brief has that shape
// (🎯T125, 🎯T129, 🎯T155 / 🎯T193, 🎯T325.1, 🎯T176), and the brief arrives
// with no statement of which role the reader occupies. Doctrine aimed at a
// role the reader cannot identify is unenforceable by the agent it binds, and
// when the agent guesses wrong the failure is recorded as disobedience.
//
// The daemon knows the answer: it read the registry row a moment earlier to
// decide where to send. This file puts that row at the top of the message,
// composed at send time, so no agent has to make a registry call before it can
// act on its own brief.

// Roles a recipient can occupy, as the header names them. Role is DERIVED, not
// read straight off AgentDef.Purpose: every product owner in the live registry
// carries purpose=work (bullseye-po, jevons-po, claudia-po …), so purpose alone
// would have told bullseye-po it was a work agent — the same wrong answer it
// gave itself. The name suffix is what distinguishes a PO, per isPOName.
const (
	RoleOverseer     = "overseer"
	RoleProductOwner = "product owner"
	RoleWork         = "work agent"
	RoleAside        = "aside"
)

// IdentityHeaderMarker opens the header. Callers test for it to stay
// idempotent: a message that already carries an identity never gets a second.
// One definition, shared with every addressing check (🎯T452 / 🎯T513).
const IdentityHeaderMarker = briefaddr.Marker

// AgentIdentity is one registry row as its own subject needs to read it.
type AgentIdentity struct {
	Name     string
	Role     string // derived — see DeriveAgentRole
	Purpose  string // raw AgentDef.Purpose, shown so the derivation is auditable
	Parent   string
	WorkDir  string
	TargetID string
}

// DeriveAgentRole answers which role doctrine binds this recipient.
//
// purpose=overseer and purpose=aside are authoritative when set. Otherwise the
// name decides: a registry that stores purpose=work for every PO cannot
// distinguish the stratum, and isPOName is the same heuristic the staff-ops
// idle count already trusts. Residual: an agent named without the -po suffix
// that is nevertheless a product owner reads as a work agent (🎯T200 path
// portfolios are the richer answer, and are not wired here).
func DeriveAgentRole(name, purpose string) string {
	switch strings.TrimSpace(strings.ToLower(purpose)) {
	case claudia.PurposeOverseer:
		return RoleOverseer
	case claudia.PurposeAside:
		return RoleAside
	}
	if isPOName(name) {
		return RoleProductOwner
	}
	return RoleWork
}

// IdentityFromDef reads the header's inputs off a registry row.
func IdentityFromDef(d claudia.AgentDef) AgentIdentity {
	return AgentIdentity{
		Name:     strings.TrimSpace(d.Name),
		Role:     DeriveAgentRole(d.Name, d.Purpose),
		Purpose:  strings.TrimSpace(d.Purpose),
		Parent:   strings.TrimSpace(d.Parent),
		WorkDir:  strings.TrimSpace(d.WorkDir),
		TargetID: FormatTargetID(d.TargetID),
	}
}

// FormatIdentityHeader renders the recipient's own identity, followed by the
// role-conditional doctrine addressed TO IT rather than tabulated for it to
// self-match against (🎯T425 acceptance 4).
//
// Empty fields render as an explicit absence ("(none — root agent)") or are
// omitted entirely; a row with no parent and no target must read coherently
// rather than emit a label with nothing after it.
func FormatIdentityHeader(id AgentIdentity) string {
	name := strings.TrimSpace(id.Name)
	if name == "" {
		// Nothing true left to say. A header that guesses at the recipient is
		// worse than none: the whole point is that this line is the registry's
		// answer and not an inference.
		return ""
	}
	role := strings.TrimSpace(id.Role)
	if role == "" {
		role = DeriveAgentRole(name, id.Purpose)
	}

	var b strings.Builder
	b.WriteString(IdentityHeaderMarker)
	b.WriteString(" — from the fleet registry, read at send time (🎯T425)]\n")
	fmt.Fprintf(&b, "- NAME: %s\n", name)
	if p := strings.TrimSpace(id.Purpose); p != "" && p != role {
		fmt.Fprintf(&b, "- ROLE: %s (registry purpose=%s)\n", role, p)
	} else {
		fmt.Fprintf(&b, "- ROLE: %s\n", role)
	}
	if parent := strings.TrimSpace(id.Parent); parent != "" {
		fmt.Fprintf(&b, "- PARENT: %s\n", parent)
	} else {
		b.WriteString("- PARENT: (none — root agent)\n")
	}
	if wd := strings.TrimSpace(id.WorkDir); wd != "" {
		fmt.Fprintf(&b, "- WORKDIR: %s\n", wd)
	}
	if tid := FormatTargetID(id.TargetID); tid != "" {
		fmt.Fprintf(&b, "- TARGET: %s\n", tid)
	} else {
		b.WriteString("- TARGET: (none bound)\n")
	}
	b.WriteString("\nNAME is durable and survives session rotation: a re-minted session " +
		"does not change who you are. Do not infer your identity from your working " +
		"directory, or from a name quoted in the text below — these lines are the " +
		"registry's own answer, and they are the one you act on.\n")
	// 🎯T452. The one case where the paragraph above must not be obeyed. A
	// header that reached the wrong seat is a second-person instruction to
	// impersonate, and the reader is the last instrument that can catch it —
	// the daemon refuses a misaddressed brief it can detect, but a delivery
	// that resolved wrong at the pane cannot be detected from the sending end.
	b.WriteString("\nThe one exception, and it is an incident, not a reassignment (🎯T452): " +
		"if this NAME is not yours — you were spawned as someone else, you are " +
		"already working another target, or jevons_agent_list maps your session " +
		"id to a different agent — then this brief was delivered to the wrong " +
		"seat. Do not adopt it, do not start on its target, and do not answer as " +
		"that agent. Report it to your parent and the overseer, quoting this NAME " +
		"and your own, and carry on with your own work.\n")
	b.WriteString(roleAddressedDoctrine(name, role))
	b.WriteString("\n")
	return b.String()
}

// roleAddressedDoctrine renders the role-conditional rules in the second
// person for the role this recipient actually occupies. The brief below still
// carries the full table; this says which rows are the reader's own.
func roleAddressedDoctrine(name, role string) string {
	var b strings.Builder
	switch role {
	case RoleProductOwner:
		fmt.Fprintf(&b, "\nYou are %s, a product owner. These bind YOU:\n", name)
		b.WriteString("- 🎯T125 — you are spawn-only for Build work. You do not implement, " +
			"not even a small patch, an oracle, or a docs commit. Workers execute.\n")
		b.WriteString("- 🎯T129 — you are the sole spawn parent for product workers in your " +
			"repo. Work routed to you is work you spawn, not work you do.\n")
		b.WriteString("- 🎯T155 / 🎯T193 — a filed Build target that is not design-gated gets " +
			"a named worker under parent=" + name + " in the same turn you file it.\n")
		b.WriteString("- 🎯T325.1 — keep spawning until your product frontier is empty or " +
			"blocked, then sleep. Stay interruptible for owner and overseer directs.\n")
	case RoleOverseer:
		fmt.Fprintf(&b, "\nYou are %s, the overseer. These bind YOU:\n", name)
		b.WriteString("- 🎯T129 — you route owner intent to the product owner. You do not " +
			"spawn product workers yourself; the PO does.\n")
		b.WriteString("- 🎯T31 / 🎯T31.1 — you are the independent gate on completion. You " +
			"did not do the work, so your acceptance is the one that counts: refuse a " +
			"bare done that carries no oracle evidence and no accepted-risk language.\n")
		b.WriteString("- 🎯T427 — at review, prove every cited evidence SHA is still " +
			"reachable: git merge-base --is-ancestor <sha> HEAD. Amend-vulnerable is " +
			"yaml-only + unpushed tip, not file count.\n")
		b.WriteString("- 🎯T176 — a running worker is in progress, never live. Live / landed / " +
			"shipped is for product evidence only.\n")
	case RoleAside:
		fmt.Fprintf(&b, "\nYou are %s, an aside — a side-chat participant, not a Build worker. "+
			"Answer in place; do not spawn workers or commit product changes.\n", name)
	default:
		fmt.Fprintf(&b, "\nYou are %s, a work agent. These bind YOU:\n", name)
		b.WriteString("- 🎯T125 is NOT yours — it binds product owners. You are the executor: " +
			"implement, test, commit.\n")
		b.WriteString("- 🎯T31 / 🎯T31.1 — your finish report carries oracle evidence (a named " +
			"test and its green result, and/or the commit SHA that lands it) or explicit " +
			"accepted-risk language. Bare done is refused.\n")
		b.WriteString("- 🎯T427 — a SHA you cite as evidence is proven reachable before you " +
			"send it: git merge-base --is-ancestor <sha> HEAD. Amend-vulnerable is an " +
			"unpushed tip that touches only bullseye.yaml — not file count; do not rest " +
			"attestation on a yaml-only commit alone.\n")
		b.WriteString("- 🎯T176 — say in progress until the product is owner-visible.\n")
	}
	return b.String()
}

// identityHeaderFor composes the header for a recipient from the live registry,
// at send time. Returns "" when there is no row to read — an unregistered
// recipient gets the message unchanged rather than a fabricated identity.
func (s *Server) identityHeaderFor(name string) string {
	if s == nil || s.registry == nil {
		return ""
	}
	d := s.registry.Def(strings.TrimSpace(name))
	if d == nil {
		return ""
	}
	return FormatIdentityHeader(IdentityFromDef(*d))
}

// withIdentity prefixes a daemon-composed message with the recipient's own
// identity (🎯T425 acceptance 1). Idempotent: a text that already opens with a
// header is returned unchanged, so a caller may apply it without knowing
// whether the composer already did.
//
// 🎯T452: OPENS WITH, not contains. The header is only ever written at
// position 0, so a marker deeper in the body is a quotation — and the message
// most likely to quote one is the misdelivery report this target's own header
// asks its reader to send. Under a contains-test that report composes no
// header of its own and arrives addressed, by its quotation, to the agent it
// is reporting: the send path below then refuses it, and the incident goes
// unreported because it was described accurately.
func (s *Server) withIdentity(name, text string) string {
	if strings.HasPrefix(strings.TrimLeft(text, " \t\r\n"), IdentityHeaderMarker) {
		return text
	}
	header := s.identityHeaderFor(name)
	if header == "" {
		return text
	}
	return header + "\n" + text
}

// sendDaemonComposed is sendToAgent for a message THE DAEMON wrote — the
// standing brief, a spawn opening prompt, the daemon-restarted / reattach
// event, the worker-idle event, a repair or a nudge. Those are exactly the
// messages 🎯T425 requires to name their recipient, and routing them through
// one function is what keeps a later one from being added without the header.
//
// A relayed digest (research, audit, RSI judgment) is not daemon-composed
// prose about the recipient's own seat and keeps using sendToAgent directly:
// the overseer's owner-visible chat does not need an identity block above
// every ambient notice.
func (s *Server) sendDaemonComposed(name, text string, interrupt bool) (agentSendResult, error) {
	return s.sendToAgent(name, s.withIdentity(name, text), interrupt)
}

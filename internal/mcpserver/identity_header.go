// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/briefaddr"
	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/roles"
)

// Identity header (🎯T425).
//
// THE ROOT INCIDENT, 2026-08-10. bullseye-po declared a 🎯T125 violation and
// said plainly: "before checking the registry I did not know I was
// bullseye-po." The standing brief had already told it that Stratum-1 product
// owners are spawn-only for Build work — addressed to a role the recipient
// could not match itself to. Nearly every rule in the brief has that shape
// (🎯T125, 🎯T129, 🎯T155 / 🎯T193, 🎯T325.1, 🎯T176, 🎯T692), and the brief arrives
// with no statement of which role the reader occupies. Doctrine aimed at a
// role the reader cannot identify is unenforceable by the agent it binds, and
// when the agent guesses wrong the failure is recorded as disobedience.
//
// The daemon knows the answer: it read the registry row a moment earlier to
// decide where to send. This file puts that row at the top of the message,
// composed at send time, so no agent has to make a registry call before it can
// act on its own brief.

// Roles a recipient can occupy, as the header names them. Role is derived
// from the explicit role and the declared purpose. A seat name does not
// decide the stratum.
const (
	RoleOverseer     = "overseer"
	RoleProductOwner = "product owner"
	RoleWork         = "work agent"
	RoleAside        = "aside"
	RoleAuditor      = "auditor"
)

// IdentityHeaderMarker is the rendered header line. Idempotence is the
// declared identity fact on line 1, not this marker (🎯T766.4). One
// definition, shared with every addressing check (🎯T452 / 🎯T513).
const IdentityHeaderMarker = briefaddr.Marker

// AgentIdentity is one registry row as its own subject needs to read it.
type AgentIdentity struct {
	Name     string
	Role     string // derived — see DeriveAgentRole
	Purpose  string // raw AgentDef.Purpose, shown so the derivation is auditable
	Parent   string
	WorkDir  string
	TargetID string
	// StateDir is the daemon's own state directory, set only when it is not
	// the development daemon's ~/.jevons (🎯T1082).
	StateDir string
}

// DeriveAgentRole answers which role doctrine binds this recipient.
//
// Explicit role (from agent_roles.json / spawn role=, 🎯T511 / 🎯T536.2) wins
// when set. Otherwise purpose=overseer, purpose=aside, and
// purpose=product-owner are authoritative. A name suffix is not a role.
// Boss purpose reads as a work agent: product-owner doctrine is the
// product-owner purpose, not every coordinator.
func DeriveAgentRole(name, purpose, role string) string {
	_ = name
	switch roles.Normalize(role) {
	case roles.Auditor:
		return RoleAuditor
	case roles.ProductOwner:
		return RoleProductOwner
	case roles.Overseer:
		return RoleOverseer
	case roles.Aside:
		return RoleAside
	case roles.Worker, roles.Boss:
		return RoleWork
	}
	switch strings.TrimSpace(strings.ToLower(purpose)) {
	case claudia.PurposeOverseer:
		return RoleOverseer
	case claudia.PurposeAside:
		return RoleAside
	case purposeProductOwner:
		return RoleProductOwner
	}
	return RoleWork
}

// IdentityFromDef reads the header's inputs off a registry row: the row's
// declared role (AgentDef.Role), else its purpose. identityHeaderFor
// overlays the recorded spawn role from agent_roles.json when present.
func IdentityFromDef(d claudia.AgentDef) AgentIdentity {
	return AgentIdentity{
		Name:     strings.TrimSpace(d.Name),
		Role:     DeriveAgentRole(d.Name, d.Purpose, d.Role),
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
		role = DeriveAgentRole(name, id.Purpose, "")
	}

	var b strings.Builder
	b.WriteString(envelope.PrefixIdentity)
	b.WriteByte('\n')
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
	if sd := strings.TrimSpace(id.StateDir); sd != "" {
		// 🎯T1082: an isolate's agents, told only that development state is
		// ~/.jevons, went and read it — one opened the development
		// database with sqlite3 from inside the journey suite.
		fmt.Fprintf(&b, "- STATE DIR: %s — this daemon's own state. It is not the development "+
			"daemon's ~/.jevons: never read, list, open, query or write anything under ~/.jevons; "+
			"a ~/.jevons/... path in your instructions means %s/... here (🎯T1082).\n", sd, sd)
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
	b.WriteByte('\n')
	// The end fact is the payload boundary. The doctrine above it stays
	// display; stripLeadingIdentityEnvelope does not read those sentences.
	b.WriteString(envelope.PrefixIdentityEnd)
	b.WriteByte('\n')
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
		b.WriteString("- 🎯T692 — a kill/stop/reap/respawn report cites a live " +
			"jevons_agent_list / GET /api/agents result from this turn, or stays " +
			"intent ('killing', 'will stop'). Do not claim a running seat is dead.\n")
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
		b.WriteString("- 🎯T692 — a kill/stop/reap/respawn claim is intent ('killing') or " +
			"cites a live registry check from this turn. Do not tell the owner a " +
			"running seat is dead.\n")
	case RoleAside:
		fmt.Fprintf(&b, "\nYou are %s, an aside — a side-chat participant, not a Build worker. "+
			"Answer in place; do not spawn workers or commit product changes.\n", name)
	case RoleAuditor:
		fmt.Fprintf(&b, "\nYou are %s, an auditor (🎯T536.2). These bind YOU:\n", name)
		b.WriteString("- You are not the implementer of the work under review. Do not write or " +
			"patch that product code; do not fix to make the report look clean.\n")
		b.WriteString("- Challenge the silent-decision ledger (🎯T536.1 / ReadSilentLedger), not " +
			"the implementation diff as the primary surface.\n")
		b.WriteString("- Report findings up. Do not file bullseye — root/PO decide.\n")
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
		b.WriteString("- 🎯T692 — lifecycle facts (killed/stopped/reaped/respawned) cite a " +
			"live registry check from this turn, or stay intent.\n")
		b.WriteString("- 🎯T509 — a terminal report MUST be a jevons finish-report envelope " +
			"(fenced ```jevons, jevons: slots). Schema and enums: internal/envelope.\n")
		// 🎯T1083: read-only workers wrote `status done`, which no status
		// value is. The enum is read from the schema, not restated.
		statuses := make([]string, 0, len(envelope.ProgressValues()))
		for _, p := range envelope.ProgressValues() {
			statuses = append(statuses, string(p))
		}
		b.WriteString("- 🎯T1083 — a done finish-report OMITS status (status takes only " +
			strings.Join(statuses, " | ") + "; none of them means done) and carries " +
			"`jevons: oracle sha=<head-sha>` — the commit that lands your work, or for a " +
			"read-only task the HEAD you measured — plus `jevons: silent-ledger none` " +
			"or a ranked ledger. Shape: the fleet standing brief.\n")
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
	id := IdentityFromDef(*d)
	if r := s.agentRole(d.Name, d.Purpose); r != "" {
		id.Role = DeriveAgentRole(d.Name, d.Purpose, r)
	}
	id.StateDir = s.isolateStateDir()
	return FormatIdentityHeader(id)
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
	body := strings.TrimLeft(text, " \t\r\n")
	nl := strings.IndexByte(body, '\n')
	first := body
	if nl >= 0 {
		first = body[:nl]
	}
	if strings.TrimSpace(first) == envelope.PrefixIdentity {
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

// SetDaemonStateDir records the daemon's state directory, so the identity
// header can tell an isolate's agents where their state is (🎯T1082). It is
// an atomic, not s.mu: the header is composed on send paths that hold s.mu.
func (s *Server) SetDaemonStateDir(dir string) {
	s.daemonStateDir.Store(strings.TrimSpace(dir))
}

// isolateStateDir is the daemon's state directory when it is not the
// development daemon's own, else "".
func (s *Server) isolateStateDir() string {
	dir, _ := s.daemonStateDir.Load().(string)
	if dir == "" || (config.Config{StateDir: dir}).IsDevelopmentState() {
		return ""
	}
	return dir
}

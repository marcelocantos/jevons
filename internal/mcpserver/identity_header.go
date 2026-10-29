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
	// RoleDoc is the repository-relative path to the authoritative role file
	// (🎯T511). Empty when the role could not be resolved.
	RoleDoc string
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

// FormatIdentityHeader renders the recipient's own identity from the registry
// row (🎯T425). Role-conditional doctrine lives in the role file named by
// RoleDoc and is injected separately via EnsureFleetBriefWithRole (🎯T511).
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
	if doc := strings.TrimSpace(id.RoleDoc); doc != "" {
		fmt.Fprintf(&b, "- ROLE DOCTRINE: %s — authoritative role file; the same "+
			"body is injected below as %q on the first standing brief (🎯T511).\n",
			doc, roles.DoctrineMarker)
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
	b.WriteByte('\n')
	// The end fact is the payload boundary. Registry facts above it stay
	// display; stripLeadingIdentityEnvelope does not read those sentences.
	b.WriteString(envelope.PrefixIdentityEnd)
	b.WriteByte('\n')
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
	roleKey := s.agentRole(d.Name, d.Purpose)
	if roleKey != "" {
		id.Role = DeriveAgentRole(d.Name, d.Purpose, roleKey)
	}
	if def, err := s.resolveRoleDef(roleKey); err == nil {
		id.RoleDoc = roles.RepoRelPath(def)
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

// recordedDaemonStateDir is the state directory SetDaemonStateDir recorded,
// or "" when none was. State paths the daemon resolves lazily are rooted
// here, never at $HOME/.jevons (🎯T1090).
func (s *Server) recordedDaemonStateDir() string {
	dir, _ := s.daemonStateDir.Load().(string)
	return dir
}

// isolateStateDir is the daemon's state directory when it is not the
// development daemon's own, else "".
func (s *Server) isolateStateDir() string {
	dir := s.recordedDaemonStateDir()
	if dir == "" || (config.Config{StateDir: dir}).IsDevelopmentState() {
		return ""
	}
	return dir
}

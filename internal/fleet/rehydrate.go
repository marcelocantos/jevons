// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cli"
)

// Lost-session rehydrate (🎯T313).
//
// claudia fails closed on resume: once a registry row is Materialized,
// Launch passes RequireResume, and a missing Claude JSONL is a hard
// error rather than a silently minted replacement conversation. That
// policy is right — quietly starting a blank agent under a name whose
// history the caller believes is intact is the worse failure.
//
// It is also a dead end. Materialized is set the instant the process
// spawns, not when a conversation exists, so an agent whose first turn
// never submitted has Materialized=true and no JSONL — forever. Every
// later launch refuses, and recovery was a manual kill → start →
// re-brief, per worker, on every daemon restart.
//
// This is the third option: notice that the session is unresumable
// BEFORE asking claudia to resume it, rotate the row onto a fresh
// session id while preserving everything that identifies the agent
// (name, parent lineage, purpose, provider, model, target binding),
// and hand the caller an explicit account of what was lost so the
// brief can be re-sent in the same step. The mint is deliberate and
// reported, which is precisely what claudia's guard exists to prevent
// happening by accident.

// LostSession records a registry row whose provider session could not
// be resumed, and the fresh session it was rotated onto. It carries the
// lineage that survived so a caller can see the rehydrate preserved it.
type LostSession struct {
	Name       string
	WorkDir    string
	Provider   claudia.Provider
	Model      string
	Parent     string
	Purpose    string
	TargetID   string
	OldSession string // the conversation that is gone
	NewSession string // the fresh session it now runs under
	JSONLPath  string // where the lost transcript would have been
}

// Describe renders the owner/PO-facing account of the rehydrate. It
// names the lost session id and states plainly that prior context is
// gone, because the only safe assumption for the caller is that the
// agent remembers nothing (🎯T313 acceptance 2).
func (l LostSession) Describe() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Rehydrated %q on a FRESH session: its previous conversation %s is gone "+
		"(no transcript at %s).", l.Name, l.OldSession, l.JSONLPath)
	b.WriteString(" PRIOR CONTEXT IS LOST — the agent remembers nothing; re-send its brief.")
	fmt.Fprintf(&b, " Preserved: parent=%s purpose=%s provider=%s", dashIfEmpty(l.Parent),
		dashIfEmpty(l.Purpose), dashIfEmpty(string(l.Provider)))
	if l.Model != "" {
		fmt.Fprintf(&b, " model=%s", l.Model)
	}
	if l.TargetID != "" {
		fmt.Fprintf(&b, " target_id=%s", l.TargetID)
	}
	fmt.Fprintf(&b, ". New session: %s.", l.NewSession)
	return b.String()
}

func dashIfEmpty(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

// SessionLost reports whether def demands a resume that cannot succeed:
// the row is Materialized (so Launch will pass RequireResume) but the
// Claude transcript backing its session id is not on disk.
//
// Only Claude is decidable here. Grok and Codex keep their sessions in
// provider-owned stores that jevons does not stat, and their resume
// semantics live behind ACP session/load — guessing "lost" from a file
// that was never expected to exist would rotate healthy agents onto
// blank sessions, which is the exact harm this path is meant to avoid.
func SessionLost(def *claudia.AgentDef) bool {
	if def == nil || !def.Materialized || def.SessionID == "" {
		return false
	}
	if p := def.Provider; p != "" && p != claudia.ProviderClaude {
		return false
	}
	exists, err := claudia.SessionExists(def.SessionID, def.WorkDir)
	if err != nil {
		// A stat error (permission denied, unreadable mount) is not
		// evidence of absence. Leave it to claudia to fail closed.
		return false
	}
	return !exists
}

// RehydratedDef returns def rotated onto newSessionID: a fresh
// conversation under the same identity.
//
// Everything that identifies the agent rides along by value copy —
// name, workdir, provider, parent, purpose, target binding, auto-start.
// Session and liveness bookkeeping change:
//
//   - Materialized=false, so the next Launch mints rather than resumes.
//   - ConnectURL/ConnectPID cleared. def is snapshotted before Stop
//     clears them on the registry's own copy, so re-registering the
//     snapshot would persist a dead endpoint and send the next Launch
//     into a reattach that resets (the 🎯T204 trap, reached here by the
//     same road as provider migration).
//   - 🎯T324: model is re-bound for the new SessionID (pin kept when
//     same-provider, else provider default) — never a sticky observation
//     from the lost session; the hub SyncEpoch-clears on rotate separately.
func RehydratedDef(def claudia.AgentDef, newSessionID string) claudia.AgentDef {
	next := def
	next.SessionID = newSessionID
	next.Materialized = false
	next.ConnectURL = ""
	next.ConnectPID = 0
	next.Model = cli.BindSessionModel(def.Model, def.Provider)
	return next
}

// RehydrateLostSession rotates a registered agent onto a fresh session
// when its current one cannot be resumed, preserving lineage and target
// binding. It does NOT launch — the caller launches, so a failed launch
// leaves a clean rotated row rather than a half-recovered agent.
//
// ok=false with a nil error means nothing was wrong and the caller
// should proceed to a normal resume.
func (f *Claudia) RehydrateLostSession(name string) (LostSession, bool, error) {
	if f == nil {
		return LostSession{}, false, fmt.Errorf("rehydrate: no fleet")
	}
	return RehydrateLostSessionIn(f.reg, name)
}

// RehydrateLostSessionIn is RehydrateLostSession against a registry
// directly. The MCP agent_start handler holds a *claudia.Registry
// rather than a Fleet, and recovery has to happen on that path — it is
// where the dead-end is actually hit.
func RehydrateLostSessionIn(reg *claudia.Registry, name string) (LostSession, bool, error) {
	if reg == nil {
		return LostSession{}, false, fmt.Errorf("rehydrate: no agent registry")
	}
	def := reg.Def(name)
	if def == nil {
		return LostSession{}, false, fmt.Errorf("rehydrate: no agent %q", name)
	}
	if !SessionLost(def) {
		return LostSession{}, false, nil
	}

	lost := LostSession{
		Name:       def.Name,
		WorkDir:    def.WorkDir,
		Provider:   def.Provider,
		Model:      def.Model,
		Parent:     def.Parent,
		Purpose:    def.Purpose,
		TargetID:   def.TargetID,
		OldSession: def.SessionID,
		NewSession: uuid.NewString(),
		JSONLPath:  claudia.SessionJSONLPath(def.SessionID, def.WorkDir),
	}

	// Stop any half-alive process before rotating: the row is about to
	// point somewhere else, and a survivor would keep writing under the
	// id we just abandoned.
	reg.Stop(name)

	if err := reg.Register(RehydratedDef(*def, lost.NewSession)); err != nil {
		return LostSession{}, false, fmt.Errorf("rehydrate %q: register fresh session: %w", name, err)
	}

	slog.Warn("agent session lost; rehydrated on a fresh conversation",
		"name", name, "old_session", lost.OldSession, "new_session", lost.NewSession,
		"jsonl", lost.JSONLPath, "parent", lost.Parent, "purpose", lost.Purpose,
		"provider", string(lost.Provider), "target_id", lost.TargetID)
	return lost, true, nil
}

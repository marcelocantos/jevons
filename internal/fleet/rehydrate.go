// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/marcelocantos/jevons/internal/seatstate"

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

// StatusDeadUnmaterialized is the fleet-wide status label for a seat whose
// registry row claims a conversation that does not exist on disk (🎯T412):
// the durable Materialized flag points at a session id with no transcript,
// and no confirmed turn backs the claim. Such a seat is dead, not running —
// agent_list, the RHS tree, and the sentinel snapshot all report it with
// this label rather than nudging it forever. Defined here because both the
// MCP layer and the web server derive it, and this package owns the
// underlying predicate (SessionLost).
const StatusDeadUnmaterialized = "dead_unmaterialized"

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
	// Refresh the resume-evidence feed at the explicit launch boundary.
	a := seatstate.RegistryAuthority(reg)
	a.Observe(seatstate.Observation{Name: name, SessionID: def.SessionID, QueueDepth: seatstate.QueueUnknown, Source: "registry.identity"})
	a.ObserveResumeEvidence(def)
	if seatstate.ReadRegistry(reg, name).ResumeLost != seatstate.Yes {
		return LostSession{}, false, nil
	}
	lost, err := rotateOntoFreshSession(reg, def)
	if err != nil {
		return LostSession{}, false, err
	}
	return lost, true, nil
}

// rotateOntoFreshSession is the T313 rotation: new session id, same
// identity, Materialized cleared so the next Launch mints. Callers that
// already have a definitive resume refusal (Cursor Invalid params) use
// this even when SessionLost cannot see the provider store.
func rotateOntoFreshSession(reg *claudia.Registry, def *claudia.AgentDef) (LostSession, error) {
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
	reg.Stop(def.Name)

	if err := reg.Register(RehydratedDef(*def, lost.NewSession)); err != nil {
		return LostSession{}, fmt.Errorf("rehydrate %q: register fresh session: %w", def.Name, err)
	}

	slog.Warn("agent session lost; rehydrated on a fresh conversation",
		"name", def.Name, "old_session", lost.OldSession, "new_session", lost.NewSession,
		"jsonl", lost.JSONLPath, "parent", lost.Parent, "purpose", lost.Purpose,
		"provider", string(lost.Provider), "target_id", lost.TargetID)
	return lost, nil
}

// LaunchRecovering is registry.Launch with lost-session recovery in
// front of it (🎯T409 / 🎯T674).
//
// claudia fails closed when a row is Materialized and its transcript is
// gone, which is correct — silently minting a replacement would discard
// history without saying so. But every caller then has to know to
// rehydrate first, and on 2026-08-10 three separate paths did not:
// jevons_agent_start had it, ensureAgentProcess did not (the impatience
// ladder retried the same error on a timer forever), and cockpit
// converge did not (the overseer failed eight launches and gave up,
// taking owner chat and the fleet's animus with it).
//
// Thirteen call sites reach registry.Launch. Recovery belongs here, once,
// rather than in each of them — a guard that every caller must remember
// is a guard that some caller will forget.
//
// 🎯T674: when Launch fails because the exclusive provider home is gone
// (GROK_HOME / CODEX_HOME), rotate onto a fresh session under the same
// name rather than leaving the seat dead until someone kills it by hand.
func LaunchRecovering(reg *claudia.Registry, name string) (*claudia.Agent, error) {
	agent, _, err := LaunchRecoveringReported(context.Background(), reg, name)
	return agent, err
}

// LaunchRecoveringReported is LaunchRecovering and also returns any remint
// that ran, so a start path can name the lost session once instead of
// silently claiming the old conversation continues (🎯T674 acceptance 2).
// ctx cancels the underlying Launch (🎯T541.2).
func LaunchRecoveringReported(ctx context.Context, reg *claudia.Registry, name string) (*claudia.Agent, LostSession, error) {
	defer seatstate.ObserveRegistrySeat(reg, name)
	if reg == nil {
		return nil, LostSession{}, fmt.Errorf("launch %q: no agent registry", name)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var reported LostSession
	if lost, ok, err := RehydrateLostSessionIn(reg, name); err != nil {
		// Not fatal: fall through and let Launch report the real problem.
		slog.Warn("lost-session rehydrate failed; launching anyway",
			"name", name, "err", err)
	} else if ok {
		reported = lost
		slog.Info("launch rehydrated lost session", "name", name, "detail", lost.Describe())
	}
	agent, err := LaunchReconciledContext(ctx, reg, name)
	if remintAfterResumeError(reg.Def(name), err) {
		def := reg.Def(name)
		rotated, rerr := rotateOntoFreshSession(reg, def)
		if rerr != nil {
			slog.Warn("resume-denied rehydrate failed", "name", name, "err", rerr)
			return nil, reported, err
		}
		reported = rotated
		slog.Warn("launch reminted after provider resume refusal",
			"name", name, "detail", rotated.Describe())
		agent, err = LaunchReconciledContext(ctx, reg, name)
		return agent, reported, err
	}
	return agent, reported, err
}

// Resume refusals that report the conversation absent, not unresumable.
// Claude has no transcript at all; a sidecar seat (anthropic, openai, …) has
// no spool record under its name. Each is the 🎯T313 case: nothing exists to
// discard, and every later launch refuses the same way.
const (
	absentClaudeJSONL  = "existing conversation required but JSONL not found at"
	absentSidecarSpool = "existing conversation required but no spool records for seat"
)

// HistoryAbsent reports that err is a resume refusal because the seat has no
// conversation history anywhere, so minting a fresh session loses nothing.
//
// A seat the broker took down while it was on a sidecar provider hit this
// forever (🎯T935, 2026-09-30): jv-t928-mcp-attach had no spool records, the
// broker-lost relaunch retried every 20s for twelve minutes, and the owner
// killed and reminted it by hand — the same mint this makes. A refusal to
// resume a store that does exist (Grok or Cursor session/load, Codex
// thread/resume) is not this, and still fails closed.
func HistoryAbsent(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, absentClaudeJSONL) || strings.Contains(msg, absentSidecarSpool)
}

// SessionHomeMissing reports that err is a resume refusal because the
// exclusive provider home for the stored session is gone from disk
// (🎯T674). Claudia will not mkdir on resume for Grok or Codex; a cold
// provider switch or a deleted state tree leaves every later launch
// stating a path that cannot exist. The seat must remint, not sit dead
// until someone kills it by hand.
//
// Matched strings are claudia's own wording:
//   - "exclusive GROK_HOME unavailable for session …"
//   - "thread/resume: exclusive CODEX_HOME missing at …"
// A session/load failure on a store that still exists is not this.
func SessionHomeMissing(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "exclusive GROK_HOME unavailable") ||
		strings.Contains(msg, "exclusive CODEX_HOME missing")
}

// remintAfterResumeError is the 🎯T627.1 / 🎯T674 gate: a failed Launch may
// rotate onto a fresh session when the history is absent (HistoryAbsent),
// when the exclusive provider home is gone (SessionHomeMissing), and
// otherwise only for Cursor session/load Invalid params (🎯T541.1 —
// stacking a second ACP writer on the same store).
//
// Grok's session/load failure ("existing conversation; refusing to mint")
// shares wording with [claudia.ErrCursorResumeDenied] and must still fail
// closed — SessionHomeMissing is the only Grok path that remints.
// SessionHomeMissing is provider-agnostic on purpose: the 🎯T674 specimen
// had provider=claude on the row while the stored session pointed at a
// Grok exclusive home, and matching on the declared provider would have
// left that seat dead.
func remintAfterResumeError(def *claudia.AgentDef, err error) bool {
	if err == nil || def == nil {
		return false
	}
	if HistoryAbsent(err) || SessionHomeMissing(err) {
		return true
	}
	if def.Provider == claudia.ProviderCursor {
		return claudia.IsCursorResumeDenied(err)
	}
	return false
}

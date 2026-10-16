// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package fleet

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/marcelocantos/jevons/internal/seatstate"

	"github.com/google/uuid"
	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cli"
)

// Lost-session rehydrate (🎯T313).
// Explicit generation-scoped loss evidence may authorize a replacement.
// Missing vendor files, spool projections and provider error prose cannot
// establish that canonical Claudia history is absent.

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

// StatusDeadUnmaterialized is the fleet-wide label for an explicitly reported
// lost conversation (🎯T412). Materialized state and historical vendor files
// alone cannot establish that loss; Claudia owns canonical generation history.
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

// rotateOntoFreshSession replaces an explicitly lost generation while
// preserving the agent identity and lineage. It never infers loss itself.
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

// LaunchRecovering applies explicit loss recovery and reconciles provider
// capabilities before launch. A refused canonical resume stays refused under
// the same generation; legacy error text never authorizes another mint.
func LaunchRecovering(reg *claudia.Registry, name string) (*claudia.Agent, error) {
	defer seatstate.ObserveRegistrySeat(reg, name)
	if reg == nil {
		return nil, fmt.Errorf("launch %q: no agent registry", name)
	}
	if lost, ok, err := RehydrateLostSessionIn(reg, name); err != nil {
		// Not fatal: fall through and let Launch report the real problem.
		slog.Warn("lost-session rehydrate failed; launching anyway",
			"name", name, "err", err)
	} else if ok {
		slog.Info("launch rehydrated lost session", "name", name, "detail", lost.Describe())
	}
	return LaunchReconciled(reg, name)
}

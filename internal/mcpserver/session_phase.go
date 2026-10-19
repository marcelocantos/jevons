// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"bytes"
	"strings"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/discovery"
	"github.com/marcelocantos/jevons/internal/seatactivity"
	"github.com/marcelocantos/jevons/internal/seatprovider"
	"github.com/marcelocantos/jevons/internal/spool"
	"github.com/marcelocantos/jevons/internal/turnev"
)

// DefaultSessionRoots is the on-disk pair the development daemon reads: Grok
// sessions (ordinary, exclusive-MCP, and durable claudia grok-homes) and
// Claude projects. Tests override via SessionPhase. Omitting exclusive-MCP
// homes made Grok absence unprovable for those seats (🎯T679.1). Omitting
// grok-homes marked a live Grok PO born-stuck while updates.jsonl grew
// (🎯T694).
func DefaultSessionRoots() discovery.Roots {
	return seatactivity.DefaultRoots()
}

// AgentTranscriptPath is the current session file for d — registry session
// id at the moment of the read, not a stale id (🎯T423 clause 6).
func AgentTranscriptPath(d claudia.AgentDef, roots discovery.Roots) string {
	if seatprovider.Resolve(d.Provider).Sidecar && spool.SeatHasHistory(spool.Dir(), d.Name) {
		// Sidecar seats have no vendor JSONL. Callers that need bytes
		// use classifyAgentSessionPhase / spool.ReadSeat (🎯T866.4).
		return ""
	}
	sid := strings.TrimSpace(d.SessionID)
	if sid == "" {
		return ""
	}
	if p := discovery.TranscriptPath(roots, sid); p != "" {
		return p
	}
	if roots.ClaudeProjects != "" && strings.TrimSpace(d.WorkDir) != "" {
		if p := discovery.ClaudeJSONLPathForWorkDir(roots.ClaudeProjects, d.WorkDir, sid); p != "" {
			return p
		}
	}
	return ""
}

// classifyAgentSessionPhase is the product idle/repair reading: T422
// Decode via ClassifyPhase on the agent's current session. Missing or
// unreadable is unknown, never idle.
func classifyAgentSessionPhase(d claudia.AgentDef, roots discovery.Roots) turnev.Phase {
	if seatprovider.Resolve(d.Provider).Sidecar && spool.SeatHasHistory(spool.Dir(), d.Name) {
		recs, err := spool.ReadSeat(spool.Dir(), d.Name)
		if err != nil || len(recs) == 0 {
			return turnev.PhaseUnknown
		}
		return turnev.ClassifyPhase(turnev.DecodeAll(bytes.NewReader(spool.AsJSONL(recs))))
	}
	return turnev.PhaseFromFile(AgentTranscriptPath(d, roots))
}

// ReadSessionEvidence asks whether durable transcript evidence exists for the
// session id the registry holds RIGHT NOW — the question the launch-time
// promotion of Materialized asked once and never asked again.
//
// It goes through claudia.SessionExists rather than reproducing the encoded-cwd
// path convention, because that convention is Claude Code's and claudia is
// where this repository tracks it. Non-Claude providers have no durable
// transcript this daemon knows how to locate; they materialize by host
// attestation, so their evidence is unknown here rather than absent.
func ReadSessionEvidence(provider claudia.Provider, sessionID, workDir string) SessionEvidence {
	if sessionID == "" || workDir == "" {
		return SessionEvidenceUnknown
	}
	if seatprovider.Resolve(provider).Sidecar {
		// Sidecar seats have no Claude JSONL. Absence of that file is
		// not a dead conversation and is not idle (🎯T866.3).
		return SessionEvidenceUnknown
	}
	// Every named plan is a sidecar seat since claudia v0.58 (a legacy row
	// storing claude is the anthropic seat, 🎯T1079); only an unset provider
	// still takes Claude Code's JSONL convention.
	if seatprovider.Resolve(provider).Plan != seatprovider.Unset {
		return SessionEvidenceUnknown
	}
	ok, err := claudia.SessionExists(sessionID, workDir)
	switch {
	case err != nil:
		return SessionEvidenceUnknown
	case ok:
		return SessionEvidencePresent
	default:
		return SessionEvidenceAbsent
	}
}

// classifyAgentListPhase is the agent_list phase column: the 🎯T305 answer, plus
// what the agent's own session records say when that answer would otherwise be
// never_briefed.
//
// It composes ClassifyAgentListStatus rather than restating it — the process
// and registry inputs still decide every case they can decide, and evidence is
// consulted only at the one point where the old derivation had run out of
// things it actually knew and asserted anyway.
func classifyAgentListPhase(alive, turnBegan, materialized bool, ev SessionEvidence) string {
	status := ClassifyAgentListStatus(alive, turnBegan, materialized)
	switch status {
	case AgentStatusNeverBriefed:
		switch ev {
		case SessionEvidencePresent:
			return AgentStatusRunning
		case SessionEvidenceAbsent:
			return AgentStatusNeverBriefed
		default:
			return AgentStatusPhaseUnknown
		}
	case AgentStatusRunning:
		// 🎯T412: "running" resting on the durable Materialized flag alone,
		// over a session whose records were located and are absent, is a dead
		// seat — the flag outlived (or preceded) the conversation it claims.
		// A confirmed in-process turn (turnBegan) keeps running: a session
		// minted this instant legitimately has no JSONL yet. Evidence
		// Unknown also keeps running — a failure to observe never
		// manufactures a death (🎯T422 clause 5).
		if !turnBegan && ev == SessionEvidenceAbsent {
			return AgentStatusDeadUnmaterialized
		}
	}
	return status
}

// classifySeatListPhase is classifyAgentListPhase for a registered seat: a
// seat the owner addresses (the overseer, an aside) has no brief to miss.
//
// never_briefed is a work seat's alarm (🎯T305): it was minted with a brief
// and no turn has begun. phase_unknown is the same seat when nothing can
// say whether that brief landed. Neither question applies to a seat that
// waits for the owner's next message: alive and not yet addressed, it is
// running and idle. On a fresh daemon the 30-second seat check could see a
// brand-new overseer alive before any turn (sidecar seats keep no record
// this daemon can call absent, so evidence is unknown), record
// phase_unknown, and /api/agents served that ahead of its alive → running
// fallback (🎯T1078). Liveness still decides — a stopped overseer is
// stopped — and so does located-absent evidence under a materialized row
// (🎯T412 dead_unmaterialized), which is a real finding for any seat.
func classifySeatListPhase(purpose string, alive, turnBegan, materialized bool, ev SessionEvidence) string {
	status := classifyAgentListPhase(alive, turnBegan, materialized, ev)
	if purpose != claudia.PurposeOverseer && purpose != claudia.PurposeAside {
		return status
	}
	switch status {
	case AgentStatusNeverBriefed, AgentStatusPhaseUnknown:
		return AgentStatusRunning
	}
	return status
}

// ClassifyAgentListStatus decides stopped | never_briefed | running.
// turnBegan is process-local evidence (successful start prompt or send).
// materialized is durable conversation evidence (registry Materialized /
// session JSONL). Either counts as "has been briefed".
func ClassifyAgentListStatus(alive, turnBegan, materialized bool) string {
	if !alive {
		return AgentStatusStopped
	}
	if turnBegan || materialized {
		return AgentStatusRunning
	}
	return AgentStatusNeverBriefed
}

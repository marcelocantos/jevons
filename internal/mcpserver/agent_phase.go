// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/fleet"
)

// 🎯T444 — A PHASE NOBODY LOOKED AT IS NOT "NEVER BRIEFED".
//
// On 2026-08-15 a fleet-wide backend re-mint rotated every agent's session id,
// and `jevons_agent_list` answered `never_briefed` for agents that were in the
// middle of a turn. bs-t68-reachability-oracle was one: 287 KB of transcript
// under its new session id, assistant events seconds old, last line "Re-running
// the gates on the final tree before committing". jv-t405-daemon-supervised and
// cl-t29-stability-snapshot read the same way in the same window. Measured on
// the live registry while this was written: THIRTY of forty-eight rows claimed
// never_briefed over a session file between 30 KB and 540 KB.
//
// The mechanism is that both inputs to the old answer go stale across a
// re-mint, and neither of them is about the session the agent is actually in:
//
//   - agentTurnBegan is process-local to THIS daemon. A restart empties it, and
//     clearAgentTurnBegan drops it whenever a seat is torn down and re-minted.
//   - AgentDef.Materialized is durable, but claudia promotes it only inside
//     Launch, against the session id as it stood at launch. A session minted at
//     that moment has no JSONL yet, so the flag stays false — and nothing ever
//     asks again. The transcript that appears one turn later is never read.
//
// So the row was derived entirely from state that a rotation invalidates, and
// derived a POSITIVE CLAIM from it. That is what makes it expensive rather than
// merely wrong. `never_briefed` is not a neutral unknown: it is the one label
// that tells the reader to brief the agent, and briefing an agent that is
// mid-turn stacks a duplicate onto a live composer — the 🎯T416 hazard. The
// overseer reads this list to decide who needs attention, so the lie lands
// exactly where acting on it costs the most. bullseye-po avoided it only by
// hand-reading two children's transcripts instead of trusting the row.
//
// THE FIX IS TO LOOK, AND TO SAY SO WHEN LOOKING WAS NOT POSSIBLE. The agent's
// durable session records are the one input a re-mint does not invalidate,
// because they are keyed by the session id the registry currently holds. Where
// they exist, the phase carries across the rotation and the row says running.
// Where they are located and genuinely absent, `never_briefed` is an
// observation and keeps its meaning (🎯T305 / 🎯T387 still have their signal).
// Where the records could not be located at all, the row says `phase_unknown`,
// which is 🎯T422 clause 5 wearing a different hat: a failure to observe cannot
// distinguish "not there" from "not there yet", and reporting the second as the
// first manufactures an absence out of one's own failure to look.
//
// WHAT THIS DELIBERATELY DOES NOT DO: promote AgentDef.Materialized from the
// evidence it reads. claudia hands Materialized straight to RequireResume on
// the next Launch, so writing the flag here would change how a seat is
// relaunched as a side effect of rendering a list. The derivation is the thing
// that was wrong; the launch contract is claudia's and is left alone.

// AgentStatusPhaseUnknown is the row for a live seat whose session records
// could not be read. Distinct from never_briefed on purpose: it is visibly not
// a claim about what the agent has received, so it invites a look rather than
// a re-brief.
const AgentStatusPhaseUnknown = "phase_unknown"

// AgentStatusDeadUnmaterialized is the row for a live seat whose registry
// claims a conversation that is not on disk (🎯T412): Materialized=true — the
// durable flag that alone makes T305 answer "running" — over a session id
// with no transcript, and no confirmed turn in this daemon to back it up.
//
// On 2026-08-10 six such seats read "running" with phase=idle while none of
// their session ids had a JSONL anywhere: the 🎯T409 recovery had minted
// replacement sessions that never materialized, the impatience ladder
// reported actions=7 errors=0 while nudging the void, and the sentinel
// prescribed the same repair on every tick forever. "Running" must mean a
// live conversation exists; this label is the honest answer when it does not.
const AgentStatusDeadUnmaterialized = fleet.StatusDeadUnmaterialized

// SessionEvidence is what an agent's durable session records say about whether
// its CURRENT session has ever hosted a conversation. Three answers, because
// "I read the records and there is no session there" and "I could not read the
// records" are different findings and the caller acts differently on each.
type SessionEvidence int

const (
	// SessionEvidenceUnknown: nothing was read. No session id, no workdir, a
	// provider whose durable transcripts this daemon cannot locate, or a stat
	// that failed for a reason other than the file being absent.
	SessionEvidenceUnknown SessionEvidence = iota
	// SessionEvidenceAbsent: the records were located and there are none. For
	// Claude this is claudia's own conversation evidence coming back negative —
	// a session that starts without a first turn writes no JSONL.
	SessionEvidenceAbsent
	// SessionEvidencePresent: durable session records exist for this session id,
	// so this seat has hosted a conversation under it.
	SessionEvidencePresent
)

func (e SessionEvidence) String() string {
	switch e {
	case SessionEvidenceAbsent:
		return "absent"
	case SessionEvidencePresent:
		return "present"
	default:
		return "unknown"
	}
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
	if provider != "" && provider != claudia.ProviderClaude {
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

// ClassifyAgentPhase is the agent_list phase column: the 🎯T305 answer, plus
// what the agent's own session records say when that answer would otherwise be
// never_briefed.
//
// It composes ClassifyAgentListStatus rather than restating it — the process
// and registry inputs still decide every case they can decide, and evidence is
// consulted only at the one point where the old derivation had run out of
// things it actually knew and asserted anyway.
func ClassifyAgentPhase(alive, turnBegan, materialized bool, ev SessionEvidence) string {
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

// agentPhase derives the phase column for one registry row.
func (s *Server) agentPhase(d claudia.AgentDef, alive bool) string {
	return ClassifyAgentPhase(alive, s.agentHasTurnBegan(d.Name), d.Materialized,
		ReadSessionEvidence(d.Provider, d.SessionID, d.WorkDir))
}

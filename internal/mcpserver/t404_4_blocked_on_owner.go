// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"

	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/ownerquestion"
	"github.com/marcelocantos/jevons/internal/ownerquestionview"
)

// AgentStatusBlockedOnOwner is the jevons_agent_list / cockpit status for a
// seat whose latest stored report is an owner-decision block citing a
// question that is still open (🎯T404.4). It is distinct from idle (the
// seat is not merely quiet — it is waiting on a named decision) and from an
// ordinary external block (🎯T938's generic "blocked on <text>", which names
// no question and nothing the owner specifically must answer).
//
// 🎯T938 already holds pressure off a blocked seat; this is that state's
// owner-facing legibility: jevons_agent_list and the cockpit fleet panel
// name the open question id and its silence_outcome instead of leaving the
// seat looking merely stopped/idle. The reuse of the IdleSkipBlockedOnOwner
// string as the status text is deliberate — one name for one state.
const AgentStatusBlockedOnOwner = IdleSkipBlockedOnOwner

// ownerDecisionBlockFor inspects name's latest stored report. When it is an
// unlifted blocked finish-report with block-class owner-decision, and the
// question it cites is still open in the durable cross-repo index, it
// returns that question's identity and silence outcome. ok is false
// otherwise — including once the cited question moves off open (answered,
// superseded or retired as moot), so the mark clears on the next list call
// without anyone restarting the seat or it storing a newer report.
func (s *Server) ownerDecisionBlockFor(name string) (id ownerquestionview.Identity, silenceOutcome string, ok bool) {
	if s == nil {
		return ownerquestionview.Identity{}, "", false
	}
	report := s.latestStoredReport(name)
	if strings.TrimSpace(report) == "" {
		return ownerquestionview.Identity{}, "", false
	}
	if s.seatBlockerCleared(name, report) {
		return ownerquestionview.Identity{}, "", false
	}
	m, _ := envelope.Parse(report)
	if m == nil || m.Kind != envelope.KindFinishReport || !m.IsBlocked() || m.BlockClass != envelope.BlockOwnerDecision {
		return ownerquestionview.Identity{}, "", false
	}
	repo := strings.TrimSpace(m.QuestionRepo)
	// 🎯T404.4: the intake canonicalizes QuestionRepo before storing
	// (ownerquestion.CanonicalRepo — git root, symlinks resolved), so the
	// lookup key must match that canonical form or every Find misses and
	// the block never clears. Canonicalization failure (repo gone, not a
	// directory) falls back to the raw slot: still enough to display.
	if canon, err := ownerquestion.CanonicalRepo(repo); err == nil {
		repo = canon
	}
	qid := ownerquestionview.Identity{
		Repo:    repo,
		Target:  strings.TrimSpace(m.Target),
		ID:      strings.TrimSpace(m.QuestionID),
		Version: strings.TrimSpace(m.QuestionVersion),
	}
	outcome := ownerquestion.Question{Default: m.QuestionDefault}.SilenceOutcome()
	dir := strings.TrimSpace(s.ownerQuestionsDir)
	if dir == "" {
		// No durable index wired: trust the envelope's own declaration
		// rather than hiding the block behind a dependency it cannot see.
		return qid, outcome, true
	}
	q, err := ownerquestionview.New(dir).Find(qid)
	if err != nil {
		// The intake records every valid owner-decision block (🎯T404), so
		// a lookup miss here means the report predates that store (or the
		// store was reset) — the envelope still names a live block.
		return qid, outcome, true
	}
	if q.State != ownerquestionview.Open {
		return ownerquestionview.Identity{}, "", false
	}
	// Prefer the store's own silence outcome: it carries whatever default
	// the intake actually recorded, which is the same value an answered
	// question would have been judged against.
	return qid, q.SilenceOutcome(), true
}

// FormatOwnerDecisionBlockLine renders the annotation line for a seat
// blocked on an open owner-decision question (🎯T404.4).
func FormatOwnerDecisionBlockLine(id ownerquestionview.Identity, silenceOutcome string) string {
	return fmt.Sprintf("blocked_on_owner: question %s/%s#%s@%s — %s",
		id.Repo, id.Target, id.ID, id.Version, silenceOutcome)
}

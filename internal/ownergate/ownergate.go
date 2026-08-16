// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package ownergate is the ledger vocabulary for one state the intent ledger
// could not previously express: the code has landed and gated, and the only
// residue is the owner's taste verdict (🎯T449).
//
// WHY A STATE AND NOT A HEURISTIC. 🎯T383 was built, gated and escalated, and
// its ledger row still read `status: identified` with no record of the
// landing. Frontier-consume (🎯T254.1) read that row correctly and auto-spawned
// an implementer against finished work — twice in one session. The sweep was
// not wrong; the ledger simply could not say what was true. So the fix is a
// recorded state, written by the accepting PO at the moment it accepts the
// gated report, and read verbatim by every reader. Nothing here infers the
// state from prose about how done something looks: an inferred state is the
// same guess the target exists to eliminate.
//
// WHERE IT LIVES. bullseye's `owned_by` (owner + reason) already means "this
// is driven by someone else; status stays active, dependents stay blocked,
// it leaves this owner's frontier". Assigning to the owner with a structured
// reason is exactly the claim, needs no schema change, and is what the
// overseer reached for by hand when it hit this the first time. This package
// writes and reads that reason; internal/poproactive classifies on it and
// internal/mcpserver parks the sweep against it.
package ownergate

import (
	"fmt"
	"strings"
	"time"
)

const (
	// OwnerHandle is the `owned_by.owner` written when the ball is in the
	// owner's court. bullseye takes a free-text handle; this is the one the
	// product writes so a reader never has to guess whether "mc" is a person.
	OwnerHandle = "owner"

	// MarkerAwaiting opens the assignment reason. Upper-case and unpunctuated
	// so it survives being pasted into chat, a commit message, or a context
	// block, and so a human scanning `bullseye_get` sees the state on line 1.
	MarkerAwaiting = "BUILT, AWAITING OWNER VERDICT"

	// MarkerAnswered opens the line written when the owner has answered but
	// the assignment has not been unwound yet. Deliberately not a substring
	// of MarkerAwaiting: the two must never match each other's text.
	MarkerAnswered = "OWNER VERDICT ANSWERED"
)

// Verdict is the owner's answer to a taste gate.
type Verdict string

const (
	// VerdictAccept: the owner accepted; the target is achievable.
	VerdictAccept Verdict = "accept"
	// VerdictReject: the owner rejected; work resumes from the landed commit.
	VerdictReject Verdict = "reject"
)

// ParseVerdict accepts the spellings an agent or the owner is likely to type.
func ParseVerdict(s string) (Verdict, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "accept", "accepted", "yes", "ok", "approve", "approved":
		return VerdictAccept, nil
	case "reject", "rejected", "no", "deny", "denied":
		return VerdictReject, nil
	}
	return "", fmt.Errorf("verdict must be accept or reject, got %q", s)
}

// Record is one awaiting-owner-verdict claim, as made by the accepting PO.
type Record struct {
	// Question is the single thing the owner must judge — the class-3 taste
	// residue (🎯T31.2), phrased so the answer is accept or reject.
	Question string
	// Evidence is what makes the claim checkable: the landed commit SHA, the
	// GATE ids, the named oracles. A claim of "built" with nothing behind it
	// is the failure mode this whole state would otherwise create.
	Evidence string
	// RecordedBy is the agent making the claim (the accepting PO).
	RecordedBy string
	// Now stamps the record; zero → time.Now().
	Now time.Time
}

// minEvidenceLen is the floor below which evidence cannot name a commit, a
// gate id, or a test — "done" and "green" both fit under it.
const minEvidenceLen = 12

// Reason renders the `owned_by.reason` for a record, or refuses.
//
// The refusal is the load-bearing part. This state removes a target from
// unattended consumption, which is exactly what an agent under completion
// pressure would like to do to work it has not finished; so the state cannot
// be claimable without naming what landed. Evidence must carry a commit-shaped
// token, a gate id, or a test name — the same currency 🎯T31 accepts for
// "done" anywhere else.
func (r Record) Reason() (string, error) {
	q := strings.TrimSpace(r.Question)
	e := strings.TrimSpace(r.Evidence)
	if q == "" {
		return "", fmt.Errorf("question required: name the single thing the owner must accept or reject")
	}
	if !HasLandedEvidence(e) {
		return "", fmt.Errorf(
			"evidence required: name what landed — a commit SHA, a GATE id (🎯T386), or a named green oracle. " +
				"A target is only awaiting the owner once the code is in the tree; without that this state would hide unstarted work (🎯T31)")
	}
	by := strings.TrimSpace(r.RecordedBy)
	if by == "" {
		by = "unknown"
	}
	when := r.Now
	if when.IsZero() {
		when = time.Now()
	}
	return fmt.Sprintf("%s — recorded %s by %s (🎯T449). Owner gate: %s Evidence: %s "+
		"Do not spawn an implementer; unassign (verdict=reject) and resume from the landed commit if the owner says no.",
		MarkerAwaiting, when.UTC().Format("2006-01-02"), by, ensureSentence(q), ensureSentence(e)), nil
}

// FormatAnswer renders the line recording the owner's answer. The PO writes
// it when the owner has spoken but the target is not yet achieved or reopened
// — a leaf must not stay parked on a gate that has already been answered.
func FormatAnswer(v Verdict, note, by string, now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	if strings.TrimSpace(by) == "" {
		by = "unknown"
	}
	line := fmt.Sprintf("%s (%s) %s recorded by %s", MarkerAnswered, v,
		now.UTC().Format("2006-01-02"), strings.TrimSpace(by))
	if n := strings.TrimSpace(note); n != "" {
		line += ": " + n
	}
	return line
}

// HasLandedEvidence reports whether text names something a reader could go
// and check: a commit-shaped hex token, a gate attestation, or a test name.
func HasLandedEvidence(text string) bool {
	t := strings.TrimSpace(text)
	if len(t) < minEvidenceLen {
		return false
	}
	lower := strings.ToLower(t)
	if strings.Contains(lower, "gate ") || strings.Contains(lower, "id=") ||
		strings.Contains(lower, "test") || strings.Contains(lower, "commit") {
		return true
	}
	for _, tok := range strings.FieldsFunc(t, func(r rune) bool {
		return !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F'))
	}) {
		if len(tok) >= 7 && len(tok) <= 40 && hasDigit(tok) {
			return true
		}
	}
	return false
}

func hasDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

// IsOwnerHandle reports whether an `owned_by.owner` names the human owner
// rather than another agent or team. Case- and punctuation-insensitive over
// the handful of spellings the product and the owner actually use.
func IsOwnerHandle(owner string) bool {
	o := strings.ToLower(strings.Trim(strings.TrimSpace(owner), "@"))
	switch o {
	case OwnerHandle, "the owner", "human", "marcelo", "marcelocantos", "marcelo.cantos":
		return true
	}
	return false
}

// IsAnswered reports whether the record carries an answered marker. Checked
// before IsAwaiting everywhere: an answered gate is no longer a gate.
func IsAnswered(reason, context string) bool {
	return strings.Contains(strings.ToUpper(reason+" "+context), MarkerAnswered)
}

// IsAwaiting reports the awaiting-owner-verdict state for one target.
//
// True when the ledger says so, two ways: the assignment reason (or context)
// carries the awaiting marker, or the target is assigned to the owner at all
// — assigning work to the human IS the claim that the ball is in their court,
// and a reader that ignored it would re-create the bug for every assignment
// made by hand. Both are recorded states; neither is inferred from how
// finished the prose sounds.
func IsAwaiting(owner, reason, context string) bool {
	if IsAnswered(reason, context) {
		return false
	}
	if strings.Contains(strings.ToUpper(reason+" "+context), "AWAITING OWNER VERDICT") {
		return true
	}
	return IsOwnerHandle(owner)
}

// ensureSentence gives free text a terminator so the rendered reason reads as
// prose rather than as run-together fields.
func ensureSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	switch s[len(s)-1] {
	case '.', '!', '?':
		return s
	}
	return s + "."
}

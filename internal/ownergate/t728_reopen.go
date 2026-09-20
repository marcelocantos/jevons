// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ownergate

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Recording the gate on a row that is already achieved (🎯T728).
//
// THE TRAP, walked into on 2026-09-21. While `jevons_owner_gate` was broken
// (🎯T720), `jevons-po` had to close 🎯T711 with its class-3 owner residue
// written into the attestation as prose, because recording it as state was
// impossible. Once the tool worked, recording it was still impossible — the row
// was achieved by then, and bullseye refuses `owner` on a terminal status
// ("ownership exclusion only applies to active targets") and refuses content
// edits to an achieved row at all. So the outage made the very targets it
// damaged permanently unrecordable, and 🎯T711 reads as fully closed while an
// owner verdict is genuinely outstanding.
//
// WHY REOPEN IS THE ANSWER AND NOT A WORKAROUND. Achieved immutability is
// load-bearing: it is what makes the ledger a record rather than a scratchpad,
// and nothing here weakens it. The reopen path bullseye documents in that very
// refusal ("Reopen it in this same apply by setting `status: identified` with a
// `reason`, then patch") is the sanctioned way to change an achieved row, and
// for this state it is also the *honest* one. A 🎯T449 gate means the code
// landed and the owner's answer is the only residue — an active state, parked
// with the owner so frontier-consume does not spawn against finished work. A
// row that is achieved while an owner verdict is outstanding is exactly the
// misstatement 🎯T728 names. Reopening does not retract the work; it says the
// closure was premature, which it was.
//
// WHAT THE REOPEN COSTS, AND WHAT THIS FILE REFUSES TO LOSE. A status
// transition drops the fields the new status forbids (🎯T64): `attestation` and
// `achieved` both go. The date is residue — `bullseye apply` has no `achieved`
// key, so a re-achieve stamps the day it happens and the original survives only
// in the audit trail. The attestation is NOT allowed to be residue. It is the
// oracle evidence, and a ceremony that deleted the evidence on its way to
// recording a verdict would be strictly worse than the prose it replaced. So
// the attestation is carried forward in both texts this ceremony writes, and
// the accept answer puts it back verbatim.
//
// WHY IT IS CARRIED AND NOT RECOVERED. bullseye's own achieve appends
// "Achieved <date>: <attestation>" to `context`, so the text does survive a
// reopen and an earlier draft of this file read it back from there. That read
// is a paragraph-boundary guess: an attestation containing a blank line — every
// multi-paragraph attestation in this ledger — is silently truncated at the
// first one, and a truncated attestation passes every non-empty check while
// being exactly the loss this ceremony exists to prevent. What is written by
// this code is read back by this code: the attestation goes after
// MarkerPreserved at the end of a scalar YAML round-trips whole, and
// PreservedAttestation slices from the marker to the end with no boundary to
// guess at. Reading bullseye's paragraph remains as a last resort for a row
// reopened by some other hand, and says so.

const (
	// MarkerReopened opens the reason written when the ceremony reopens an
	// achieved row to hold a gate. Upper-case and unpunctuated for the same
	// reasons as MarkerAwaiting, and matched by IsReopenedGate on the way
	// back: the answer half must be able to tell a row it reopened from a row
	// that was active all along, because only the first has an achievement to
	// put back.
	MarkerReopened = "REOPENED TO RECORD OWNER GATE"

	// MarkerPreserved introduces an attestation carried across a reopen. It is
	// always last in the text that carries it, so recovering the attestation
	// is a slice to the end rather than a search for where it stops.
	MarkerPreserved = "PRESERVED ATTESTATION:"
)

// dateRe matches the ISO day stamps bullseye writes into its audit lines.
var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}`)

// achievedAuditRe matches the audit paragraph bullseye appends on achieve:
// "Achieved 2026-09-21: <attestation>".
var achievedAuditRe = regexp.MustCompile(`(?m)^Achieved \d{4}-\d{2}-\d{2}: `)

// Reopen is one achieved row about to be reopened to hold a gate.
type Reopen struct {
	// Record is the gate being recorded — same evidence contract as an
	// ordinary record; an achieved row gets no discount.
	Record Record
	// AchievedOn is the row's `achieved` date, read before the reopen clears
	// it. Empty is tolerated and says so in the text rather than inventing a
	// date.
	AchievedOn string
	// Attestation is the row's attestation, read before the reopen clears it.
	Attestation string
}

// Reason renders the `reason` for the reopening apply. bullseye appends it to
// context as "Reverted <today>: <reason>", so this text is the permanent audit
// record of why an achieved row was reopened — and the copy of the attestation
// that survives even if the assignment is later cleared by hand.
func (r Reopen) Reason() (string, error) {
	// The gate's own contract runs first: no evidence, no reopen. An achieved
	// row is the last place to relax it — reopening on an unevidenced claim
	// would turn this ceremony into a way to un-achieve other people's work.
	if _, err := r.Record.Reason(); err != nil {
		return "", err
	}
	when := r.Record.Now
	if when.IsZero() {
		when = time.Now()
	}
	by := strings.TrimSpace(r.Record.RecordedBy)
	if by == "" {
		by = "unknown"
	}
	text := fmt.Sprintf(
		"%s — %s recorded the 🎯T449 owner gate on %s against a row achieved %s (🎯T728). "+
			"The code landed and gated; the sole residue is the owner's taste verdict, which an achieved row cannot hold: "+
			"bullseye refuses `owner` on a terminal status and refuses content edits to an achieved row, so the reopen it "+
			"documents is the ceremony. This is not a retraction of the work — it records that the closure was premature "+
			"while the owner's answer is outstanding. Owner gate: %s Evidence: %s "+
			"On accept (`jevons_owner_gate op=answer verdict=accept`) the row is re-achieved with the attestation below "+
			"restored verbatim; on reject it stays active and work resumes from the landed commit.",
		MarkerReopened, by, when.UTC().Format("2006-01-02"), r.achievedPhrase(),
		ensureSentence(strings.TrimSpace(r.Record.Question)),
		ensureSentence(strings.TrimSpace(r.Record.Evidence)))
	return withPreserved(text, r.Attestation), nil
}

// GateReason renders the `owned_by.reason` for a gate recorded on a reopened
// row: the ordinary awaiting-verdict claim, what a later reader must know that
// an always-active row would not make them ask, and the attestation.
//
// This is the copy the answer half reads. It is a single YAML scalar written
// and read by this package, which is what makes the round trip exact.
func (r Reopen) GateReason() (string, error) {
	base, err := r.Record.Reason()
	if err != nil {
		return "", err
	}
	text := fmt.Sprintf("%s %s — this row was achieved %s and was reopened to hold this gate (🎯T728). "+
		"Answer it with `jevons_owner_gate op=answer`: accept re-achieves the row and restores the attestation carried "+
		"below; reject leaves it active and work resumes from the landed commit. This is not unstarted work, and it must "+
		"not be hand-achieved while the gate is open.",
		base, MarkerReopened, r.achievedPhrase())
	return withPreserved(text, r.Attestation), nil
}

// achievedPhrase names the date the row had been achieved, or says it is
// unknown rather than inventing one.
func (r Reopen) achievedPhrase() string {
	if d := strings.TrimSpace(r.AchievedOn); d != "" {
		return d
	}
	return "on an unrecorded date"
}

// withPreserved appends the attestation to a text, last, under the marker.
func withPreserved(text, attestation string) string {
	att := strings.TrimSpace(attestation)
	if att == "" {
		return text
	}
	return text + " " + MarkerPreserved + " " + att
}

// IsReopenedGate reports whether an assignment reason was written by the reopen
// ceremony — that is, whether answering it has an achievement to put back.
// Read before the answer half decides what accept means.
func IsReopenedGate(reason string) bool {
	return strings.Contains(strings.ToUpper(reason), MarkerReopened)
}

// ReopenedAchievedDate recovers the date the row had been achieved from a
// reopened gate's reason. Empty when the reason does not carry one, which the
// callers render as unrecorded rather than guessing.
func ReopenedAchievedDate(reason string) string {
	const needle = "ACHIEVED "
	upper := strings.ToUpper(reason)
	for i := strings.Index(upper, needle); i >= 0; {
		rest := strings.TrimLeft(reason[i+len(needle):], " ")
		if d := dateRe.FindString(rest); d != "" {
			return d
		}
		next := strings.Index(upper[i+1:], needle)
		if next < 0 {
			break
		}
		i += 1 + next
	}
	return ""
}

// PreservedAttestation recovers the attestation a reopen dropped.
//
// Pass it the text this package wrote — a reopened gate's `owned_by.reason`, or
// the reopen audit in `context`. The attestation is everything after the last
// MarkerPreserved, so a multi-paragraph attestation comes back whole; there is
// no boundary to infer, which is the entire reason it is written that way.
//
// The fallback reads bullseye's own "Achieved <date>: …" audit paragraph, for a
// row reopened by some other hand than this ceremony. That read stops at the
// first blank line and so can truncate; it is a last resort ahead of returning
// nothing, and callers say which one they got.
func PreservedAttestation(text string) string {
	if i := strings.LastIndex(text, MarkerPreserved); i >= 0 {
		return strings.TrimSpace(text[i+len(MarkerPreserved):])
	}
	return ""
}

// AttestationFromAchieveAudit is the truncating last resort described on
// PreservedAttestation, kept separate so no caller reaches it by accident.
func AttestationFromAchieveAudit(context string) string {
	locs := achievedAuditRe.FindAllStringIndex(context, -1)
	if len(locs) == 0 {
		return ""
	}
	last := locs[len(locs)-1]
	body := context[last[1]:]
	if end := strings.Index(body, "\n\n"); end >= 0 {
		body = body[:end]
	}
	return strings.TrimSpace(body)
}

// RestoreAttestation renders the `attestation` for re-achieving a row the
// ceremony reopened, once the owner has accepted.
//
// It states the two facts a reader of the re-achieved row would otherwise have
// to reconstruct from git: that the owner answered, and that the `achieved`
// date is the day of the answer rather than the day the work landed, because
// `bullseye apply` has no key for that field. The original attestation is
// reproduced rather than referenced — it is the row's evidence, and a pointer
// into an audit paragraph is the sort of indirection that decays.
func RestoreAttestation(v Verdict, note, by, achievedOn, preserved string, now time.Time) string {
	if now.IsZero() {
		now = time.Now()
	}
	was := "on an unrecorded date"
	if d := strings.TrimSpace(achievedOn); d != "" {
		was = d
	}
	text := fmt.Sprintf("%s — the 🎯T449 owner gate on this row was answered %s, so the row is re-achieved (🎯T728). "+
		"It had been achieved %s and was reopened to hold the gate, because an achieved row can hold neither an owner "+
		"assignment nor a content edit. The `achieved` date on this row is the day the owner answered, not the day the "+
		"work landed: `bullseye apply` has no `achieved` key, so the original date survives in this text and in the "+
		"context audit only (declared residue, 🎯T728). The attestation of that original achieve follows verbatim.",
		FormatAnswer(v, note, by, now), now.UTC().Format("2006-01-02"), was)
	if p := strings.TrimSpace(preserved); p != "" {
		return withPreserved(text, p)
	}
	return text + " The original attestation could not be recovered from the row; read it from the git history of the ledger."
}

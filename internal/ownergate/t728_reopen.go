// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ownergate

import (
	"fmt"
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
// this code is read back by this code: MarkerPreserved opens its own line and
// the attestation is every line after that line, so a multi-paragraph
// attestation comes back whole. A mid-line copy of the marker words is not
// the slot. Reading bullseye's paragraph remains as a last resort for a row
// reopened by some other hand, and says so.

const (
	// MarkerReopened opens the reason written when the ceremony reopens an
	// achieved row to hold a gate. Upper-case and unpunctuated for the same
	// reasons as MarkerAwaiting, and matched by IsReopenedGate on the way
	// back: the answer half must be able to tell a row it reopened from a row
	// that was active all along, because only the first has an achievement to
	// put back.
	MarkerReopened = "REOPENED TO RECORD OWNER GATE"

	// MarkerPreserved opens its own line. The attestation is the text on the
	// following lines. A mid-line copy of these words is not the slot.
	MarkerPreserved = "PRESERVED ATTESTATION:"
)

// isoDate reports a YYYY-MM-DD token. The shape is the date, not a search
// for the word that introduces it.
func isoDate(s string) bool {
	if len(s) != len("2006-01-02") {
		return false
	}
	for i, c := range s {
		switch i {
		case 4, 7:
			if c != '-' {
				return false
			}
		default:
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

// achievedField is the declared date slot on a reopen line. Empty when the
// row had no recorded achieved date.
func (r Reopen) achievedField() string {
	d := strings.TrimSpace(r.AchievedOn)
	if isoDate(d) {
		return "achieved=" + d
	}
	return "achieved="
}

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
		"%s %s — %s recorded the 🎯T449 owner gate on %s against a row achieved %s (🎯T728). "+
			"The code landed and gated; the sole residue is the owner's taste verdict, which an achieved row cannot hold: "+
			"bullseye refuses `owner` on a terminal status and refuses content edits to an achieved row, so the reopen it "+
			"documents is the ceremony. This is not a retraction of the work — it records that the closure was premature "+
			"while the owner's answer is outstanding. Owner gate: %s Evidence: %s "+
			"On accept (`jevons_owner_gate op=answer verdict=accept`) the row is re-achieved with the attestation below "+
			"restored verbatim; on reject it stays active and work resumes from the landed commit.",
		MarkerReopened, r.achievedField(), by, LedgerDay(when), r.achievedPhrase(),
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
	text := fmt.Sprintf("%s\n%s %s — this row was achieved %s and was reopened to hold this gate (🎯T728). "+
		"Answer it with `jevons_owner_gate op=answer`: accept re-achieves the row and restores the attestation carried "+
		"below; reject leaves it active and work resumes from the landed commit. This is not unstarted work, and it must "+
		"not be hand-achieved while the gate is open.",
		base, MarkerReopened, r.achievedField(), r.achievedPhrase())
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

// withPreserved appends the attestation on the lines after a line that opens
// with MarkerPreserved. The marker is not written into the middle of a sentence.
func withPreserved(text, attestation string) string {
	att := strings.TrimSpace(attestation)
	if att == "" {
		return text
	}
	return text + "\n" + MarkerPreserved + "\n" + att
}

// IsReopenedGate reports whether an assignment reason was written by the reopen
// ceremony — that is, whether answering it has an achievement to put back.
// Read before the answer half decides what accept means. The marker opens a
// line. A sentence that uses the same words later is not the ceremony.
func IsReopenedGate(reason string) bool {
	return opensWithMarker(reason, MarkerReopened)
}

// ReopenedAchievedDate recovers the declared achieved= date from a line that
// opens with the reopen marker. Empty when that slot is absent or not a date.
// Prose that says the row was achieved is not the slot.
func ReopenedAchievedDate(reason string) string {
	rest, ok := markerRemainder(reason, MarkerReopened)
	if !ok {
		return ""
	}
	for _, field := range strings.Fields(rest) {
		val, found := strings.CutPrefix(field, "ACHIEVED=")
		if !found {
			continue
		}
		if isoDate(val) {
			return val
		}
		return ""
	}
	return ""
}

// PreservedAttestation recovers the attestation a reopen dropped.
//
// Pass it the text this package wrote — a reopened gate's `owned_by.reason`, or
// the reopen audit in `context`. The attestation is every line after the last
// line that opens with MarkerPreserved, so a multi-paragraph attestation comes
// back whole. A mid-line copy of the marker words is not the slot, and a
// historical row that wrote the marker inline returns empty.
// AttestationFromAchieveAudit is the truncating last resort for a row reopened
// by some other hand; callers say which one they got.
func PreservedAttestation(text string) string {
	return textAfterOpeningMarker(text, MarkerPreserved)
}

// textAfterOpeningMarker returns the document after the last line that opens
// with marker. The break rule matches markerRemainder: the marker may stand
// alone or be followed by space, tab, or one of "(:.,". A copy of the marker
// later in a line is not that line. markerRemainder itself stays first-match
// and returns only the remainder of the matching line.
func textAfterOpeningMarker(text, marker string) string {
	if marker == "" {
		return ""
	}
	want := strings.ToUpper(marker)
	lines := strings.Split(text, "\n")
	at := -1
	for i, line := range lines {
		trimmed := strings.ToUpper(strings.TrimSpace(line))
		rest, ok := strings.CutPrefix(trimmed, want)
		if !ok {
			continue
		}
		if rest == "" {
			at = i
			continue
		}
		switch rest[0] {
		case ' ', '\t', '(', ':', '.', ',':
			at = i
		}
	}
	if at < 0 || at+1 >= len(lines) {
		return ""
	}
	return strings.TrimSpace(strings.Join(lines[at+1:], "\n"))
}

// splitAchievedStamp reports the body after a line that opens with the
// declared stamp: the word Achieved and a YYYY-MM-DD date. "Achieved by …"
// is not a stamp.
func splitAchievedStamp(trim string) (string, bool) {
	fields := strings.Fields(trim)
	if len(fields) < 2 || fields[0] != "Achieved" {
		return "", false
	}
	date := strings.TrimSuffix(fields[1], ":")
	if !isoDate(date) {
		return "", false
	}
	idx := strings.Index(trim, date)
	if idx < 0 {
		return "", false
	}
	rest := strings.TrimSpace(trim[idx+len(date):])
	rest = strings.TrimPrefix(rest, ":")
	return strings.TrimSpace(rest), true
}

// AttestationFromAchieveAudit is the truncating last resort described on
// PreservedAttestation, kept separate so no caller reaches it by accident.
func AttestationFromAchieveAudit(context string) string {
	var body string
	found := false
	for _, line := range strings.Split(context, "\n") {
		if rest, ok := splitAchievedStamp(strings.TrimSpace(line)); ok {
			found = true
			body = rest
			continue
		}
		if !found {
			continue
		}
		if strings.TrimSpace(line) == "" {
			break
		}
		if body != "" {
			body += "\n"
		}
		body += line
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
		FormatAnswer(v, note, by, now), LedgerDay(now), was)
	if p := strings.TrimSpace(preserved); p != "" {
		return withPreserved(text, p)
	}
	return text + " The original attestation could not be recovered from the row; read it from the git history of the ledger."
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

// DoneClass is the typed answer to "is this report done?" (🎯T766.4).
//
// One answer serves the finished-work reap path and supervision. Prose is
// not a fallback: a report that declares nothing is Unknown, not Yes.
type DoneClass int

const (
	// DoneUnknown: no envelope, or a fence that did not validate as a
	// terminal claim. Never treated as finished.
	DoneUnknown DoneClass = iota
	// DoneNo: a well-formed envelope that is explicitly not product-done
	// (scout, status-ping, blocked finish-report, in-progress mid-work).
	DoneNo
	// DoneYes: a valid finish-report that claims completion (oracle or
	// accepted-risk) and does not declare blocked or in-progress.
	DoneYes
)

// String returns a stable token for logs and probes.
func (c DoneClass) String() string {
	switch c {
	case DoneYes:
		return "yes"
	case DoneNo:
		return "no"
	default:
		return "unknown"
	}
}

// IsDone is true only for DoneYes.
func (c DoneClass) IsDone() bool { return c == DoneYes }

// DoneShape is the smallest finish-report ClassifyDone answers DoneYes for
// (🎯T1083). It parses as written: the placeholders are non-empty slot
// values, and a ratchet holds it to DoneYes. Instruction files that show a
// done finish-report are held to the same classification.
//
// It has no status slot. Every status value (ProgressValues) either says
// the seat is not done (in-progress, blocked) or claims owner-visible
// product evidence (live, landed, shipped — 🎯T176), so a finished report
// with nothing owner-visible to claim, a read-only survey above all, omits
// the slot. That is the shape no instruction showed before 🎯T1083: two
// read-only pilot workers on 2026-10-20 wrote `status done`, then
// `status achieved`, each read as unknown, and their parent reaped them by
// hand.
const DoneShape = "```jevons\n" +
	"jevons: kind finish-report\n" +
	"jevons: target T<n>\n" +
	"jevons: oracle sha=<head-sha>\n" +
	"jevons: silent-ledger none\n" +
	"```"

// DoneShapeNote is the guidance that travels with DoneShape.
const DoneShapeNote = "Omit status: done is not a status value, and in-progress and blocked mean not done. " +
	"The oracle is the commit that lands the work — or, for a read-only task, the HEAD you measured " +
	"(git rev-parse HEAD). Add gate-id <id> and verdict GREEN when you cite a bin/gate run, or " +
	"risk class-3 instead of an oracle for accepted risk; use silent-ledger ranked when the brief was " +
	"silent on a material choice."

// ClassifyDone is the single field-read answer to is-this-report-done
// (🎯T766.4). It does not scan English for a completion word.
//
// Rules:
//   - No fence / Parse returns nil → DoneUnknown.
//   - KindFinishReport carrying an active-wait slot → DoneUnknown. The
//     report claims a finish and declares an alive external wait in the
//     same breath; it is contradictory, so it is never Done (🎯T1024,
//     typed by 🎯T766.4.1). Readers that must decide reap vs park read
//     the slot itself (ActiveWaitIn) and park.
//   - KindFinishReport with status blocked or in-progress → DoneNo, even
//     when another required slot is missing. The status slot is the claim.
//   - KindFinishReport that fails validation, or that carries neither oracle
//     nor accepted-risk → DoneUnknown. Evidence validation is not inferred
//     from the payload.
//   - KindFinishReport valid, with oracle or risk, not blocked or
//     in-progress → DoneYes.
//   - KindScoutReport, status-ping, ack, spawn-brief, escalation, and
//     target-file-request, when valid → DoneNo.
//   - Any other kind, or a malformed non-finish fence → DoneUnknown.
func ClassifyDone(text string) DoneClass {
	m, err := Parse(text)
	if m == nil {
		return DoneUnknown
	}
	switch m.Kind {
	case KindFinishReport:
		if m.HasActiveWait() {
			return DoneUnknown
		}
		if m.Status == ProgressBlocked || m.IsBlocked() {
			return DoneNo
		}
		if m.Status == ProgressInProgress {
			return DoneNo
		}
		if err != nil || (!m.HasOracle() && !m.HasRisk()) {
			return DoneUnknown
		}
		return DoneYes
	case KindScoutReport, KindStatusPing, KindAck, KindSpawnBrief,
		KindEscalation, KindTargetFileRequest:
		if err != nil {
			return DoneUnknown
		}
		return DoneNo
	default:
		return DoneUnknown
	}
}

// IsDoneReport is ClassifyDone(text).IsDone(). Reap and supervision both
// call this (🎯T766.4).
func IsDoneReport(text string) bool {
	return ClassifyDone(text).IsDone()
}

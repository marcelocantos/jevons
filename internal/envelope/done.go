// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

// DoneClass is the typed answer to "is this report done?" (🎯T766.4).
//
// One answer serves both the finished-work reap path and supervision
// routing (relayroute). Prose heuristics are not a fallback: a report that
// declares nothing is Unknown, not Yes.
type DoneClass int

const (
	// DoneUnknown: no envelope, a malformed load-bearing fence, or a kind
	// that does not claim terminal product work. Never treated as finished.
	DoneUnknown DoneClass = iota
	// DoneNo: a well-formed envelope that is explicitly not product-done
	// (scout, status-ping, blocked finish-report, in-progress mid-work).
	DoneNo
	// DoneYes: a valid finish-report that claims completion (oracle or
	// accepted-risk) and does not declare mid-work remaining scope.
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

// ClassifyDone is the single field-read answer to is-this-report-done
// (🎯T766.4 acceptance 2–3). Controls that used to decide by English
// completion words call this instead.
//
// Rules:
//   - No fence / Parse returns nil → DoneUnknown.
//   - Malformed load-bearing finish-report → DoneUnknown (never finish on a
//     fence the daemon could not validate).
//   - KindFinishReport with status blocked or in-progress → DoneNo.
//   - KindFinishReport valid with oracle or risk, not mid-work → DoneYes.
//   - KindScoutReport / status-ping / ack / spawn-brief / escalation /
//     target-file-request → DoneNo (never product-done).
//   - Any other kind → DoneUnknown.
//
// Remaining-work named only in free prose of a finish-report that otherwise
// claims completion is a residual: callers that still need T784 mid-work
// payload vetoes compose MidWorkRemaining on top of this class. The class
// itself does not scan English for "done".
func ClassifyDone(text string) DoneClass {
	m, err := Parse(text)
	if m == nil {
		return DoneUnknown
	}
	switch m.Kind {
	case KindFinishReport:
		if err != nil {
			return DoneUnknown
		}
		if m.Status == ProgressBlocked || m.IsBlocked() {
			return DoneNo
		}
		if m.Status == ProgressInProgress {
			return DoneNo
		}
		if !m.HasOracle() && !m.HasRisk() {
			// Validate would have failed; treat as unknown rather than a
			// finish claim with no evidence.
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

// IsDoneReport is ClassifyDone(text).IsDone() — the shared predicate for
// reap and supervision (🎯T766.4 acceptance 3).
func IsDoneReport(text string) bool {
	return ClassifyDone(text).IsDone()
}

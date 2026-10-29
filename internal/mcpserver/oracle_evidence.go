// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/gate"
)

// CompletionEvidenceClass is a hermetic classification of a finish report
// for oracle-first enforcement (🎯T31 / 🎯T31.1). Heuristic only — not a
// full NLP judge; overseer judgment still applies.
type CompletionEvidenceClass int

const (
	// CompletionBareDone: claim language without oracle or accepted-risk.
	CompletionBareDone CompletionEvidenceClass = iota
	// CompletionOracleEvidence: named tests/green/SHA-style oracle markers.
	CompletionOracleEvidence
	// CompletionAcceptedRisk: explicit accepted-risk / isolated class-3.
	CompletionAcceptedRisk
	// CompletionNoClaim: no completion claim and no evidence (neutral).
	CompletionNoClaim
	// CompletionMissingSilentLedger: finish-report has oracle/risk but no
	// explicit silent-decision ledger (🎯T536.1) — flagged, not complete.
	CompletionMissingSilentLedger
)

func (c CompletionEvidenceClass) String() string {
	switch c {
	case CompletionBareDone:
		return "bare_done"
	case CompletionOracleEvidence:
		return "oracle_evidence"
	case CompletionAcceptedRisk:
		return "accepted_risk"
	case CompletionNoClaim:
		return "no_claim"
	case CompletionMissingSilentLedger:
		return "missing_silent_ledger"
	default:
		return "unknown"
	}
}

var completionClaimMarkers = []string{
	"done",
	"complete",
	"completed",
	"finished",
	"achieved",
	"shipped",
	"ready to merge",
	"ready for review",
	"all done",
	"work is done",
	"mission complete",
}

// HasOracleEvidence is true only for a valid envelope oracle slot (🎯T509 /
// 🎯T766.4). A sentence is not oracle evidence.
func HasOracleEvidence(report string) bool {
	m, err := envelope.Parse(report)
	return m != nil && err == nil && m.HasOracle()
}

// HasAcceptedRisk is true only for a valid envelope risk slot (🎯T509 /
// 🎯T766.4). A sentence is not accepted risk.
func HasAcceptedRisk(report string) bool {
	m, err := envelope.Parse(report)
	return m != nil && err == nil && m.HasRisk()
}

func oracleScanBody(report string) string {
	if m, _ := envelope.Parse(report); m != nil && m.Payload != "" {
		return m.Payload
	}
	return report
}

// LooksLikeBareDone is true when the report claims completion without
// oracle evidence or accepted-risk language (🎯T31.1 refuse bare done).
func LooksLikeBareDone(report string) bool {
	return ClassifyCompletionReport(report) == CompletionBareDone
}

// HasOracleOrRisk is true when the report carries either oracle evidence
// or accepted-risk language (acceptable under T31.1).
func HasOracleOrRisk(report string) bool {
	c := ClassifyCompletionReport(report)
	return c == CompletionOracleEvidence || c == CompletionAcceptedRisk
}

// ClassifyCompletionReport classifies a worker/PO finish report from
// envelope fields (🎯T509 / 🎯T536.1). A sentence is not a claim (🎯T766.4).
func ClassifyCompletionReport(report string) CompletionEvidenceClass {
	m, err := envelope.Parse(report)
	if m == nil {
		return CompletionNoClaim
	}
	switch m.Kind {
	case envelope.KindFinishReport:
		if envelope.MissingSilentLedger(m) {
			return CompletionMissingSilentLedger
		}
		if err == nil {
			if m.HasRisk() {
				return CompletionAcceptedRisk
			}
			if m.HasOracle() {
				return CompletionOracleEvidence
			}
			return CompletionBareDone
		}
		// Other malformation with a ledger still present: do not treat
		// payload prose as a complete oracle finish.
		if m.HasSilentLedger() {
			if m.HasRisk() {
				return CompletionAcceptedRisk
			}
			if m.HasOracle() {
				return CompletionOracleEvidence
			}
		}
	case envelope.KindStatusPing, envelope.KindAck, envelope.KindSpawnBrief, envelope.KindScoutReport:
		if err == nil {
			return CompletionNoClaim
		}
	}
	return CompletionNoClaim
}

// LooksLikeMissingSilentLedger is true when a finish-report claims done
// with oracle/risk slots but carries no explicit silent-decision ledger
// (🎯T536.1).
func LooksLikeMissingSilentLedger(report string) bool {
	return ClassifyCompletionReport(report) == CompletionMissingSilentLedger
}

// hasOracleEvidence is not a control decision (🎯T766.4). A sentence is
// not oracle evidence. HasOracleEvidence reads the envelope oracle slot.
func hasOracleEvidence(string) bool { return false }

// hasAcceptedRisk is not a control decision (🎯T766.4). A sentence is
// not accepted risk. HasAcceptedRisk reads the envelope risk slot.
func hasAcceptedRisk(string) bool { return false }

// hasCompletionClaim is not a control decision (🎯T766.4). A sentence is
// not a completion claim. The done answer is the envelope.
func hasCompletionClaim(string) bool { return false }

// containsWordish matches phrase as a whole word or phrase: the characters
// flanking it must not be letters or digits.
//
// 🎯T395: this was a plain substring match, which made "incomplete" a claim of
// "complete", "unfinished" a claim of "finished", and "abandoned" a claim of
// "done" — three words that assert the opposite of completion, read as
// completion. That is the backwards bias at its purest, and it reaped workers
// for saying they had not finished. Only completion claims are matched this
// way; the oracle-evidence and accepted-risk markers stay substring matches,
// where a partial hit merely means the report gets more scrutiny, not less.
func containsWordish(lower, phrase string) bool {
	return indexWordish(lower, phrase) >= 0
}

// indexWordish is containsWordish returning where the phrase matched, or -1.
func indexWordish(lower, phrase string) int {
	for i := 0; i+len(phrase) <= len(lower); {
		j := strings.Index(lower[i:], phrase)
		if j < 0 {
			return -1
		}
		start, end := i+j, i+j+len(phrase)
		if !wordRuneBefore(lower, start) && !wordRuneAt(lower, end) {
			return start
		}
		i = start + 1
	}
	return -1
}

// FindCompletionClaim does not name a sentence as a claim (🎯T766.4).
// A reap log does not decide from a completion word.
func FindCompletionClaim(string) (marker, span string, offset int, ok bool) {
	return "", "", 0, false
}

func wordRuneBefore(s string, i int) bool {
	if i <= 0 {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return isWordRune(r)
}

func wordRuneAt(s string, i int) bool {
	if i >= len(s) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return isWordRune(r)
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// developmentSurfaceEvidenceMarkers cite an activated development surface in
// legacy finish-report prose. HasActivationEvidence does not read these
// (🎯T766.4); HasDailyPathEvidence does (🎯T572 legacy accept).
var developmentSurfaceEvidenceMarkers = []string{
	"restart-jevonsd",
	"restart-daily-jevonsd",
	"live probe",
	"development path",
	"development surface",
	"daily path", // legacy accept — speech is development (🎯T572)
	"daily-path",
	"curl ",
	"curl\t",
	"http 200",
	"http/1.1 200",
	"non-404",
	"not 404",
	":13705",
	"127.0.0.1:13705",
	"localhost:13705",
	"/api/frontier",
	"/health",
	"stale binary",
	"zero-downtime upgrade",
	"proven zero-downtime",
}

// HasActivationEvidence reports whether a finish report declares activation
// of the development surface in the envelope daily slot. 🎯T552 / 🎯T553.2:
// this is a seam classifier, not an achieve gate. Observation of the
// running surface is the test. A sentence that names a restart or a curl
// is not that slot (🎯T766.4).
func HasActivationEvidence(report string) bool {
	m, err := envelope.Parse(report)
	return m != nil && err == nil && m.HasActivation()
}

// HasDailyPathEvidence is the legacy name for classifying development-surface
// activation citations in finish reports (🎯T572). It accepts the envelope
// daily slot and legacy prose (including "daily path") so older reports still
// classify. Not an achieve gate — observation of the running surface is.
func HasDailyPathEvidence(report string) bool {
	if HasActivationEvidence(report) {
		return true
	}
	s := strings.ToLower(strings.TrimSpace(oracleScanBody(report)))
	if s == "" {
		return false
	}
	for _, m := range developmentSurfaceEvidenceMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	if strings.Contains(s, "get ") && (strings.Contains(s, "200") || strings.Contains(s, "non-404")) {
		if strings.Contains(s, "http") || strings.Contains(s, "/api/") || strings.Contains(s, ":13705") {
			return true
		}
	}
	return false
}

// reportCitesOracleID is true only for a valid envelope oracle slot
// (🎯T766.4). A SHA or GATE line in prose is not a citation.
func reportCitesOracleID(report string) bool {
	m, err := envelope.Parse(report)
	return m != nil && err == nil && m.HasOracle()
}

// UncitedClaimAgainstDirtyWorktree is not a reap veto (🎯T766.4).
// Marker words are not oracle evidence, so a dirty worktree is not
// judged from a sentence. Nothing on the reap path calls it
// (🎯T766.4.1); owned uncommitted scope is the 🎯T972 typed check.
func UncitedClaimAgainstDirtyWorktree(string, *gate.TreeProvenance) bool { return false }

// LooksLikeHermeticOnlyDaemonClaim is true when a report declares an oracle
// and does not declare activation (🎯T194). A hermetic sentence is not that
// claim (🎯T766.4). Instructional residual for overseer review — not a hard block.
func LooksLikeHermeticOnlyDaemonClaim(report string) bool {
	return HasOracleEvidence(report) && !HasActivationEvidence(report)
}

// LiveRenderOracleDidNotRun is not a browser-oracle admission (🎯T766.4).
// A sentence that says the live-render oracle did not run is not that admission.
func LiveRenderOracleDidNotRun(string) bool { return false }

// LooksLikeSilentBrowserOracleFallback is not a silent-fallback finding (🎯T766.4).
// Citing hermetics beside a browser sentence is not that finding.
func LooksLikeSilentBrowserOracleFallback(string) bool { return false }

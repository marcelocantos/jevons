// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/marcelocantos/jevons/internal/envelope"
	"github.com/marcelocantos/jevons/internal/gate"
	"github.com/marcelocantos/jevons/internal/shaevidence"
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

// commitSHARe matches short/full git SHAs (7–40 hex). Word-boundary style
// via non-hex flanks so "pass" / common English words are not false hits.
var commitSHARe = regexp.MustCompile(`(?i)(?:^|[^0-9a-f])([0-9a-f]{7,40})(?:[^0-9a-f]|$)`)

// 🎯T784: bare "pass" is removed — "the scout pass is done" (a status
// noun, not a test verdict) false-positived as oracle evidence, which
// short-circuited hasFinishShape's bare-claim check and reaped a worker
// mid-status (jv-t762-dropped-spawn-half, 2026-09-21). "passed" stays: it
// is verb-shaped and paired oracle prose ("the test passed", "N passed")
// is the common case; a bare "tests pass" / "suite passes" phrasing is
// covered by the compound entries below instead of the single word.
var oracleEvidenceMarkers = []string{
	"go test",
	"make test",
	"test-web",
	"test-go",
	"test-journey",
	"test-ui",
	"tests pass",
	"test passes",
	"suite passes",
	"passed",
	"green",
	"oracle",
	"hermetic",
	"sha ",
	"commit ",
	"commits ",
	"attestation",
}

var acceptedRiskMarkers = []string{
	"accepted risk",
	"accepted-risk",
	"class-3",
	"class 3",
	"human gate",
	"owner accept",
	"owner smoke",
	"residual:",
	"residual declared",
	"isolated class-3",
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

// HasOracleEvidence reports whether report contains executable-oracle
// markers (tests/green/SHA). Envelope oracle slots win when present
// (🎯T509); otherwise the prose heuristic.
func HasOracleEvidence(report string) bool {
	if m, err := envelope.Parse(report); m != nil && err == nil && m.HasOracle() {
		return true
	}
	return hasOracleEvidence(strings.ToLower(oracleScanBody(report)))
}

// HasAcceptedRisk reports explicit accepted-risk / class-3 residual language.
// Envelope risk slots win when present (🎯T509).
func HasAcceptedRisk(report string) bool {
	if m, err := envelope.Parse(report); m != nil && err == nil && m.HasRisk() {
		return true
	}
	return hasAcceptedRisk(strings.ToLower(oracleScanBody(report)))
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

// ClassifyCompletionReport classifies a worker/PO finish report.
// Priority: envelope fields (🎯T509 / 🎯T536.1) when present, else
// accepted-risk > oracle evidence > bare-done claim > no claim.
func ClassifyCompletionReport(report string) CompletionEvidenceClass {
	if m, err := envelope.Parse(report); m != nil {
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
		if m.Payload != "" {
			report = m.Payload
		}
	}
	s := strings.ToLower(strings.TrimSpace(report))
	if s == "" {
		return CompletionNoClaim
	}
	// Accepted risk is an explicit residual path — preferred over bare claim
	// even when "done" words co-occur.
	if hasAcceptedRisk(s) {
		return CompletionAcceptedRisk
	}
	if hasOracleEvidence(s) {
		return CompletionOracleEvidence
	}
	if hasCompletionClaim(s) {
		return CompletionBareDone
	}
	return CompletionNoClaim
}

// LooksLikeMissingSilentLedger is true when a finish-report claims done
// with oracle/risk slots but carries no explicit silent-decision ledger
// (🎯T536.1).
func LooksLikeMissingSilentLedger(report string) bool {
	return ClassifyCompletionReport(report) == CompletionMissingSilentLedger
}

func hasOracleEvidence(lower string) bool {
	if commitSHARe.MatchString(lower) {
		// Require a claim-adjacent or evidence-shaped context: bare hex
		// alone in a paragraph can false-positive; require SHA/commit word
		// nearby or an explicit test/pass marker.
		if strings.Contains(lower, "sha") || strings.Contains(lower, "commit") {
			return true
		}
	}
	// 🎯T784: word-boundary match, not a bare substring scan. A short
	// marker like "pass" false-positived inside "the scout pass is done"
	// (jv-t762-dropped-spawn-half, 2026-09-21), reading a status update
	// as oracle evidence and short-circuiting hasFinishShape's bare-claim
	// check straight into a reap.
	for _, m := range oracleEvidenceMarkers {
		if containsWordish(lower, strings.TrimSpace(m)) {
			return true
		}
	}
	// SHA without the word "sha" still counts when report also claims done
	// with a short hex token after "SHA"/"sha" pattern handled above, or
	// conventional "SHA abcdef0" form.
	if commitSHARe.MatchString(lower) && (strings.Contains(lower, "sha") ||
		strings.Contains(lower, "commit") ||
		strings.Contains(lower, "landed") ||
		strings.Contains(lower, "merged")) {
		return true
	}
	return false
}

func hasAcceptedRisk(lower string) bool {
	for _, m := range acceptedRiskMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// hasCompletionClaim is true when the report ASSERTS completion. 🎯T750: the
// scan runs over claimScanText, so a marker the worker quoted, fenced, or
// negated is not read as a claim it made.
func hasCompletionClaim(lower string) bool {
	scan := claimScanText(lower)
	for _, m := range completionClaimMarkers {
		if containsWordish(scan, m) {
			return true
		}
	}
	return false
}

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

// FindCompletionClaim locates the completion word that makes report read as a
// claim, and the span of text around it (🎯T439). Reported so a reap can say in
// the lifecycle log which words it fired on, rather than leaving a worker's
// disappearance from the fleet unexplained.
//
// Diagnostic only: no decision is taken from this, so a miss costs an empty
// span in a log line and nothing more.
func FindCompletionClaim(report string) (marker, span string, offset int, ok bool) {
	// 🎯T750: name the claim the worker made, so the log line and the reap
	// decision are reading the same text.
	lower := claimScanText(asciiLower(report))
	best, at := "", -1
	for _, m := range completionClaimMarkers {
		i := indexWordish(lower, m)
		if i < 0 || (at >= 0 && i >= at) {
			continue
		}
		best, at = m, i
	}
	if at < 0 {
		return "", "", 0, false
	}
	// 🎯T470: name the sentence that matched, not a 200-byte window around
	// an offset — a wrong reap must be diagnosable from the log alone.
	span, offset = matchedSentence(report, at, at+len(best))
	return best, span, offset, true
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

// dailyPathEvidenceMarkers cite an activated owner-visible surface (🎯T194 /
// 🎯T572). Hermetic-only finish reports for daemon/API work are not sufficient.
var dailyPathEvidenceMarkers = []string{
	"restart-jevonsd",
	"restart-jevonsd",
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
	"stale binary", // often named when proving bounce fixed it
	"zero-downtime upgrade",
	"proven zero-downtime",
}

// hermeticOnlyMarkers are evidence that does not activate the development surface.
// Used only to document the T194 residual in tests — HasActivationEvidence
// does not treat these as daily-path proof.
var hermeticOnlyMarkers = []string{
	"go test",
	"make test",
	"hermetic",
	"test-web",
	"test-go",
	"node web/scripts",
}

// HasActivationEvidence reports whether a finish report cites activation
// of the development surface (restart script, curl, :13705, …). 🎯T552 / 🎯T553.2:
// this is a seam classifier, not an achieve gate. Observation of the
// running surface is the test. Pure string heuristic.
func HasActivationEvidence(report string) bool {
	if m, err := envelope.Parse(report); m != nil && err == nil && m.HasActivation() {
		return true
	}
	s := strings.ToLower(strings.TrimSpace(oracleScanBody(report)))
	if s == "" {
		return false
	}
	for _, m := range dailyPathEvidenceMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	// Live HTTP probe shapes: "GET … 200", "curl -sS … → 200"
	if strings.Contains(s, "get ") && (strings.Contains(s, "200") || strings.Contains(s, "non-404")) {
		if strings.Contains(s, "http") || strings.Contains(s, "/api/") || strings.Contains(s, ":13705") {
			return true
		}
	}
	return false
}

// reportCitesOracleID is true when report names a checkable oracle artifact:
// an envelope sha/gate-id slot, an evidence-shaped SHA in prose (🎯T427
// shape), or a GATE attestation line (🎯T386 shape). Marker words alone —
// "pass", "green", "oracle" — name nothing a reader can go check.
func reportCitesOracleID(report string) bool {
	if m, err := envelope.Parse(report); m != nil && err == nil && m.HasOracle() {
		return true
	}
	body := oracleScanBody(report)
	if len(shaevidence.ExtractEvidenceSHAs(body)) > 0 {
		return true
	}
	return len(gate.ParseAttestations(body)) > 0
}

// UncitedClaimAgainstDirtyWorktree is true when report reads as oracle
// evidence purely on marker words (🎯T946) — no cited SHA, no cited gate id —
// while tree says the worker's own worktree carries uncommitted tracked
// changes or new untracked source files.
//
// jv-t906-accept-lifts-block and jv-t943-stale-token-reload were both scored
// finished_work this way: neither report named a landed commit or a gate id,
// and both worktrees were fully uncommitted at the moment of the reap.
// Nothing existed for the "pass"/"green"/"go test" words to describe.
func UncitedClaimAgainstDirtyWorktree(report string, tree *gate.TreeProvenance) bool {
	if tree == nil || tree.Clean {
		return false
	}
	if reportCitesOracleID(report) {
		return false
	}
	return hasOracleEvidence(strings.ToLower(oracleScanBody(report)))
}

// LooksLikeHermeticOnlyDaemonClaim is true when the report claims completion
// with hermetic/test oracle language but no daily-path evidence (🎯T194
// anti-pattern: achieve while stale binary may still serve). Instructional
// residual for overseer review — not a hard block.
func LooksLikeHermeticOnlyDaemonClaim(report string) bool {
	if HasActivationEvidence(report) {
		return false
	}
	if !hasCompletionClaim(strings.ToLower(report)) && !HasOracleEvidence(report) {
		return false
	}
	// Require hermetic-ish oracle markers so pure bare-done is T31, not T194.
	lower := strings.ToLower(report)
	for _, m := range hermeticOnlyMarkers {
		if strings.Contains(lower, m) {
			return hasCompletionClaim(lower) || HasOracleEvidence(report)
		}
	}
	return false
}

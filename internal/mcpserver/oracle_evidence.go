// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"regexp"
	"strings"
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
	default:
		return "unknown"
	}
}

// commitSHARe matches short/full git SHAs (7–40 hex). Word-boundary style
// via non-hex flanks so "pass" / common English words are not false hits.
var commitSHARe = regexp.MustCompile(`(?i)(?:^|[^0-9a-f])([0-9a-f]{7,40})(?:[^0-9a-f]|$)`)

var oracleEvidenceMarkers = []string{
	"go test",
	"make test",
	"test-web",
	"test-go",
	"test-journey",
	"test-ui",
	"pass",
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
// markers (tests/green/SHA). Pure string heuristic.
func HasOracleEvidence(report string) bool {
	return hasOracleEvidence(strings.ToLower(report))
}

// HasAcceptedRisk reports explicit accepted-risk / class-3 residual language.
func HasAcceptedRisk(report string) bool {
	return hasAcceptedRisk(strings.ToLower(report))
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
// Priority: accepted-risk > oracle evidence > bare-done claim > no claim.
func ClassifyCompletionReport(report string) CompletionEvidenceClass {
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

func hasOracleEvidence(lower string) bool {
	if commitSHARe.MatchString(lower) {
		// Require a claim-adjacent or evidence-shaped context: bare hex
		// alone in a paragraph can false-positive; require SHA/commit word
		// nearby or an explicit test/pass marker.
		if strings.Contains(lower, "sha") || strings.Contains(lower, "commit") {
			return true
		}
	}
	for _, m := range oracleEvidenceMarkers {
		if strings.Contains(lower, m) {
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

func hasCompletionClaim(lower string) bool {
	for _, m := range completionClaimMarkers {
		if containsWordish(lower, m) {
			return true
		}
	}
	return false
}

// containsWordish is a light whole-phrase match (substring after lowercasing).
func containsWordish(lower, phrase string) bool {
	return strings.Contains(lower, phrase)
}

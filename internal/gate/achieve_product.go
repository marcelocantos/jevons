// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"fmt"
	"regexp"
	"strings"
)

// FlagAchieveProductPathUnrun: the target's acceptance names the running
// product, but the attestation's own text declares that path unrun (🎯T703).
const (
	FlagAchieveProductPathUnrun FlagKind = "achieve_product_path_unrun"
)

// acceptanceProductMarkers are phrases in a target's acceptance that require
// observation of the running product, not hermetic green alone.
var acceptanceProductMarkers = []string{
	"live surface",
	"minted seat",
	"hard-reloaded cockpit",
	"activated daemon",
	"running product",
	"the running product",
	"live evidence",
	"seat minted",
	"product land",
	"owner-visible",
	"hard reload",
	"hard-reload",
	"real unattended pass",
	"on the product path",
	"production-path activation",
	"production path activation",
	"real production-path",
	"real-path activation",
}

// attestationProductObservationMarkers cite that the development surface was
// observed, not only built or unit-tested. Kept in gate (not mcpserver) so
// achieve verification stays self-contained.
var attestationProductObservationMarkers = []string{
	"activation independently confirmed",
	"activation confirmed",
	"activated on the development daemon",
	"genuinely live on the development daemon",
	"serving build=",
	"curl http://localhost:13705",
	"curl http://127.0.0.1:13705",
	"localhost:13705",
	"127.0.0.1:13705",
	":13705/health",
	"hard reload",
	"hard-reload",
	"live probe",
	"live playwright",
	"playwright behavioral",
	"real browser",
	"real device",
	"product-path activation",
	"production-path activation",
	"observed on the running surface",
	"observed on the development daemon",
	"landed on claude",
	"landing on claude",
	"seat minted while",
	"invoked a real jevons_",
}

var achieveAcceptanceOverrideRe = regexp.MustCompile(
	`(?i)\bachieve[_ -]acceptance[_ -]override\b`)

// AcceptanceRequiresRunningProduct reports whether any acceptance bullet names
// the running product path (live surface, mint, activated daemon, etc.).
func AcceptanceRequiresRunningProduct(acceptance []string) bool {
	_, ok := AcceptanceProductClause(acceptance)
	return ok
}

// AcceptanceProductClause returns the first acceptance bullet that requires the
// running product, for refusal detail text.
func AcceptanceProductClause(acceptance []string) (clause string, ok bool) {
	for _, line := range acceptance {
		lower := strings.ToLower(strings.TrimSpace(line))
		if lower == "" {
			continue
		}
		for _, m := range acceptanceProductMarkers {
			if strings.Contains(lower, m) {
				return strings.TrimSpace(line), true
			}
		}
	}
	return "", false
}

// HasAchieveAcceptanceOverride is true when the attestation records that the
// target's acceptance is wrong about requiring product observation (🎯T703).
func HasAchieveAcceptanceOverride(attestation string) bool {
	return achieveAcceptanceOverrideRe.MatchString(attestation)
}

// attestationClaimsProductObservation reports whether the attestation cites
// observation of the running product, not only hermetic gates.
func attestationClaimsProductObservation(attestation string) bool {
	lower := strings.ToLower(attestation)
	for _, m := range attestationProductObservationMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	if strings.Contains(lower, "curl ") && (strings.Contains(lower, ":13705") ||
		strings.Contains(lower, "/health") || strings.Contains(lower, "/api/")) {
		return true
	}
	return false
}

// attestationAdmitsProductPathUnrun returns the phrase in the attestation that
// declares the product path was not exercised, and a short kind label.
func attestationAdmitsProductPathUnrun(attestation string) (phrase string, ok bool) {
	lower := strings.ToLower(attestation)
	switch {
	case strings.Contains(lower, "daemon not rebuilt"):
		return "daemon not rebuilt", true
	case strings.Contains(lower, "not activated"):
		return "not activated", true
	}
	if strings.Contains(lower, "local only") && !attestationClaimsProductObservation(attestation) {
		return "local only", true
	}
	if ledgerGateRe.MatchString(attestation) && !attestationClaimsProductObservation(attestation) {
		return "hermetic gate evidence only", true
	}
	return "", false
}

func achieveProductPathFlags(args *AchieveCheckArgs) []Flag {
	if len(args.Acceptance) == 0 || HasAchieveAcceptanceOverride(args.Attestation) {
		return nil
	}
	clause, required := AcceptanceProductClause(args.Acceptance)
	if !required {
		return nil
	}
	phrase, admits := attestationAdmitsProductPathUnrun(args.Attestation)
	if !admits {
		return nil
	}
	excerpt := clause
	if len(excerpt) > 120 {
		excerpt = excerpt[:117] + "..."
	}
	return []Flag{{
		Kind: FlagAchieveProductPathUnrun,
		Detail: fmt.Sprintf(
			"acceptance requires the running product (%q); attestation declares that path unrun — %q",
			excerpt, phrase),
		Evidence: phrase,
	}}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package rsi

import "strings"

// DefaultFilingAcceptance is the generic placeholder acceptance the light
// filing path substitutes when none is given. It does not count as a real
// acceptance criterion under the T333 quality bar.
const DefaultFilingAcceptance = "Owner confirms the desired state is met."

// FilingQuality is the T333 quality-bar verdict for a coach-linked filing.
type FilingQuality struct {
	OK      bool
	Reasons []string
}

// barePhrasePrefixes are leading tokens that mark a phrase-friction leaf
// ("friction: timeout") rather than a desired-state assertion with a mechanism.
var barePhrasePrefixes = map[string]struct{}{
	"friction":  {},
	"pain":      {},
	"annoyance": {},
	"issue":     {},
	"problem":   {},
	"phrase":    {},
}

// IsBarePhraseFrictionName reports whether name is a bare phrase-friction
// leaf: a friction-word prefix with fewer than four words of mechanism after
// it, or too short to assert anything.
func IsBarePhraseFrictionName(name string) bool {
	words := strings.Fields(strings.ToLower(strings.TrimSpace(name)))
	if len(words) < 3 {
		return true
	}
	first := strings.Trim(words[0], ":-—()")
	if _, ok := barePhrasePrefixes[first]; ok && len(words)-1 < 4 {
		return true
	}
	return false
}

// ClassifyFilingQuality applies the 🎯T333 quality bar to a filing that came
// from a coach judgment: refuse bare phrase-friction leaves; require a real
// name, at least one non-placeholder acceptance criterion, and an evidence
// pointer (session/event). Pure — hermetic tests decide it without a daemon.
func ClassifyFilingQuality(name string, acceptance []string, evidence string) FilingQuality {
	var reasons []string
	if IsBarePhraseFrictionName(name) {
		reasons = append(reasons, "bare_phrase_friction: name lacks a mechanism — assert the desired state, not the phrase")
	}
	real := false
	for _, a := range acceptance {
		a = strings.TrimSpace(a)
		if a == "" || strings.EqualFold(a, DefaultFilingAcceptance) {
			continue
		}
		real = true
		break
	}
	if !real {
		reasons = append(reasons, "missing_acceptance: at least one concrete acceptance criterion required")
	}
	if strings.TrimSpace(evidence) == "" {
		reasons = append(reasons, "missing_evidence: evidence pointer (session/event id) required")
	}
	return FilingQuality{OK: len(reasons) == 0, Reasons: reasons}
}

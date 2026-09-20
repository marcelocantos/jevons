// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// LifecycleNarrationClass is a hermetic classification of a report that
// claims a seat was killed, stopped, reaped, or respawned (🎯T692).
// Heuristic only — not a live registry probe; overseer judgment still applies.
type LifecycleNarrationClass int

const (
	// LifecycleNarrationNone: no named-seat lifecycle claim.
	LifecycleNarrationNone LifecycleNarrationClass = iota
	// LifecycleNarrationIntent: plan/gerund ('killing', 'will stop'), not fact.
	LifecycleNarrationIntent
	// LifecycleNarrationVerified: past-tense claim preceded by a live-list result.
	LifecycleNarrationVerified
	// LifecycleNarrationUnverified: past-tense claim with no live-list citation.
	LifecycleNarrationUnverified
)

func (c LifecycleNarrationClass) String() string {
	switch c {
	case LifecycleNarrationNone:
		return "none"
	case LifecycleNarrationIntent:
		return "intent"
	case LifecycleNarrationVerified:
		return "verified"
	case LifecycleNarrationUnverified:
		return "unverified"
	default:
		return "unknown"
	}
}

// Past-tense completed-fact verbs. Wordish so "killing" is not "killed".
var lifecyclePastTense = []string{
	"killed", "stopped", "reaped", "respawned",
}

// Intent/plan phrasing from the acceptance ('killing', 'will stop').
var lifecycleIntentPhrases = []string{
	"killing", "stopping", "reaping", "respawning",
	"will kill", "will stop", "will reap", "will respawn",
	"going to kill", "going to stop", "going to reap", "going to respawn",
	"about to kill", "about to stop", "about to reap", "about to respawn",
}

// liveListResultMarkers are a live registry check whose RESULT is cited —
// not the tool name appearing in doctrine. "jevons_agent_list" in an
// instruction is not a citation; "jevons_agent_list: jv-x not running" is.
var liveListResultMarkers = []string{
	"jevons_agent_list:",
	"jevons_agent_list shows",
	"jevons_agent_list lists",
	"jevons_agent_list returned",
	"get /api/agents:",
	"get /api/agents returned",
	"/api/agents returned",
	"/api/agents:",
	"live registry:",
	"agent_list shows",
}

// lifecycleClaimWindow is how close a named seat must sit to a past-tense
// verb to count as one claim. Tight enough that doctrine can name the
// specimen seat in a later sentence than the vocabulary list.
const lifecycleClaimWindow = 120

// LooksLikeUnverifiedLifecycleClaim is the 🎯T692 anti-pattern: a named
// seat was killed/stopped/reaped/respawned stated as fact, with no live
// registry result cited earlier in the same turn.
func LooksLikeUnverifiedLifecycleClaim(report string) bool {
	return ClassifyLifecycleNarration(report) == LifecycleNarrationUnverified
}

// ClassifyLifecycleNarration classifies a report for 🎯T692.
// Priority: unverified > verified > intent > none.
func ClassifyLifecycleNarration(report string) LifecycleNarrationClass {
	lower := strings.ToLower(strings.TrimSpace(report))
	if lower == "" {
		return LifecycleNarrationNone
	}
	claims := pastTenseLifecycleClaims(lower)
	if len(claims) > 0 {
		for _, c := range claims {
			if !hasLiveListResultCitation(lower[:c.start]) {
				return LifecycleNarrationUnverified
			}
		}
		return LifecycleNarrationVerified
	}
	if hasIntentLifecycleClaim(lower) {
		return LifecycleNarrationIntent
	}
	return LifecycleNarrationNone
}

type lifecycleClaim struct {
	start int
	end   int
	name  string
}

func pastTenseLifecycleClaims(lower string) []lifecycleClaim {
	var out []lifecycleClaim
	for _, verb := range lifecyclePastTense {
		for i := 0; i+len(verb) <= len(lower); {
			j := indexWordish(lower[i:], verb)
			if j < 0 {
				break
			}
			start := i + j
			end := start + len(verb)
			name, ok := agentNameNear(lower, start, end)
			if ok {
				out = append(out, lifecycleClaim{start: start, end: end, name: name})
			}
			i = end
		}
	}
	return out
}

func hasIntentLifecycleClaim(lower string) bool {
	for _, p := range lifecycleIntentPhrases {
		for i := 0; i+len(p) <= len(lower); {
			j := indexWordish(lower[i:], p)
			if j < 0 {
				break
			}
			start := i + j
			end := start + len(p)
			if _, ok := agentNameNear(lower, start, end); ok {
				return true
			}
			i = end
		}
	}
	return false
}

func hasLiveListResultCitation(prefix string) bool {
	for _, m := range liveListResultMarkers {
		if strings.Contains(prefix, m) {
			return true
		}
	}
	return false
}

func agentNameNear(lower string, start, end int) (string, bool) {
	from := start - lifecycleClaimWindow
	if from < 0 {
		from = 0
	}
	to := end + lifecycleClaimWindow
	if to > len(lower) {
		to = len(lower)
	}
	var found string
	eachAgentName(lower[from:to], func(name string, _, _ int) {
		if found == "" {
			found = name
		}
	})
	return found, found != ""
}

func eachAgentName(lower string, fn func(name string, start, end int)) {
	for _, prefix := range []string{"jv-", "cl-"} {
		for i := 0; i < len(lower); {
			j := strings.Index(lower[i:], prefix)
			if j < 0 {
				break
			}
			start := i + j
			if wordRuneBefore(lower, start) {
				i = start + 1
				continue
			}
			end := start + len(prefix)
			for end < len(lower) {
				r, size := utf8.DecodeRuneInString(lower[end:])
				if !isAgentNameRune(r) {
					break
				}
				end += size
			}
			if end > start+len(prefix) {
				fn(lower[start:end], start, end)
			}
			if end <= start {
				i = start + 1
			} else {
				i = end
			}
		}
	}
	for _, exact := range []string{"jevons-po", "claudia-po"} {
		for i := 0; i < len(lower); {
			j := indexWordish(lower[i:], exact)
			if j < 0 {
				break
			}
			start := i + j
			end := start + len(exact)
			fn(exact, start, end)
			i = end
		}
	}
}

func isAgentNameRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '-' || r == '_'
}

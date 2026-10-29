// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package ownerquestion

import (
	"regexp"
	"strings"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// An untyped report may still contain an explicit, answerable owner request.
// The blocker is the authoritative ask: incidental mentions in the report
// body are not promoted. In particular, an unavailable device alone is not
// an owner decision. A bare "owner verdict outstanding" is not an exact ask.
var ownerAsk = regexp.MustCompile(`(?i)\b(?:owner|the owner)\s+(?:review|decision|input)\s*(?:requested|required|needed|:|—|-)\s*(.+)`)
var ownerVerb = regexp.MustCompile(`(?i)\b(?:accept|reject|approve|choose|decide|review|provide|confirm)\b`)
var evidenceURL = regexp.MustCompile(`https?://[^\s<>\x60]+`)

// FromExplicitBlocker is a conservative fallback to typed owner questions.
// It does not relax the T1028.1 owner-decision schema: the original envelope
// remains an ordinary blocked report. A stable family and content version
// allow the existing durable view and outbox to enforce replay/lifecycle.
func FromExplicitBlocker(text, workdir, asker string) (Question, bool, error) {
	m, err := envelope.Parse(text)
	if m == nil || m.Kind != envelope.KindFinishReport || m.Status != envelope.ProgressBlocked || m.BlockClass == envelope.BlockOwnerDecision {
		return Question{}, false, nil
	}
	if err != nil {
		return Question{}, false, err
	}
	return explicitBlockerFields(m.Target, m.Blocker, m.Payload, m.SHA, workdir, asker)
}

func repoName(repo string) string {
	parts := strings.Split(strings.TrimRight(repo, "/"), "/")
	return parts[len(parts)-1]
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package ownerquestion

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
	match := ownerAsk.FindStringSubmatch(strings.TrimSpace(m.Blocker))
	if len(match) != 2 || !ownerVerb.MatchString(match[1]) {
		return Question{}, false, nil
	}
	ask := strings.TrimSpace(match[1])
	// An actionable question is not inferred from a missing device. It must
	// name what the owner can actually answer, even if a prerequisite remains.
	if len(ask) < 12 || (strings.Contains(strings.ToLower(ask), "when device") || strings.Contains(strings.ToLower(ask), "once device")) {
		return Question{}, false, nil
	}
	link := evidenceURL.FindString(m.Payload)
	if link == "" && m.SHA == "" {
		return Question{}, false, nil
	}
	repo, err := SharedRepo(workdir)
	if err != nil {
		return Question{}, false, err
	}
	evidence := link
	if evidence == "" {
		evidence = "commit " + m.SHA
	}
	hash := sha256.Sum256([]byte(ask))
	q := Question{Identity: Identity{Repo: repo, Target: m.Target, ID: "explicit-owner-blocker", Version: hex.EncodeToString(hash[:8])}, Text: fmt.Sprintf("%s/%s — owner action: %s. Evidence: %s. Answer via jevons-po for %s.", repoName(repo), m.Target, ask, evidence, m.Target), Asker: asker, AnswerRoute: "reply to jevons-po with target " + m.Target, Link: link, State: Open}
	return q, true, q.Validate()
}

func repoName(repo string) string {
	parts := strings.Split(strings.TrimRight(repo, "/"), "/")
	return parts[len(parts)-1]
}

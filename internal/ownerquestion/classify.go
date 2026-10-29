// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package ownerquestion

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"

	"github.com/marcelocantos/jevons/internal/ownergate"
)

// actionableReviewFields is the shared tier-2 classifier for finish-reports
// and ledger target rows. payload is the evidentiary body; blocker is the
// authoritative wait reason (envelope blocker or owned_by.reason).
func actionableReviewFields(target, sha, gateID, payload, blocker, workdir, asker string) (ReviewEvent, bool, error) {
	payload = strings.ToLower(payload)
	blocker = strings.ToLower(blocker)
	if target == "" || sha == "" || !(strings.Contains(payload, "activated") || strings.Contains(payload, "reported serving")) || !strings.Contains(payload, "hard-reload") || !strings.Contains(payload, "visual verdict") || !strings.Contains(payload, "screenshot") || !strings.Contains(payload, "folded/unfolded") || !strings.Contains(blocker, "owner verdict") || !strings.Contains(blocker, "disconnected") || !strings.Contains(blocker, "folded/unfolded") {
		return ReviewEvent{}, false, nil
	}
	shots := screenshotEvidence.FindAllString(payload, -1)
	if len(shots) < 2 {
		return ReviewEvent{}, false, nil
	}
	repo, err := SharedRepo(workdir)
	if err != nil {
		return ReviewEvent{}, false, err
	}
	evidence := fmt.Sprintf("commit %s; reported screenshot refs (not hardware verification) %s", sha, strings.Join(shots, ", "))
	h := sha256.Sum256([]byte(evidence))
	e := ReviewEvent{Identity: Identity{Repo: repo, Target: target, ID: "hardware-visual-review", Version: hex.EncodeToString(h[:8])}, Evidence: evidence, CommitSHA: sha, GateID: gateID, ScreenshotRefs: shots, Prerequisite: "Reconnect the Fold and capture folded/unfolded hardware screenshots", Action: "review both folded/unfolded screenshots and give an accept/reject visual verdict", AnswerRoute: "reply to jevons-po with target " + target + " (accept/reject)", Asker: asker, Lifecycle: Open, Readiness: PrerequisiteBlocked, Link: "http://localhost:13705/"}
	if !strings.Contains(payload, "`:13705`") {
		e.Link = ""
	}
	return e, true, e.Question().Validate()
}

// explicitBlockerFields is the shared tier-3 classifier.
func explicitBlockerFields(target, blocker, payload, sha, workdir, asker string) (Question, bool, error) {
	match := ownerAsk.FindStringSubmatch(strings.TrimSpace(blocker))
	if len(match) != 2 || !ownerVerb.MatchString(match[1]) {
		return Question{}, false, nil
	}
	ask := strings.TrimSpace(match[1])
	if len(ask) < 12 || (strings.Contains(strings.ToLower(ask), "when device") || strings.Contains(strings.ToLower(ask), "once device")) {
		return Question{}, false, nil
	}
	link := evidenceURL.FindString(payload)
	if link == "" && sha == "" {
		return Question{}, false, nil
	}
	repo, err := SharedRepo(workdir)
	if err != nil {
		return Question{}, false, err
	}
	evidence := link
	if evidence == "" {
		evidence = "commit " + sha
	}
	hash := sha256.Sum256([]byte(ask))
	q := Question{Identity: Identity{Repo: repo, Target: target, ID: "explicit-owner-blocker", Version: hex.EncodeToString(hash[:8])}, Text: fmt.Sprintf("%s/%s — owner action: %s. Evidence: %s. Answer via jevons-po for %s.", repoName(repo), target, ask, evidence, target), Asker: asker, AnswerRoute: "reply to jevons-po with target " + target, Link: link, State: Open}
	return q, true, q.Validate()
}

var ownerGateQuestion = regexp.MustCompile(`(?i)owner gate:\s*(.+?)\s+evidence:\s*(.+)`)

// explicitOwnerGateReason admits a T449-shaped owned_by.reason without a
// finish-report: structured marker + landed evidence + a named owner gate ask.
func explicitOwnerGateReason(target, reason, workdir, asker string) (Question, bool, error) {
	if !ownergate.IsAwaiting(ownergate.OwnerHandle, reason, "") {
		return Question{}, false, nil
	}
	match := ownerGateQuestion.FindStringSubmatch(reason)
	if len(match) != 3 {
		return Question{}, false, nil
	}
	ask := strings.TrimSpace(match[1])
	evidence := strings.TrimSpace(match[2])
	if len(ask) < 8 || !ownergate.HasLandedEvidence(evidence) {
		return Question{}, false, nil
	}
	repo, err := SharedRepo(workdir)
	if err != nil {
		return Question{}, false, err
	}
	hash := sha256.Sum256([]byte(ask + "\n" + evidence))
	link := evidenceURL.FindString(evidence)
	q := Question{Identity: Identity{Repo: repo, Target: target, ID: "explicit-owner-blocker", Version: hex.EncodeToString(hash[:8])}, Text: fmt.Sprintf("%s/%s — owner action: %s. Evidence: %s. Answer via jevons_owner_gate op=answer for %s.", repoName(repo), target, ask, evidence, target), Asker: asker, AnswerRoute: "jevons_owner_gate op=answer target " + target, Link: link, State: Open}
	return q, true, q.Validate()
}

// firstCommitSHA finds the first commit-shaped hex token in prose.
func firstCommitSHA(text string) string {
	for _, tok := range strings.FieldsFunc(text, func(r rune) bool {
		return !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F'))
	}) {
		if len(tok) >= 7 && len(tok) <= 40 && hasHexDigit(tok) {
			return tok
		}
	}
	return ""
}

func hasHexDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

// deviceOnlyOwnerHold is true when the ledger parks on a physical/device
// prerequisite the owner cannot answer as a review/decision (T370-class).
func deviceOnlyOwnerHold(reason, context string) bool {
	blob := strings.ToLower(reason + "\n" + context)
	if strings.Contains(blob, "owner decision requested:") || strings.Contains(blob, "owner review requested:") || strings.Contains(blob, "owner input requested:") {
		return false
	}
	if strings.Contains(blob, "visual verdict") || strings.Contains(blob, "owner gate:") {
		return false
	}
	for _, needle := range []string{"keypress", "cmd-shift", "⌘", "device unavailable", "wait for device", "when device", "once device", "disconnected; owner verdict outstanding"} {
		if strings.Contains(blob, needle) {
			return true
		}
	}
	return false
}

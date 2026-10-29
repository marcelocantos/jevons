// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package ownerquestion

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// Readiness separates notifying about a prerequisite from soliciting a verdict.
type Readiness string

const (
	PrerequisiteBlocked Readiness = "prerequisite_blocked"
	Actionable          Readiness = "actionable"
)

// ReviewEvent is the reusable T1042 contract for future review UI and push
// transports. Identity is repo + target + ask family + content version; the
// prerequisite is NOT an answer to the subsequent owner review question.
// An open event is an actionable sequence, not a claim that review occurred.
type ReviewEvent struct {
	Identity Identity `json:"identity"`
	Evidence string   `json:"evidence"`
	// Typed, bounded citations. Prose in Evidence is never upgraded to verification.
	CommitSHA      string    `json:"commit_sha,omitempty"`
	GateID         string    `json:"gate_id,omitempty"`
	ReportAgent    string    `json:"report_agent,omitempty"`
	ReportID       string    `json:"report_id,omitempty"`
	ScreenshotRefs []string  `json:"screenshot_refs,omitempty"`
	Prerequisite   string    `json:"prerequisite"`
	Action         string    `json:"action"`
	AnswerRoute    string    `json:"answer_route"`
	Asker          string    `json:"asker"`
	Lifecycle      Lifecycle `json:"lifecycle"`
	Readiness      Readiness `json:"readiness"`
	Link           string    `json:"link,omitempty"`
}

func (e ReviewEvent) Question() Question {
	return Question{Identity: e.Identity, Text: fmt.Sprintf("%s/%s — %s. Then %s. Evidence: %s. Answer via %s. (Readiness: %s; no visual approval before the hardware review.)", filepath.Base(e.Identity.Repo), e.Identity.Target, e.Prerequisite, e.Action, e.Evidence, e.AnswerRoute, e.Readiness), Asker: e.Asker, AnswerRoute: e.AnswerRoute, State: e.Lifecycle, Link: e.Link}
}

var screenshotEvidence = regexp.MustCompile(`(?i)(?:artifacts/[^\s\x60,;]+\.(?:png|jpg|jpeg))`)

// SharedRepo resolves linked worktrees to their shared repository identity.
// git's --show-toplevel alone would give each worktree a different question.
func SharedRepo(dir string) (string, error) {
	root, err := CanonicalRepo(dir)
	if err != nil {
		return "", err
	}
	out, err := exec.Command("git", "-C", root, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return root, nil
	} // hermetic standalone repositories
	common := strings.TrimSpace(string(out))
	if !filepath.IsAbs(common) {
		common = filepath.Join(root, common)
	}
	common, err = filepath.EvalSymlinks(common)
	if err != nil {
		return "", err
	}
	if filepath.Base(common) != ".git" {
		return root, nil
	}
	shared := filepath.Dir(common)
	if _, err = os.Stat(shared); err != nil {
		return "", err
	}
	return CanonicalRepo(shared)
}

// FromActionableReview is deliberately separate from the typed T1028.1
// contract. It admits only an evidence-backed, activated UI visual verdict
// with an explicit device prerequisite. A device outage alone, vague owner
// go-ahead, or proposed design is not an owner review event.
func FromActionableReview(text, workdir, asker string) (ReviewEvent, bool, error) {
	m, err := envelope.Parse(text)
	if m == nil || m.Kind != envelope.KindFinishReport || m.Status != envelope.ProgressBlocked || m.BlockClass == envelope.BlockOwnerDecision {
		return ReviewEvent{}, false, nil
	}
	if err != nil {
		return ReviewEvent{}, false, err
	}
	return actionableReviewFields(m.Target, m.SHA, m.GateID, m.Payload, m.Blocker, workdir, asker)
}

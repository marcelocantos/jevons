// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package ownerquestion defines the explicit owner-decision intake contract.
// A blocked seat is not necessarily asking the owner anything: only a typed
// blocked finish-report or a successful owner-gate record creates a question.
// Persistence and notification are separate consumers of this contract.
package ownerquestion

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/marcelocantos/jevons/internal/envelope"
)

type Lifecycle string

const (
	Open       Lifecycle = "open"
	Answered   Lifecycle = "answered"
	Superseded Lifecycle = "superseded"
	Moot       Lifecycle = "moot"
)

type Identity struct {
	Repo    string `json:"repo"` // canonical absolute repository root
	Target  string `json:"target"`
	ID      string `json:"id"`      // stable question identity within repo and target
	Version string `json:"version"` // changes when the decision being requested changes
}

type Question struct {
	Identity    Identity  `json:"identity"`
	Text        string    `json:"text"`
	Asker       string    `json:"asker"`
	AnswerRoute string    `json:"answer_route"`
	State       Lifecycle `json:"state"`
}

// CanonicalRepo resolves a caller directory to its git root where possible,
// then resolves symlinks. Non-git test/standalone directories remain their own
// roots. A report's repo slot must already name a root, not a subdirectory.
func CanonicalRepo(dir string) (string, error) {
	if !filepath.IsAbs(dir) {
		return "", fmt.Errorf("repo must be an absolute path")
	}
	root := filepath.Clean(dir)
	if out, err := exec.Command("git", "-C", root, "rev-parse", "--show-toplevel").Output(); err == nil {
		root = strings.TrimSpace(string(out))
	}
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("repo: %w", err)
	}
	st, err := os.Stat(resolved)
	if err != nil || !st.IsDir() {
		return "", fmt.Errorf("repo is not a directory: %s", resolved)
	}
	return resolved, nil
}

func (q Question) Validate() error {
	if q.State != Open && q.State != Answered && q.State != Superseded && q.State != Moot {
		return fmt.Errorf("invalid owner-question lifecycle %q", q.State)
	}
	if q.Identity.Repo == "" || q.Identity.Target == "" || q.Identity.ID == "" || q.Identity.Version == "" || strings.TrimSpace(q.Text) == "" || strings.TrimSpace(q.Asker) == "" || strings.TrimSpace(q.AnswerRoute) == "" {
		return fmt.Errorf("owner question requires repo, target, question id/version, text, asker and answer route")
	}
	root, err := CanonicalRepo(q.Identity.Repo)
	if err != nil {
		return err
	}
	if root != q.Identity.Repo {
		return fmt.Errorf("repo must be canonical root %q", root)
	}
	return nil
}

// FromOwnerGate is called only after the bullseye record operation succeeds.
// The gate is a landed-code taste decision; its stable ID and content version
// distinguish a repeat from a changed question on the same target.
func FromOwnerGate(cwd, target, text, asker string) (Question, error) {
	repo, err := CanonicalRepo(cwd)
	if err != nil {
		return Question{}, err
	}
	hash := sha256.Sum256([]byte(strings.TrimSpace(text)))
	q := Question{Identity: Identity{Repo: repo, Target: target, ID: "owner-gate", Version: hex.EncodeToString(hash[:8])}, Text: strings.TrimSpace(text), Asker: asker, AnswerRoute: "jevons_owner_gate op=answer", State: Open}
	return q, q.Validate()
}

// FromBlockedReport reads the typed pre-implementation path. No blocker
// phrase, qualified status spelling or free prose is ever an owner question.
// A malformed envelope is rejected rather than being promoted by guesswork.
func FromBlockedReport(text string) (Question, bool, error) {
	m, err := envelope.Parse(text)
	if m == nil || m.Kind != envelope.KindFinishReport || m.Status != envelope.ProgressBlocked || m.BlockClass != envelope.BlockOwnerDecision {
		return Question{}, false, nil
	}
	if err != nil {
		return Question{}, false, err
	}
	repo, err := CanonicalRepo(m.QuestionRepo)
	if err != nil {
		return Question{}, false, err
	}
	q := Question{Identity: Identity{Repo: repo, Target: m.Target, ID: m.QuestionID, Version: m.QuestionVersion}, Text: m.Question, Asker: m.QuestionAsker, AnswerRoute: m.AnswerRoute, State: Open}
	if err := q.Validate(); err != nil {
		return Question{}, false, err
	}
	return q, true, nil
}

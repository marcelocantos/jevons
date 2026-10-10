// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package landinggrant

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/worktree"
)

const approvalFence = "```jevons-integration-approval\n"

// Approval is the PO's exact, single-batch decision. It is ONLY an input
// when emitted as the complete response of the current registered PO process;
// parsing a worker message, MCP actor or copied transcript is never proof.
type Approval struct {
	Review  worktree.BatchReview `json:"review"`
	Expires time.Time            `json:"expires"`
}

// ParseApproval accepts exactly one fenced JSON object and nothing else.
// Unknown fields, trailing JSON, prose, and unscoped requests fail closed.
func ParseApproval(text string) (Approval, error) {
	var a Approval
	if !strings.HasPrefix(text, approvalFence) || !strings.HasSuffix(text, "\n```") {
		return a, errors.New("approval: entire completed response must be a single approval fence")
	}
	body := strings.TrimSuffix(strings.TrimPrefix(text, approvalFence), "\n```")
	d := json.NewDecoder(strings.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(&a); err != nil {
		return a, fmt.Errorf("approval: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return a, errors.New("approval: trailing JSON or malformed suffix")
	}
	if !validReview(a.Review) || a.Expires.IsZero() {
		return a, errors.New("approval: exact review and expiry required")
	}
	return a, nil
}

// MintApproval obtains the current hold version from the SAME authority used
// at redemption. The verifier configured on Store must prove this event came
// from the current registered PO process. Snapshot followed by Issue is safe:
// Issue rechecks the epoch under WithCurrent, so a hold between them refuses.
func (s *Store) MintApproval(a Approval, eventID string) (Grant, error) {
	var g Grant
	if s == nil || s.Epoch == nil || s.VerifyPOEvent == nil || s.SnapshotEpoch == nil || eventID == "" {
		return g, errors.New("approval: authenticated PO event and durable epoch authority required")
	}
	now := s.now()
	if !a.Expires.After(now) || a.Expires.After(now.Add(5*time.Minute)) {
		return g, errors.New("approval: expiry must be within five minutes")
	}
	epoch, held, err := s.SnapshotEpoch(a.Review.Repo, a.Review.Target)
	if err != nil {
		return g, err
	}
	if held {
		return g, errors.New("approval: target is held; explicit verified release required")
	}
	bytes := make([]byte, 32)
	if _, err = rand.Read(bytes); err != nil {
		return g, err
	}
	g = Grant{ID: hex.EncodeToString(bytes), Review: a.Review, Epoch: epoch, Expires: a.Expires, POEvent: eventID}
	if err = s.Issue(g); err != nil {
		return Grant{}, err
	}
	return g, nil
}

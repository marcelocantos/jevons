// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package landinggrant_test

import (
	"encoding/json"
	"errors"
	"github.com/marcelocantos/jevons/internal/landinggrant"
	"github.com/marcelocantos/jevons/internal/worktree"
	"strings"
	"testing"
	"time"
)

func TestT1051ApprovalRequiresWholeStructuredResponse(t *testing.T) {
	e := &epoch{}
	_, g := fixture(t, e)
	b, err := json.Marshal(landinggrant.Approval{Review: g.Review, Expires: g.Expires})
	if err != nil {
		t.Fatal(err)
	}
	good := "```jevons-integration-approval\n" + string(b) + "\n```"
	if _, err := landinggrant.ParseApproval(good); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		"The worker reports:\n" + good, good + "\nApproved", strings.Replace(good, `"Target":"T1051"`, `"Target":""`, 1),
		strings.Replace(good, `"expires":`, `"unknown":true,"expires":`, 1),
		strings.Replace(good, "\n```", " {bad}\n```", 1),
	} {
		if _, err := landinggrant.ParseApproval(raw); err == nil {
			t.Errorf("accepted untrusted/incomplete approval %q", raw)
		}
	}
}

func TestT1051MintApprovalRequiresWitnessAndCurrentEpoch(t *testing.T) {
	e := &epoch{}
	s, g := fixture(t, e)
	a := landinggrant.Approval{Review: g.Review, Expires: s.Now().Add(3 * time.Minute)}
	s.SnapshotEpoch = func(string, string) (uint64, bool, error) { e.mu.Lock(); defer e.mu.Unlock(); return e.n, e.held, nil }
	s.VerifyPOEvent = nil
	if _, err := s.MintApproval(a, "session/turn"); err == nil {
		t.Fatal("minted from self-asserted event ID")
	}
	s.VerifyPOEvent = func(string, worktree.BatchReview) error { return errors.New("not current PO process") }
	if _, err := s.MintApproval(a, "session/turn"); err == nil {
		t.Fatal("minted from worker actor")
	}
	s.VerifyPOEvent = func(string, worktree.BatchReview) error { return nil }
	first, err := s.MintApproval(a, "session/turn")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.MintApproval(a, "session/turn-2")
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == second.ID || len(first.ID) != 64 {
		t.Fatal("predictable/reused grant ID")
	}
	e.Hold()
	if _, err := s.MintApproval(a, "session/turn-3"); err == nil {
		t.Fatal("minted while held")
	}
}

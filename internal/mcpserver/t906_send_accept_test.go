// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"testing"

	"github.com/marcelocantos/jevons/internal/agenterr"
	"github.com/marcelocantos/jevons/internal/fleetintent"
)

// 🎯T906: a send accepted by a seat (OutcomeBegun — the payload became a
// turn) is evidence the SEAT took delivery, not that the PROVIDER answered
// it. Only a real reply (authored assistant text, replyFailure, jwork,
// event_push) may clear a blocked_provider fleet intent.
//
// Red against the pre-fix tree: reportSendOutcome's OutcomeBegun branch
// called s.ObserveProviderOK() on delivery alone, so a probe send accepted
// by a seat mid-outage cleared the block a few seconds before the seat's own
// next turn failed on the same wall (T885/T905 401 shape, 2026-09-29 15:00
// specimen) — an owner-notified clear/re-enter flap on every probe.
func TestT906AcceptedSendDoesNotClearHardBlock(t *testing.T) {
	dir := t.TempDir()
	store, err := fleetintent.Open(filepath.Join(dir, "fleet"))
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{}
	s.SetFleetIntentStore(store)
	n := &t406Notifier{}
	s.SetOwnerNotifier(n)

	raw := `provider refused the turn: 401 {"type":"error","error":{"type":"authentication_error","message":"OAuth access token has been revoked"}}`
	class := agenterr.ClassifyText(raw)
	if !agenterr.HardBlock(class, raw) {
		t.Fatalf("fixture must hard-block; class=%s", class)
	}
	s.ObserveProviderFailure(class, raw)
	if s.fleetIntent().FleetState() != fleetintent.BlockedProvider {
		t.Fatal("setup: want blocked_provider")
	}
	n.mu.Lock()
	notifiedBefore := n.n
	n.mu.Unlock()

	// A seat accepts a probe send: the payload became a turn (OutcomeBegun).
	res, err := s.reportSendOutcome(
		"jevons-po", "are you there?", OutcomeBegun,
		FlightInFlight, TurnEvidence{PayloadSeen: true, Detail: "payload matched in transcript"},
		false, false, nil, sendMech{},
	)
	if err != nil {
		t.Fatalf("reportSendOutcome: %v", err)
	}
	if res.Status == "" {
		t.Fatal("expected a non-empty accepted status")
	}

	if got := s.fleetIntent().FleetState(); got != fleetintent.BlockedProvider {
		t.Fatalf("accepted send cleared the hard-block: fleet=%q want blocked_provider", got)
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.n != notifiedBefore {
		t.Fatalf("accepted send re-notified the owner: n=%d want %d (no clear/re-enter flap)", n.n, notifiedBefore)
	}
}

// The mirror: authored assistant text (a real reply) still clears the block,
// so this target does not silently widen into "sends never clear it."
func TestT906RealReplyStillClearsHardBlock(t *testing.T) {
	s, _ := t406Server(t)
	raw := "You've hit your monthly spend limit"
	s.ObserveProviderFailure(agenterr.ClassifyText(raw), raw)
	if s.fleetIntent().FleetState() != fleetintent.BlockedProvider {
		t.Fatal("setup: want blocked_provider")
	}
	s.ObserveProviderOK()
	if got := s.fleetIntent().FleetState(); got != fleetintent.Working {
		t.Fatalf("real reply did not clear: fleet=%q want working", got)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/sendq"
)

// An owned attempt is not Backlog.Uncertain. The sweep's in-flight branch
// must leave the attempt and the claimable entry behind it. Reap itself
// drops that claimable entry; ClaimFrontID refusing the attempt is not
// what saves the entry behind it.
func TestT7665InFlightQueueIsNotReaped(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s, sender, up := t418Daemon(t, dir)
	s.registry = reg
	const agent = "jv-departed"
	now := time.Now().Add(-time.Hour)
	first, _, err := s.sendQueue().Append(agent, "in flight payload", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.sendQueue().Append(agent, "behind the attempt", now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := s.sendQueue().ClaimFront(agent)
	if err != nil || !ok || attempt.ID != first.ID || attempt.State != sendq.Attempting {
		t.Fatalf("claim: %+v %v %v", attempt, ok, err)
	}
	before, err := s.sendQueue().Backlog(agent)
	if err != nil || before.Uncertain != 0 || before.InFlight != 1 || before.Depth != 2 {
		t.Fatalf("before sweep = %+v %v", before, err)
	}
	s.SweepSendBacklogs()
	s.SettleHeldReceipts()
	rows, err := s.sendQueue().Snapshot(agent)
	if err != nil || len(rows) != 2 ||
		rows[0].ID != first.ID || rows[0].State != sendq.Attempting || rows[0].AttemptID != attempt.AttemptID || rows[0].Text != "in flight payload" ||
		rows[1].State != sendq.Pending || rows[1].Text != "behind the attempt" {
		t.Fatalf("sweep changed the live attempt: %+v %v", rows, err)
	}
	if got := sender.delivered(); len(got) != 0 {
		t.Fatalf("sweep delivered %v", got)
	}
	if containsLine(up.all(), "UNDELIVERED") {
		t.Fatalf("sweep discarded a live attempt: %v", up.all())
	}
}

// reapBacklogForMissingAgent drops the claimable entry behind an owned
// attempt. The sweep test above stays green only because the in-flight
// branch does not call this.
func TestT7665ReapDropsClaimableTailBehindOwnedAttempt(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s, _, up := t418Daemon(t, dir)
	s.registry = reg
	const agent = "jv-departed"
	now := time.Now().Add(-time.Hour)
	first, _, err := s.sendQueue().Append(agent, "in flight payload", now)
	if err != nil {
		t.Fatal(err)
	}
	tail, _, err := s.sendQueue().Append(agent, "behind the attempt", now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := s.sendQueue().ClaimFront(agent)
	if err != nil || !ok || attempt.ID != first.ID {
		t.Fatalf("claim: %+v %v %v", attempt, ok, err)
	}
	b, err := s.sendQueue().Backlog(agent)
	if err != nil {
		t.Fatal(err)
	}
	s.reapBacklogForMissingAgent(b, time.Now())
	rows, err := s.sendQueue().Snapshot(agent)
	if err != nil || len(rows) != 1 || rows[0].ID != attempt.ID || rows[0].State != sendq.Attempting || rows[0].Text != "in flight payload" {
		t.Fatalf("reap consumed the owned attempt or left the tail: %+v %v", rows, err)
	}
	archived, err := s.sendQueue().ReadSupersession(tail.ID)
	if err != nil || len(archived.Removed) != 1 || archived.Removed[0].Text != "behind the attempt" {
		t.Fatalf("tail archive = %+v %v", archived, err)
	}
	if _, err := s.sendQueue().ReadSupersession(attempt.ID); err == nil {
		t.Fatal("reap archived the owned attempt")
	}
	if !containsLine(up.all(), "UNDELIVERED") {
		t.Fatalf("tail drop was silent: %v", up.all())
	}
}

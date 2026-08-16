// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sendq

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The property the package exists for: a queue written by one process is read
// by the next one. The second Store is a different object over the same
// directory, which is what a daemon restart looks like from disk.
func TestQueueSurvivesTheProcessThatAcceptedIt(t *testing.T) {
	dir := t.TempDir()
	accepted := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)

	before := NewStore(filepath.Join(dir, "sendq"))
	if _, depth, err := before.Append("jv-worker", "first", accepted); err != nil || depth != 1 {
		t.Fatalf("Append = %d, %v; want depth 1, nil", depth, err)
	}
	if _, depth, err := before.Append("jv-worker", "second", accepted.Add(time.Minute)); err != nil || depth != 2 {
		t.Fatalf("Append = %d, %v; want depth 2, nil", depth, err)
	}

	after := NewStore(filepath.Join(dir, "sendq"))
	entries, err := after.Snapshot("jv-worker")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(entries) != 2 || entries[0].Text != "first" || entries[1].Text != "second" {
		t.Fatalf("recovered queue = %+v; want first,second in order", entries)
	}
	if !entries[0].EnqueuedAt.Equal(accepted) {
		t.Fatalf("EnqueuedAt = %v; want the original acceptance time %v", entries[0].EnqueuedAt, accepted)
	}
}

func TestPopFrontIsFIFOAndEmptiesTheRecord(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	at := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	for _, text := range []string{"one", "two"} {
		if _, _, err := s.Append("a", text, at); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	e, ok, err := s.PopFront("a")
	if err != nil || !ok || e.Text != "one" {
		t.Fatalf("PopFront = %q, %v, %v; want one, true, nil", e.Text, ok, err)
	}
	if _, _, err := s.PopFront("a"); err != nil {
		t.Fatalf("PopFront: %v", err)
	}
	if _, ok, err := s.PopFront("a"); ok || err != nil {
		t.Fatalf("PopFront on drained queue = %v, %v; want false, nil", ok, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.json")); !os.IsNotExist(err) {
		t.Fatalf("drained queue left a record behind: %v", err)
	}
}

// A returned entry keeps the age it was accepted with. Re-stamping it would
// reset the one number a stalled queue is visible in.
func TestPushFrontKeepsTheOriginalAge(t *testing.T) {
	s := NewStore(t.TempDir())
	accepted := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	if _, _, err := s.Append("a", "held", accepted); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, _, err := s.Append("a", "behind it", accepted.Add(time.Minute)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	e, _, err := s.PopFront("a")
	if err != nil {
		t.Fatalf("PopFront: %v", err)
	}
	if err := s.PushFront("a", e); err != nil {
		t.Fatalf("PushFront: %v", err)
	}
	entries, err := s.Snapshot("a")
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(entries) != 2 || entries[0].Text != "held" {
		t.Fatalf("queue = %+v; want the returned entry at the head", entries)
	}
	if !entries[0].EnqueuedAt.Equal(accepted) {
		t.Fatalf("EnqueuedAt = %v; want the original %v", entries[0].EnqueuedAt, accepted)
	}
}

// House rule for durable state: a malformed record is an error, never a silent
// empty queue — those are indistinguishable to every observer from delivery.
func TestMalformedRecordIsAnErrorNotAnEmptyQueue(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	s := NewStore(dir)
	if _, err := s.Snapshot("a"); err == nil {
		t.Fatal("Snapshot over a malformed record returned no error")
	}
	if depth, err := s.Depth("a"); err == nil || depth != 0 {
		t.Fatalf("Depth = %d, %v; want 0 AND an error", depth, err)
	}
}

func TestUnusableAgentNameIsRefused(t *testing.T) {
	s := NewStore(t.TempDir())
	for _, name := range []string{"", "../escape", `back\slash`, "."} {
		if _, _, err := s.Append(name, "x", time.Now()); err == nil {
			t.Fatalf("Append accepted agent name %q", name)
		}
	}
}

// Depth alone cannot tell a queue that is moving from one that has not moved
// since a rotation, so the age of the head is part of the answer.
func TestBacklogsReportDepthAndAgeOldestFirst(t *testing.T) {
	s := NewStore(t.TempDir())
	now := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	if _, _, err := s.Append("recent", "r", now.Add(-time.Minute)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, _, err := s.Append("stalled", "s1", now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, _, err := s.Append("stalled", "s2", now.Add(-time.Hour)); err != nil {
		t.Fatalf("Append: %v", err)
	}
	backlogs, err := s.Backlogs()
	if err != nil {
		t.Fatalf("Backlogs: %v", err)
	}
	if len(backlogs) != 2 || backlogs[0].Agent != "stalled" {
		t.Fatalf("Backlogs = %+v; want the oldest queue first", backlogs)
	}
	if backlogs[0].Depth != 2 || backlogs[0].OldestAge(now) != 2*time.Hour {
		t.Fatalf("stalled backlog = depth %d age %v; want 2, 2h", backlogs[0].Depth, backlogs[0].OldestAge(now))
	}
	if desc := backlogs[0].Describe(now); !strings.Contains(desc, "2 queued") || !strings.Contains(desc, "2h0m0s") {
		t.Fatalf("Describe = %q; want depth and the age of the oldest entry", desc)
	}
}

func TestClearDropsTheWholeQueue(t *testing.T) {
	s := NewStore(t.TempDir())
	if _, _, err := s.Append("gone", "orphan", time.Now()); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := s.Clear("gone"); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if depth, err := s.Depth("gone"); depth != 0 || err != nil {
		t.Fatalf("Depth after Clear = %d, %v; want 0, nil", depth, err)
	}
	names, err := s.Agents()
	if err != nil || len(names) != 0 {
		t.Fatalf("Agents = %v, %v; want empty", names, err)
	}
}

// A store with no directory is the same queue with no durability — and says so,
// so nothing reports a memory-backed backlog as having survived a restart.
func TestMemoryBackedStoreBehavesTheSameAndAdmitsItIsNotDurable(t *testing.T) {
	s := NewStore("")
	if s.Durable() {
		t.Fatal("a directoryless store claims to be durable")
	}
	at := time.Date(2026, 8, 15, 9, 0, 0, 0, time.UTC)
	if _, depth, err := s.Append("a", "held", at); err != nil || depth != 1 {
		t.Fatalf("Append = %d, %v; want 1, nil", depth, err)
	}
	backlogs, err := s.Backlogs()
	if err != nil || len(backlogs) != 1 || backlogs[0].Depth != 1 {
		t.Fatalf("Backlogs = %+v, %v; want one queue of depth 1", backlogs, err)
	}
	e, ok, err := s.PopFront("a")
	if err != nil || !ok || e.Text != "held" {
		t.Fatalf("PopFront = %q, %v, %v; want held, true, nil", e.Text, ok, err)
	}
	if names, err := s.Agents(); err != nil || len(names) != 0 {
		t.Fatalf("Agents after drain = %v, %v; want empty", names, err)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sendq

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAttemptSurvivesRestartWithoutAnotherClaim(t *testing.T) {
	dir := t.TempDir()
	q := NewStore(dir)
	original, _, err := q.Append("a", "accepted payload", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	attempt, ok, err := q.ClaimFront("a")
	if err != nil || !ok {
		t.Fatalf("claim: %+v %v %v", attempt, ok, err)
	}
	after := NewStore(dir)
	held, ok, err := after.ClaimFront("a")
	if err != nil || ok || held != attempt || held.ID != original.ID || held.Text != original.Text {
		t.Fatalf("restart changed or replayed attempt: %+v %v %v", held, ok, err)
	}
	if _, _, err := after.PopFront("a"); err == nil {
		t.Fatal("legacy pop bypassed unresolved attempt")
	}
	if err := after.PushFront("a", original); err == nil {
		t.Fatal("legacy prepend bypassed unresolved attempt")
	}
	if err := after.Resolve("a", attempt, Unverified, "no receiver evidence"); err != nil {
		t.Fatal(err)
	}
	backlogs, err := after.Backlogs()
	if err != nil || len(backlogs) != 1 || backlogs[0].Uncertain != 1 {
		t.Fatalf("uncertain backlog: %+v %v", backlogs, err)
	}
}

func TestAttemptTokensRejectLateResultsAndPreserveNewArrivals(t *testing.T) {
	for _, dir := range []string{"", t.TempDir()} {
		q := NewStore(dir)
		first, _, err := q.Append("a", "first", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		old, _, err := q.ClaimFront("a")
		if err != nil {
			t.Fatal(err)
		}
		if err := q.Resolve("a", old, DefinitelyNotSent, "busy before write"); err != nil {
			t.Fatal(err)
		}
		current, ok, err := q.ClaimFront("a")
		if err != nil || !ok || current.ID != first.ID || current.AttemptID == old.AttemptID || !current.EnqueuedAt.Equal(first.EnqueuedAt) {
			t.Fatalf("retry changed identity/age or reused token: %+v %v %v", current, ok, err)
		}
		if err := q.Resolve("a", old, Confirmed, "late result"); err == nil {
			t.Fatal("stale result consumed current attempt")
		}
		second, _, err := q.Append("a", "new arrival", time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if err := q.Resolve("a", current, Confirmed, "receiver record"); err != nil {
			t.Fatal(err)
		}
		entries, err := q.Snapshot("a")
		if err != nil || len(entries) != 1 || entries[0] != second {
			t.Fatalf("resolution erased new arrival: %+v %v", entries, err)
		}
		entries[0].Text = "mutated snapshot"
		entries, _ = q.Snapshot("a")
		if entries[0] != second {
			t.Fatal("snapshot aliases queue storage")
		}
		claimed, _, _ := q.ClaimFront("a")
		if _, err := q.Discard("a"); err != nil {
			t.Fatal(err)
		}
		if err := q.Resolve("a", claimed, DefinitelyNotSent, "late result after authorized discard"); err == nil {
			t.Fatal("late result resurrected explicitly discarded entry")
		}
	}
}

func TestOnlyOneConcurrentDrainClaimsTheHead(t *testing.T) {
	q := NewStore(t.TempDir())
	if _, _, err := q.Append("a", "one payload", time.Now()); err != nil {
		t.Fatal(err)
	}
	var claims atomic.Int32
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			_, ok, err := q.ClaimFront("a")
			if err != nil {
				t.Error(err)
			}
			if ok {
				claims.Add(1)
			}
		})
	}
	wg.Wait()
	if claims.Load() != 1 {
		t.Fatalf("%d drains claimed one message", claims.Load())
	}
}

func TestMalformedAttemptCannotBecomePending(t *testing.T) {
	for _, body := range []string{
		`{"entries":[{"id":"a","text":"payload","state":"surprise"}]}`,
		`{"entries":[{"id":"a","text":"payload","state":"attempting"}]}`,
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := NewStore(dir).ClaimFront("a"); err == nil {
			t.Fatal("malformed attempt was replayable")
		}
	}
}

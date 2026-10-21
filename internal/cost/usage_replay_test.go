// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package cost

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/claudiawire"
)

func completeInventory(generations ...claudiawire.GenerationIdentity) *claudiawire.GenerationPage {
	if generations == nil {
		generations = []claudiawire.GenerationIdentity{}
	}
	return &claudiawire.GenerationPage{Generations: generations, Issues: []claudiawire.GenerationIssue{}, Complete: true}
}

func TestHistoryCatchUpWithoutSpoolOrRegistrySurvivesRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "usage.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	start, final := usagePair("lost-spool", 1)
	queries := 0
	source := &UsageSource{
		Discover: func(context.Context, claudiawire.GenerationQuery) (*claudiawire.GenerationPage, error) {
			return completeInventory(claudiawire.GenerationIdentity{Seat: start.Seat, SessionID: start.SessionID}), nil
		},
		Query: func(_ context.Context, q claudiawire.UsageQuery) (*claudiawire.UsagePage, error) {
			queries++
			page := &claudiawire.UsagePage{Seat: q.Seat, SessionID: q.SessionID, Facts: []claudiawire.UsageFact{}, Next: q.After}
			if q.After == 0 {
				page.Facts, page.Next = []claudiawire.UsageFact{start, final}, final.Seq
			}
			return page, nil
		},
	}
	c := NewCollector(&CollectorArgs{Store: s, UsageSource: source})
	if err := c.CatchUpOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	// Discovery now fails: the durable generation still gets queried.
	source.Discover = func(context.Context, claudiawire.GenerationQuery) (*claudiawire.GenerationPage, error) {
		return nil, errors.New("inventory unavailable")
	}
	c = NewCollector(&CollectorArgs{Store: s, UsageSource: source})
	if err := c.CatchUpOnce(ctx); err == nil {
		t.Fatal("missing discovery was hidden")
	}
	if queries != 2 {
		t.Fatalf("restarted consumer queries=%d", queries)
	}
	got, err := s.StructuredUsage(ctx, testNow, testNow.Add(time.Hour))
	if err != nil || got.Calls != 1 || got.KnownConsumptionTokens() != 25 {
		t.Fatalf("replay doubled or lost usage: %+v %v", got, err)
	}
	if c.Health().InventoryIncomplete != true || c.Health().LastHistoryErr == "" {
		t.Fatal("missing inventory coverage hidden")
	}
	if _, err := c.PollOnce(); err != nil {
		t.Fatal(err)
	}
	monitor := NewMonitor(&MonitorArgs{Store: s, Config: DefaultBudgetConfig, CollectorHealth: c.Health})
	snapshot, err := monitor.Snapshot()
	if err != nil || snapshot.Collector == nil || snapshot.Collector.LastHistoryErr == "" || snapshot.Collector.LastPoll.IsZero() {
		t.Fatalf("healthy spool poll hid missing history in API snapshot: %+v %v", snapshot, err)
	}
}

func TestHistoryFailureRotatesDurablyAndRemainsVisibleUntilRepair(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "usage.db")
	s, err := OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	var identities []claudiawire.GenerationIdentity
	for i := 0; i < usageReplayBatch+1; i++ {
		identities = append(identities, claudiawire.GenerationIdentity{Seat: fmt.Sprintf("seat-%02d", i), SessionID: "generation"})
	}
	broken := true
	var seen []string
	source := &UsageSource{
		Discover: func(context.Context, claudiawire.GenerationQuery) (*claudiawire.GenerationPage, error) {
			return completeInventory(identities...), nil
		},
		Query: func(_ context.Context, q claudiawire.UsageQuery) (*claudiawire.UsagePage, error) {
			seen = append(seen, q.Seat)
			if broken && q.Seat == identities[0].Seat {
				return nil, errors.New("history unavailable")
			}
			return &claudiawire.UsagePage{Seat: q.Seat, SessionID: q.SessionID, Facts: []claudiawire.UsageFact{}, Next: q.After}, nil
		},
	}
	c := NewCollector(&CollectorArgs{Store: s, UsageSource: source})
	if err := c.CatchUpOnce(ctx); err == nil {
		t.Fatal("failure hidden")
	}
	if len(seen) != usageReplayBatch {
		t.Fatalf("unbounded pass: %v", seen)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = OpenStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	c = NewCollector(&CollectorArgs{Store: s, UsageSource: source})
	if err := c.CatchUpOnce(ctx); err == nil || !strings.Contains(err.Error(), "still have replay errors") {
		t.Fatalf("previous failure disappeared: %v", err)
	}
	if seen[len(seen)-1] != identities[usageReplayBatch].Seat {
		t.Fatalf("restart starved last generation: %v", seen)
	}
	broken = false
	if err := c.CatchUpOnce(ctx); err != nil {
		t.Fatalf("repair failed: %v", err)
	}
	if c.Health().LastHistoryPass.IsZero() || c.Health().LastHistoryErr != "" {
		t.Fatal("repair not reflected")
	}
}

func TestHistoryCursorExpiryRestartsDiscoveryWithoutLosingUsageCursor(t *testing.T) {
	ctx := context.Background()
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	start, final := usagePair("call", 1)
	var cursors []string
	source := &UsageSource{
		Discover: func(_ context.Context, q claudiawire.GenerationQuery) (*claudiawire.GenerationPage, error) {
			cursors = append(cursors, q.Cursor)
			if len(cursors) == 2 {
				return nil, &claudiawire.GenerationDiscoveryError{Code: claudiawire.DiscoveryCursorExpired}
			}
			return &claudiawire.GenerationPage{Generations: []claudiawire.GenerationIdentity{{Seat: start.Seat, SessionID: start.SessionID}}, Issues: []claudiawire.GenerationIssue{}, More: true, Cursor: "next-page"}, nil
		},
		Query: func(_ context.Context, q claudiawire.UsageQuery) (*claudiawire.UsagePage, error) {
			page := &claudiawire.UsagePage{Seat: q.Seat, SessionID: q.SessionID, Facts: []claudiawire.UsageFact{}, Next: q.After}
			if q.After == 0 {
				page.Facts, page.Next = []claudiawire.UsageFact{start, final}, 2
			}
			return page, nil
		},
	}
	c := NewCollector(&CollectorArgs{Store: s, UsageSource: source})
	for i := 0; i < 3; i++ {
		err := c.CatchUpOnce(ctx)
		if (err != nil) != (i == 1) {
			t.Fatalf("pass %d: %v", i, err)
		}
	}
	if fmt.Sprint(cursors) != "[ next-page ]" {
		t.Fatalf("did not restart cursor: %#v", cursors)
	}
	if seq, err := s.UsageCursor(ctx, start.Seat, start.SessionID); err != nil || seq != 2 {
		t.Fatalf("lost replay progress: %d %v", seq, err)
	}
}

func TestSlowHistoryDoesNotBlockSpoolPollOrHoldDatabase(t *testing.T) {
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	entered := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := NewCollector(&CollectorArgs{Store: s, UsageSource: &UsageSource{
		Discover: func(ctx context.Context, _ claudiawire.GenerationQuery) (*claudiawire.GenerationPage, error) {
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > usageHistoryTimeout {
				t.Error("unbounded discovery")
			}
			close(entered)
			<-ctx.Done()
			return nil, ctx.Err()
		},
		Query: func(context.Context, claudiawire.UsageQuery) (*claudiawire.UsagePage, error) {
			panic("unexpected query")
		},
	}})
	done := make(chan error, 1)
	go func() { done <- c.CatchUpOnce(ctx) }()
	<-entered
	if err := c.CatchUpOnce(context.Background()); err == nil {
		t.Fatal("parallel replay admitted")
	}
	pollDone := make(chan error, 1)
	go func() { _, err := c.PollOnce(); pollDone <- err }()
	select {
	case err := <-pollDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("history blocked spool")
	}
	start, final := usagePair("live", 1)
	storeCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := s.IngestUsageFact(storeCtx, start.Seat, start.SessionID, &final); err != nil {
		t.Fatalf("history held database: %v", err)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancellation hidden")
		}
	case <-time.After(time.Second):
		t.Fatal("history did not cancel")
	}
}

func TestBadHistoryPagesDoNotAdvanceReplayOrDiscovery(t *testing.T) {
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	start, _ := usagePair("call", 1)
	if err := s.IngestUsageFact(context.Background(), start.Seat, start.SessionID, &start); err != nil {
		t.Fatal(err)
	}
	c := NewCollector(&CollectorArgs{Store: s, UsageSource: &UsageSource{
		Discover: func(context.Context, claudiawire.GenerationQuery) (*claudiawire.GenerationPage, error) {
			return &claudiawire.GenerationPage{Cursor: "bad", More: false}, nil
		},
		Query: func(_ context.Context, q claudiawire.UsageQuery) (*claudiawire.UsagePage, error) {
			return &claudiawire.UsagePage{Seat: "other", SessionID: q.SessionID, Next: 100}, nil
		},
	}})
	if err := c.CatchUpOnce(context.Background()); err == nil {
		t.Fatal("invalid page accepted")
	}
	if c.discoveryCursor != "" {
		t.Fatal("invalid discovery advanced")
	}
	if seq, err := s.UsageCursor(context.Background(), start.Seat, start.SessionID); err != nil || seq != 0 {
		t.Fatalf("invalid replay advanced: %d %v", seq, err)
	}
}

func TestCollectorRunReachesHistoryWhilePollingIndependently(t *testing.T) {
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	entered, historyExited := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := NewCollector(&CollectorArgs{Store: s, UsageSource: &UsageSource{
		Discover: func(ctx context.Context, _ claudiawire.GenerationQuery) (*claudiawire.GenerationPage, error) {
			close(entered)
			<-ctx.Done()
			close(historyExited)
			return nil, ctx.Err()
		},
		Query: func(context.Context, claudiawire.UsageQuery) (*claudiawire.UsagePage, error) {
			panic("unexpected query")
		},
	}})
	c.scan = func() ([]string, error) { return nil, nil }
	done := make(chan struct{})
	go func() { c.Run(ctx, time.Hour, time.Millisecond); close(done) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("Run never reached history source")
	}
	deadline := time.After(time.Second)
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for c.LastPoll().IsZero() {
		select {
		case <-ticker.C:
		case <-deadline:
			t.Fatal("blocked history starved polling")
		}
	}
	cancel()
	select {
	case <-historyExited:
	case <-time.After(time.Second):
		t.Fatal("history RPC was not canceled")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("collector did not stop")
	}
	// Wait for the pass to finish using the database before closing it.
	c.replayMu.Lock()
	c.replayMu.Unlock()
}

func TestCollectorBacksOffDiscoveryBusyWithoutStoppingReplay(t *testing.T) {
	ctx := context.Background()
	s, err := OpenStore(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	start, final := usagePair("live", 1)
	if err := s.IngestUsageFact(ctx, start.Seat, start.SessionID, &final); err != nil {
		t.Fatal(err)
	}
	now := testNow
	discoveries, queries := 0, 0
	c := NewCollector(&CollectorArgs{Store: s, Now: func() time.Time { return now }, UsageSource: &UsageSource{
		Discover: func(context.Context, claudiawire.GenerationQuery) (*claudiawire.GenerationPage, error) {
			discoveries++
			return nil, &claudiawire.GenerationDiscoveryError{Code: claudiawire.DiscoveryBusy}
		},
		Query: func(_ context.Context, q claudiawire.UsageQuery) (*claudiawire.UsagePage, error) {
			queries++
			page := &claudiawire.UsagePage{Seat: q.Seat, SessionID: q.SessionID, Facts: []claudiawire.UsageFact{}, Next: q.After}
			if q.After == 0 {
				page.Facts, page.Next = []claudiawire.UsageFact{start, final}, 2
			}
			return page, nil
		},
	}})
	for i := 0; i < 3; i++ {
		err := c.CatchUpOnce(ctx)
		if (err != nil) != (i == 0) {
			t.Fatalf("pass %d: %v", i, err)
		}
		now = now.Add(DefaultPollInterval)
	}
	if discoveries != 1 || queries != 3 || !c.Health().InventoryIncomplete {
		t.Fatalf("busy backoff lost replay or coverage: discovery=%d query=%d %+v", discoveries, queries, c.Health())
	}
	now = testNow.Add(usageInventoryInterval)
	if err := c.CatchUpOnce(ctx); err == nil || discoveries != 2 {
		t.Fatalf("busy backoff never retried: %d %v", discoveries, err)
	}
}

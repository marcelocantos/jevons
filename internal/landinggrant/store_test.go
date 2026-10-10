// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package landinggrant_test

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/landinggrant"
	"github.com/marcelocantos/jevons/internal/worktree"
)

type epoch struct {
	mu      sync.Mutex
	n       uint64
	held    bool
	entered chan struct{}
}

func (e *epoch) WithCurrent(repo, target string, n uint64, action func() error) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.held || e.n != n {
		return errors.New("stale or held")
	}
	if e.entered != nil {
		close(e.entered)
		e.entered = nil
	}
	return action()
}
func (e *epoch) Hold()    { e.mu.Lock(); defer e.mu.Unlock(); e.n++; e.held = true }
func (e *epoch) Release() { e.mu.Lock(); defer e.mu.Unlock(); e.n++; e.held = false }
func fixture(t *testing.T, e *epoch) (*landinggrant.Store, landinggrant.Grant) {
	t.Helper()
	dir := t.TempDir()
	s := &landinggrant.Store{Dir: dir, Epoch: e, VerifyPOEvent: func(string, worktree.BatchReview) error { return nil }, Actor: "integrator", Command: "integrate -repo x one", Now: func() time.Time { return time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC) }}
	r := worktree.BatchReview{Repo: "/repo", Target: "T1051", BaseRef: "refs/heads/master", BaseSHA: "base", Workers: []worktree.ReviewedWorker{{Name: "one", Ref: "refs/heads/jevons-worktree/one", SHA: "tip"}}}
	return s, landinggrant.Grant{ID: "g1", Review: r, Epoch: e.n, POEvent: "observed-completed-PO-turn-1", Expires: s.Now().Add(time.Hour)}
}
func TestT1051GrantExactOneUseAndDurable(t *testing.T) {
	e := &epoch{}
	s, g := fixture(t, e)
	if err := s.Issue(g); err != nil {
		t.Fatal(err)
	}
	landed := 0
	if err := s.RedeemAndLand(g.ID, g.Review, func() error { landed++; return nil }); err != nil {
		t.Fatal(err)
	}
	s2 := &landinggrant.Store{Dir: s.Dir, Epoch: e, Now: s.Now}
	if err := s2.RedeemAndLand(g.ID, g.Review, func() error { landed++; return nil }); err == nil {
		t.Fatal("replay accepted")
	}
	if landed != 1 {
		t.Fatalf("landed %d times", landed)
	}
	b, err := os.ReadFile(filepath.Join(s.Dir, "audit.jsonl"))
	if err != nil || len(b) == 0 {
		t.Fatalf("audit: %v", err)
	}
}
func TestT1051HoldBeforeRedemptionRefuses(t *testing.T) {
	e := &epoch{}
	s, g := fixture(t, e)
	if err := s.Issue(g); err != nil {
		t.Fatal(err)
	}
	e.Hold()
	if err := s.RedeemAndLand(g.ID, g.Review, func() error { t.Fatal("landed after hold"); return nil }); err == nil {
		t.Fatal("hold accepted old grant")
	}
	e.Release()
	if err := s.RedeemAndLand(g.ID, g.Review, func() error { t.Fatal("old epoch landed"); return nil }); err == nil {
		t.Fatal("released hold revived stale grant")
	}
}
func TestT1051HoldCannotBeAcceptedBetweenSpendAndLanding(t *testing.T) {
	e := &epoch{}
	s, g := fixture(t, e)
	if err := s.Issue(g); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	continueLand := make(chan struct{})
	done := make(chan error, 1)
	held := make(chan struct{})
	go func() {
		done <- s.RedeemAndLand(g.ID, g.Review, func() error { close(entered); <-continueLand; return nil })
	}()
	<-entered
	go func() { e.Hold(); close(held) }()
	select {
	case <-held:
		t.Fatal("hold accepted while earlier landing was in-flight")
	case <-time.After(30 * time.Millisecond):
	}
	close(continueLand)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	select {
	case <-held:
	case <-time.After(time.Second):
		t.Fatal("hold never accepted")
	}
	if err := s.RedeemAndLand(g.ID, g.Review, func() error { t.Fatal("landed after hold"); return nil }); err == nil {
		t.Fatal("post-hold replay accepted")
	}
}
func TestT1051GrantUnavailableAuthorityFailsClosed(t *testing.T) {
	e := &epoch{}
	s, g := fixture(t, e)
	s.Epoch = nil
	if err := s.Issue(g); err == nil {
		t.Fatal("mint without epoch authority")
	}
	if err := s.RedeemAndLand(g.ID, g.Review, func() error { t.Fatal("landed"); return nil }); !errors.Is(err, worktree.ErrNoAuthority) {
		t.Fatal(err)
	}
}

func TestT1051MintRejectsSelfAssertedPOEventAndStaleEpoch(t *testing.T) {
	e := &epoch{}
	s, g := fixture(t, e)
	s.VerifyPOEvent = nil
	if err := s.Issue(g); err == nil {
		t.Fatal("minted without current PO process witness")
	}
	s.VerifyPOEvent = func(string, worktree.BatchReview) error { return errors.New("event came from worker") }
	if err := s.Issue(g); err == nil {
		t.Fatal("forged event minted grant")
	}
	s.VerifyPOEvent = func(string, worktree.BatchReview) error { return nil }
	e.Hold()
	if err := s.Issue(g); err == nil {
		t.Fatal("stale approval minted through hold")
	}
}

func TestT1051ConcurrentRedemptionSingleSpend(t *testing.T) {
	e := &epoch{}
	s, g := fixture(t, e)
	if err := s.Issue(g); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	successes := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.RedeemAndLand(g.ID, g.Review, func() error { successes <- struct{}{}; return nil }); err == nil {
				return
			}
		}()
	}
	wg.Wait()
	close(successes)
	if len(successes) != 1 {
		t.Fatalf("landed %d times", len(successes))
	}
}

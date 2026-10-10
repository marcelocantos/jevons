// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package landingauth

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestT1050T1048ChronologyStaleDeliveredGrant(t *testing.T) {
	repo := t.TempDir()
	dir := t.TempDir()
	s := NewStore(dir)
	before, err := s.Snapshot(repo, "T1048")
	if err != nil || before.Epoch != 0 || before.Held {
		t.Fatalf("initial: %+v %v", before, err)
	}
	// Corrected in-progress worker report -> automatic continuation with
	// generic integrator wording -> worker invocation. None is PO approval.
	history := []string{"corrected in-progress report", "automatic continuation: integrate", "worker invokes integrator"}
	if len(history) != 3 {
		t.Fatal("broken chronology")
	}
	// The permission message was already delivered; it is not in sendq.
	held, err := s.AcceptHold(repo, "T1048", "typed-hold-1048", "do not integrate T1048", time.Now())
	if err != nil || !held.Held || held.Epoch != 1 {
		t.Fatalf("hold: %+v %v", held, err)
	}
	restarted := NewStore(dir)
	var mutated bool
	err = restarted.WithCurrent(repo, "T1048", before.Epoch, func() error { mutated = true; return nil })
	if err == nil || mutated || !strings.Contains(err.Error(), "typed-hold-1048") {
		t.Fatalf("stale delivered grant: mutated=%t err=%v", mutated, err)
	}
	// A new grant cannot bypass a hold simply by recording the new epoch.
	err = restarted.WithCurrent(repo, "T1048", held.Epoch, func() error { mutated = true; return nil })
	if err == nil || mutated {
		t.Fatalf("held epoch permitted mutation: %v", err)
	}
	after, err := restarted.ReleaseHold(repo, "T1048", "verified-po-event", time.Now())
	if err != nil || after.Held || after.Epoch != 2 {
		t.Fatalf("release: %+v %v", after, err)
	}
	if err := restarted.WithCurrent(repo, "T1048", after.Epoch, func() error { mutated = true; return nil }); err != nil || !mutated {
		t.Fatalf("reviewed release: %v", err)
	}
	if err := restarted.WithCurrent(repo, "T1048", held.Epoch, func() error { t.Fatal("pre-release epoch reused"); return nil }); err == nil {
		t.Fatal("old epoch accepted")
	}
}

func TestT1050AtomicActionAndTargetIsolation(t *testing.T) {
	repo := t.TempDir()
	s := NewStore(t.TempDir())
	other := NewStore(s.dir)
	entered, release := make(chan struct{}), make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := s.WithCurrent(repo, "T1050", 0, func() error { close(entered); <-release; return nil }); err != nil {
			t.Error(err)
		}
	}()
	<-entered
	done := make(chan error, 1)
	go func() { _, err := other.AcceptHold(repo, "T1050", "hold", "stop", time.Now()); done <- err }()
	select {
	case err := <-done:
		t.Fatalf("hold crossed protected action: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	wg.Wait()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := s.WithCurrent(repo, "T1050", 0, func() error { t.Fatal("stale action"); return nil }); err == nil {
		t.Fatal("stale grant accepted")
	}
	if err := s.WithCurrent(repo, "T1051", 0, func() error { return nil }); err != nil {
		t.Fatalf("target isolation: %v", err)
	}
}

// Simulates a grant already spent but not yet advanced to the shared ref.
// A hold attempted at exactly that seam must wait; a post-accepted-hold
// landing would violate the safety boundary even if the spend was valid.
func TestT1050HoldCannotSlipBetweenSpendAndRefMutation(t *testing.T) {
	repo := t.TempDir()
	state := t.TempDir()
	auth := NewStore(state)
	holdWriter := NewStore(state)
	ref := filepath.Join(repo, "HEAD")
	if err := os.WriteFile(ref, []byte("base"), 0600); err != nil {
		t.Fatal(err)
	}
	spent, proceed := make(chan struct{}), make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- auth.WithCurrent(repo, "T1048", 0, func() error {
			// Durable spend precedes this point in T1051's callback.
			close(spent)
			<-proceed
			return os.WriteFile(ref, []byte("landed"), 0600)
		})
	}()
	<-spent
	holdResult := make(chan error, 1)
	go func() {
		_, err := holdWriter.AcceptHold(repo, "T1048", "hold-after-spend", "stop", time.Now())
		holdResult <- err
	}()
	select {
	case err := <-holdResult:
		t.Fatalf("hold accepted before bounded ref mutation completed: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	b, err := os.ReadFile(ref)
	if err != nil || string(b) != "base" {
		t.Fatalf("premature mutation: %s %v", b, err)
	}
	close(proceed)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	if err := <-holdResult; err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(ref)
	if err != nil || string(b) != "landed" {
		t.Fatalf("ref advance: %s %v", b, err)
	}
	if err := auth.WithCurrent(repo, "T1048", 0, func() error { t.Fatal("stale redemption"); return nil }); err == nil {
		t.Fatal("post-hold stale permission accepted")
	}
}

func TestT1050CorruptAndUnavailableStoreFailClosed(t *testing.T) {
	repo := t.TempDir()
	dir := t.TempDir()
	s := NewStore(dir)
	if err := os.WriteFile(filepath.Join(dir, "epochs.json"), []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.WithCurrent(repo, "T1050", 0, func() error { t.Fatal("corrupt store ran action"); return nil }); err == nil {
		t.Fatal("corrupt epochs allowed")
	}
	if _, err := s.AcceptHold(repo, "T1050", "hold", "stop", time.Now()); err == nil {
		t.Fatal("corrupt epochs overwritten")
	}
	missing := NewStore(filepath.Join(dir, "epochs.json", "impossible"))
	if err := missing.WithCurrent(repo, "T1050", 0, func() error { t.Fatal("unavailable store ran action"); return nil }); err == nil {
		t.Fatal("unavailable store allowed")
	}
}

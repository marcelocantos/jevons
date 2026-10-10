package sendq

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDirectiveSupersessionAcrossRestartAndIsolation(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	at := time.Now()
	auth := Directive{Family: "T1050", Kind: "authorization"}
	hold := Directive{Family: "T1050", Kind: "hold"}
	first, _, _, err := s.ApplyDirective("worker", "permission one", auth, true, at)
	if err != nil {
		t.Fatal(err)
	}
	second, _, _, err := s.ApplyDirective("worker", "permission two", auth, true, at.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	s.Append("worker", "unrelated FIFO", at)
	s.ApplyDirective("worker", "other family", Directive{Family: "other", Kind: "authorization"}, true, at)
	s.ApplyDirective("other-worker", "other recipient", auth, true, at)
	h, depth, removed, err := s.ApplyDirective("worker", "stop", hold, true, at.Add(2*time.Second))
	if err != nil || depth != 3 || len(removed) != 2 || removed[0].ID != first.ID || removed[1].ID != second.ID {
		t.Fatalf("hold: depth=%d removed=%+v err=%v", depth, removed, err)
	}
	s = NewStore(dir)
	a, err := s.ReadSupersession(h.ID)
	if err != nil || len(a.Removed) != 2 || a.Removed[0].Text != "permission one" {
		t.Fatalf("archive: %+v %v", a, err)
	}
	entries, err := s.Snapshot("worker")
	if err != nil || len(entries) != 3 || entries[0].Text != "unrelated FIFO" || entries[1].Text != "other family" || entries[2].Text != "stop" {
		t.Fatalf("queue: %+v %v", entries, err)
	}
	other, _ := s.Snapshot("other-worker")
	if len(other) != 1 {
		t.Fatalf("recipient affected: %+v", other)
	}
	_, _, _, err = s.ApplyDirective("worker", "new permission", auth, true, at.Add(3*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	entries, _ = s.Snapshot("worker")
	if len(entries) != 4 || entries[3].Text != "new permission" {
		t.Fatalf("hold-before-auth: %+v", entries)
	}
}

func TestDirectiveAttemptRaceAndConcurrentDrain(t *testing.T) {
	s := NewStore(t.TempDir())
	auth := Directive{Family: "job", Kind: "authorization"}
	hold := Directive{Family: "job", Kind: "hold"}
	_, _, _, err := s.ApplyDirective("w", "auth", auth, true, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	e, ok, err := s.ClaimFront("w")
	if err != nil || !ok {
		t.Fatalf("claim %v %v", ok, err)
	}
	if _, _, _, err = s.ApplyDirective("w", "hold", hold, true, time.Now()); err == nil {
		t.Fatal("hold must fail closed against in-flight authorization")
	}
	s.Resolve("w", e, DefinitelyNotSent, "test")
	// Concurrent claim and hold have two valid linearizations: either the
	// authorization was claimed first and hold refuses, or hold removed it.
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("w%d", i)
		s.ApplyDirective(name, "auth", auth, true, time.Now())
		var wg sync.WaitGroup
		wg.Add(2)
		var claimed bool
		var claimedAuth bool
		var holdErr error
		go func() {
			defer wg.Done()
			e, ok, _ := s.ClaimFront(name)
			claimed = ok
			claimedAuth = ok && e.Text == "auth"
		}()
		go func() { defer wg.Done(); _, _, _, holdErr = s.ApplyDirective(name, "hold", hold, true, time.Now()) }()
		wg.Wait()
		if claimedAuth && holdErr == nil {
			t.Fatalf("authorization claimed and hold falsely accepted at %d", i)
		}
		if !claimed && holdErr != nil {
			t.Fatalf("neither progressed at %d: %v", i, holdErr)
		}
	}
}

func TestDirectiveDigestDoesNotEraseIdentity(t *testing.T) {
	s := NewStore(t.TempDir())
	d := Directive{Family: "x", Kind: "authorization"}
	for i := 0; i < 7; i++ {
		s.Append("w", fmt.Sprint(i), time.Now())
	}
	s.ApplyDirective("w", "auth", d, true, time.Now())
	_, ok, err := s.ClaimDigest("w")
	if err != nil || ok {
		t.Fatalf("digest swallowed typed auth: %v %v", ok, err)
	}
}

func TestDirectiveFailedQueueSaveDoesNotAssertSupersession(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	auth := Directive{Family: "incident", Kind: "authorization"}
	hold := Directive{Family: "incident", Kind: "hold"}
	s.ApplyDirective("worker", "old permission", auth, true, time.Now())
	// Force save to fail only after the prepared archive was persisted.
	if err := os.Mkdir(filepath.Join(dir, "worker.json.tmp"), 0755); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := s.ApplyDirective("worker", "hold", hold, true, time.Now())
	if err == nil {
		t.Fatal("write failure accepted hold")
	}
	entries, _ := NewStore(dir).Snapshot("worker")
	if len(entries) != 1 || entries[0].Text != "old permission" {
		t.Fatalf("failed transaction changed queue: %+v", entries)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "supersessions", "*.json"))
	if len(files) != 1 {
		t.Fatalf("expected prepared audit, got %v", files)
	}
	id := strings.TrimSuffix(filepath.Base(files[0]), ".json")
	if a, err := NewStore(dir).ReadSupersession(id); err == nil || a.State != "prepared" {
		t.Fatalf("false committed disposition: %+v %v", a, err)
	}
}

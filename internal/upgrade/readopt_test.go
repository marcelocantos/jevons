// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package upgrade

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/agenterr"
)

const notOwnerText = `broker protocol: not_owner (name="jevons"): grant jevons is not owned by this connection`

// ProtocolError is the shape of claudia's broker ProtocolError.
// The production type lives in claudia's broker package, which this
// module does not import. Code is the machine code. The sentence is not.
type ProtocolError struct {
	Code string
}

func (e *ProtocolError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == "not_owner" {
		return notOwnerText
	}
	return "broker protocol: " + e.Code
}

func notOwnerErr() error { return &ProtocolError{Code: "not_owner"} }

// fakeSeat models the registry + a broker handle: the stale handle is alive
// until detached, adopt returns a fresh handle and registers it.
type fakeSeat struct {
	mu       sync.Mutex
	cur      *claudia.Agent
	detached map[*claudia.Agent]bool
	adopts   atomic.Int32
	adoptErr error
	slept    []time.Duration
	clock    time.Time
}

func newFakeSeat() (*fakeSeat, *Readopter) {
	f := &fakeSeat{cur: &claudia.Agent{}, detached: map[*claudia.Agent]bool{}, clock: time.Unix(1_000_000, 0)}
	r := &Readopter{
		Gap: time.Second, MaxGap: 8 * time.Second, Window: time.Minute, MaxAttempts: 3,
		get: func(string) *claudia.Agent { f.mu.Lock(); defer f.mu.Unlock(); return f.cur },
		adopt: func(string) (*claudia.Agent, error) {
			f.adopts.Add(1)
			if f.adoptErr != nil {
				return nil, f.adoptErr
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			f.cur = &claudia.Agent{}
			return f.cur, nil
		},
		detach: func(a *claudia.Agent) error { f.mu.Lock(); f.detached[a] = true; f.mu.Unlock(); return nil },
		alive:  func(a *claudia.Agent) bool { f.mu.Lock(); defer f.mu.Unlock(); return !f.detached[a] },
		now:    func() time.Time { f.mu.Lock(); defer f.mu.Unlock(); return f.clock },
		sleep: func(_ context.Context, d time.Duration) error {
			f.mu.Lock()
			f.slept = append(f.slept, d)
			f.clock = f.clock.Add(d)
			f.mu.Unlock()
			return nil
		},
	}
	return f, r
}

func TestT796IsNotOwnerClassification(t *testing.T) {
	raw := errors.New(notOwnerText)
	if IsNotOwner(raw) || IsNotOwner(agenterr.NoteBrokerGrant(raw)) {
		t.Fatal("the broker sentence is not a grant refusal")
	}
	noted := agenterr.NoteBrokerGrant(notOwnerErr())
	if !IsNotOwner(noted) || !strings.Contains(noted.Error(), "not_owner") {
		t.Fatal("the boundary must type the protocol code and keep its text")
	}
	if IsNotOwner(agenterr.NoteBrokerGrant(noted)) != true || agenterr.NoteBrokerGrant(noted) != noted {
		t.Fatal("a second boundary read must keep the typed refusal")
	}
	for _, e := range []error{nil, errors.New("prompt already in flight"), errors.New("overseer not running"), &ProtocolError{Code: "spawn_failed"}} {
		if IsNotOwner(e) || IsNotOwner(agenterr.NoteBrokerGrant(e)) {
			t.Fatalf("%v must not classify as not_owner", e)
		}
	}
}

func TestT796ReadoptDetachesStaleThenAdoptsOnce(t *testing.T) {
	f, r := newFakeSeat()
	stale := f.cur
	var got *claudia.Agent
	r.OnReadopt = func(_ string, a *claudia.Agent) { got = a }
	fresh, err := r.Readopt(context.Background(), "jevons", stale)
	if err != nil {
		t.Fatal(err)
	}
	if fresh == stale || got != fresh {
		t.Fatal("re-adopt must hand back a different handle and announce it")
	}
	if !f.detached[stale] || f.adopts.Load() != 1 {
		t.Fatalf("stale detached=%v adopts=%d; want detach then exactly one adopt", f.detached[stale], f.adopts.Load())
	}
}

func TestT796ConcurrentReadoptsShareOneAdopt(t *testing.T) {
	f, r := newFakeSeat()
	stale := f.cur
	var wg sync.WaitGroup
	results := make([]*claudia.Agent, 8)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, err := r.Readopt(context.Background(), "jevons", stale)
			if err != nil {
				t.Error(err)
			}
			results[i] = a
		}()
	}
	wg.Wait()
	if n := f.adopts.Load(); n != 1 {
		t.Fatalf("adopts=%d; concurrent not_owner callers must share one re-adopt", n)
	}
	for _, a := range results {
		if a != results[0] {
			t.Fatal("every caller must end up on the same fresh handle")
		}
	}
}

func TestT796BackoffDoublesAndCaps(t *testing.T) {
	f, r := newFakeSeat()
	for i := 0; i < 3; i++ {
		if _, err := r.Readopt(context.Background(), "jevons", f.cur); err != nil {
			t.Fatal(err)
		}
	}
	// Attempt 1 waits nothing; 2 waits Gap; 3 waits 2*Gap.
	want := []time.Duration{time.Second, 2 * time.Second}
	if len(f.slept) != len(want) {
		t.Fatalf("slept=%v want %v", f.slept, want)
	}
	for i, d := range want {
		if f.slept[i] != d {
			t.Fatalf("slept=%v want %v", f.slept, want)
		}
	}
}

func TestT796CapFailsLoudOnceThenFast(t *testing.T) {
	f, r := newFakeSeat()
	var notices []error
	r.Notify = func(_ string, err error) { notices = append(notices, err) }
	for i := 0; i < r.MaxAttempts; i++ {
		if _, err := r.Readopt(context.Background(), "jevons", f.cur); err != nil {
			t.Fatal(err)
		}
	}
	before := f.adopts.Load()
	for i := 0; i < 3; i++ {
		_, err := r.Readopt(context.Background(), "jevons", f.cur)
		if !errors.Is(err, ErrReadoptExhausted) {
			t.Fatalf("attempt past the cap: %v; want ErrReadoptExhausted", err)
		}
	}
	if f.adopts.Load() != before {
		t.Fatal("no adopt may run once the budget is spent")
	}
	if len(notices) != 1 {
		t.Fatalf("owner notices=%d; want exactly one", len(notices))
	}
}

func TestT796BudgetRefreshesAfterQuietWindow(t *testing.T) {
	f, r := newFakeSeat()
	for i := 0; i < r.MaxAttempts; i++ {
		_, _ = r.Readopt(context.Background(), "jevons", f.cur)
	}
	if _, err := r.Readopt(context.Background(), "jevons", f.cur); !errors.Is(err, ErrReadoptExhausted) {
		t.Fatal("cap not reached")
	}
	f.mu.Lock()
	f.clock = f.clock.Add(2 * r.Window)
	f.mu.Unlock()
	if _, err := r.Readopt(context.Background(), "jevons", f.cur); err != nil {
		t.Fatalf("a quiet seat gets a fresh budget: %v", err)
	}
}

func TestT796AdoptFailureIsReportedAndCounted(t *testing.T) {
	f, r := newFakeSeat()
	f.adoptErr = errors.New("grant_held")
	if _, err := r.Readopt(context.Background(), "jevons", f.cur); err == nil {
		t.Fatal("a refused adopt must surface")
	}
	if f.adopts.Load() != 1 {
		t.Fatal("one attempt expected")
	}
}

func TestT796WithReadoptRetriesOnlyOnNotOwner(t *testing.T) {
	f, r := newFakeSeat()
	DefaultReadopter = r
	defer func() { DefaultReadopter = nil }()

	var calls []*claudia.Agent
	stale := f.cur
	err := WithReadopt(context.Background(), "jevons", stale, func(a *claudia.Agent) error {
		calls = append(calls, a)
		if a == stale {
			return notOwnerErr()
		}
		return nil
	})
	if err != nil || len(calls) != 2 || calls[1] == stale {
		t.Fatalf("err=%v calls=%d; want a send on the stale handle refused, then success on a fresh one", err, len(calls))
	}

	// Any other refusal is returned untouched with no adopt.
	f.adopts.Store(0)
	busy := errors.New("prompt already in flight")
	if err := WithReadopt(context.Background(), "jevons", f.cur, func(*claudia.Agent) error { return busy }); err != busy {
		t.Fatalf("err=%v", err)
	}
	if f.adopts.Load() != 0 {
		t.Fatal("a non-not_owner failure must not re-adopt")
	}

	// A second not_owner on the fresh handle is returned, never looped.
	n := 0
	err = WithReadopt(context.Background(), "jevons", f.cur, func(*claudia.Agent) error { n++; return notOwnerErr() })
	if !IsNotOwner(err) || n != 2 {
		t.Fatalf("err=%v n=%d; want exactly one retry", err, n)
	}
}

func TestT796WithReadoptPassesThroughWithoutReadopter(t *testing.T) {
	DefaultReadopter = nil
	sentence := errors.New(notOwnerText)
	err := WithReadopt(context.Background(), "x", nil, func(*claudia.Agent) error { return sentence })
	if err != sentence || IsNotOwner(err) {
		t.Fatal("no Readopter installed: a sentence stays the caller's error")
	}
	coded := notOwnerErr()
	err = WithReadopt(context.Background(), "x", nil, func(*claudia.Agent) error { return coded })
	if !IsNotOwner(err) || !strings.Contains(err.Error(), "not_owner") {
		t.Fatal("no Readopter installed: the protocol code is typed and its text stays")
	}
	busy := errors.New("prompt already in flight")
	if got := WithReadopt(context.Background(), "x", nil, func(*claudia.Agent) error { return busy }); got != busy {
		t.Fatal("a non-matching error is the caller's")
	}
}

func TestT796NoBrokerRefusesReadopt(t *testing.T) {
	f, r := newFakeSeat()
	r.brokerPresent = func() bool { return false }
	if _, err := r.Readopt(context.Background(), "jevons", f.cur); err == nil || f.adopts.Load() != 0 {
		t.Fatalf("err=%v adopts=%d; without a broker there is nothing to adopt from and no fallback", err, f.adopts.Load())
	}
}

func TestT796ConfirmBrokerGrantReadoptsOnStaleGrant(t *testing.T) {
	f, r := newFakeSeat()
	DefaultReadopter = r
	t.Cleanup(func() { DefaultReadopter = nil })
	pa := brokerAvailable
	brokerAvailable = func() bool { return true }
	t.Cleanup(func() { brokerAvailable = pa })
	calls := 0
	oldProbe := grantProbe
	t.Cleanup(func() { grantProbe = oldProbe })
	grantProbe = func(context.Context, *claudia.Agent) error {
		calls++
		if calls == 1 {
			return agenterr.NoteBrokerGrant(notOwnerErr())
		}
		return nil
	}
	stale := f.cur
	fresh, err := ConfirmBrokerGrant(context.Background(), "jevons", stale)
	if err != nil {
		t.Fatalf("ConfirmBrokerGrant: %v", err)
	}
	if fresh == stale || fresh == nil {
		t.Fatal("expected a freshly adopted handle after not_owner on the grant probe")
	}
	if f.adopts.Load() != 1 {
		t.Fatalf("adopts=%d want 1", f.adopts.Load())
	}
}

func TestT796ConfirmBrokerGrantSkipsProbeWhenBrokerSilent(t *testing.T) {
	f, r := newFakeSeat()
	DefaultReadopter = r
	t.Cleanup(func() { DefaultReadopter = nil })
	pa := brokerAvailable
	brokerAvailable = func() bool { return false }
	t.Cleanup(func() { brokerAvailable = pa })
	calls := 0
	oldProbe := grantProbe
	t.Cleanup(func() { grantProbe = oldProbe })
	grantProbe = func(context.Context, *claudia.Agent) error {
		calls++
		return agenterr.NoteBrokerGrant(notOwnerErr())
	}
	proc := f.cur
	fresh, err := ConfirmBrokerGrant(context.Background(), "jevons", proc)
	if err != nil || fresh != proc || calls != 0 || f.adopts.Load() != 0 {
		t.Fatalf("err=%v fresh=%p proc=%p probe_calls=%d adopts=%d", err, fresh, proc, calls, f.adopts.Load())
	}
}

func TestT796ConfirmBrokerGrantNoOpWhenGrantHeld(t *testing.T) {
	f, r := newFakeSeat()
	DefaultReadopter = r
	t.Cleanup(func() { DefaultReadopter = nil })
	pa := brokerAvailable
	brokerAvailable = func() bool { return true }
	t.Cleanup(func() { brokerAvailable = pa })
	oldProbe := grantProbe
	t.Cleanup(func() { grantProbe = oldProbe })
	grantProbe = func(context.Context, *claudia.Agent) error { return nil }
	proc := f.cur
	fresh, err := ConfirmBrokerGrant(context.Background(), "jevons", proc)
	if err != nil || fresh != proc || f.adopts.Load() != 0 {
		t.Fatalf("err=%v fresh=%p proc=%p adopts=%d", err, fresh, proc, f.adopts.Load())
	}
}

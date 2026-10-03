// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package missionbound

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBoundSurvivesRenameRestartAndRequiresAttributedOverride(t *testing.T) {
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "starts.json")
	open := func() *Store {
		s, e := Open(path, Policy{2, 24}, nil, nil, now)
		if e != nil {
			t.Fatal(e)
		}
		return s
	}
	s := open()
	for i := 0; i < 2; i++ {
		if _, _, e := s.Reserve(Start{Scope: "repo", Target: "T766.2", Seat: fmt.Sprintf("worker-%d", i)}, false, false, now); e != nil {
			t.Fatal(e)
		}
	}
	s = open()
	x := Start{Scope: "repo", Target: "T766.2", Seat: "renamed", Actor: "jevons-po", OverrideReason: "force engage"}
	if _, n, e := s.Reserve(x, false, false, now); e == nil || n == nil || n.Rank != 1 || n.Count != 2 {
		t.Fatalf("first refusal notice=%+v err=%v", n, e)
	}
	s = open()
	if _, n, e := s.Reserve(x, false, false, now); e == nil || n != nil {
		t.Fatalf("repeat notice=%+v err=%v", n, e)
	}
	if _, _, e := s.Reserve(x, true, false, now); e == nil {
		t.Fatal("PO override authorized")
	}
	x.Actor = "jevons"
	x.OverrideReason = ""
	if _, _, e := s.Reserve(x, true, true, now); e == nil {
		t.Fatal("reasonless override")
	}
	x.OverrideReason = "owner requested recovery"
	if _, _, e := s.Reserve(x, true, true, now); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), x.OverrideReason) || !strings.Contains(string(b), `"actor": "jevons"`) {
		t.Fatal("override not recorded")
	}
	if _, _, e := s.Reserve(x, false, false, now); e == nil {
		t.Fatal("one override opened unlimited starts")
	}
	if _, _, e := s.Reserve(x, false, false, now.Add(25*time.Hour)); e != nil {
		t.Fatal("expired window denied", e)
	}
}
func TestConcurrentAdmissionsAndCancel(t *testing.T) {
	now := time.Now()
	s, e := Open(filepath.Join(t.TempDir(), "s.json"), Policy{3, 24}, nil, nil, now)
	if e != nil {
		t.Fatal(e)
	}
	var admitted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, e := s.Reserve(Start{Scope: "r", Target: "T1", Seat: "w"}, false, false, now)
			if e == nil {
				admitted.Add(1)
			}
		}()
	}
	wg.Wait()
	if admitted.Load() != 3 {
		t.Fatalf("admitted %d", admitted.Load())
	}
	id, _, e := s.Reserve(Start{Scope: "other-repo", Target: "T1", Seat: "w"}, false, false, now)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Cancel(id); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		if _, _, e = s.Reserve(Start{Scope: "other-repo", Target: "T1", Seat: "w"}, false, false, now); e != nil {
			t.Fatal(e)
		}
	}
}
func TestSeedLifecycleAndRejectCorruption(t *testing.T) {
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	var history strings.Builder
	for _, name := range []string{"worker", "worker", "jevons", "jevons-po"} {
		fmt.Fprintf(&history, `{"ts":"2026-10-02T23:00:00Z","component":"agent_lifecycle","decision":"start","fields":{"name":%q,"target_id":"T1","workdir":"repo","outcome":"ok"}}`+"\n", name)
	}
	path := filepath.Join(t.TempDir(), "s.json")
	s, e := Open(path, Policy{2, 24}, strings.NewReader(history.String()), nil, now)
	if e != nil {
		t.Fatal(e)
	}
	if _, n, e := s.Reserve(Start{Scope: "repo", Target: "T1", Seat: "new"}, false, false, now); e == nil || n.Count != 2 {
		t.Fatalf("seed count %+v %v", n, e)
	}
	if e = os.WriteFile(path, []byte("broken"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = Open(path, Policy{2, 24}, nil, nil, now); e == nil {
		t.Fatal("corruption silently reset")
	}
	if _, e = Open(filepath.Join(t.TempDir(), "s.json"), Policy{2, 24}, strings.NewReader("broken\n"), nil, now); e == nil {
		t.Fatal("malformed history ignored")
	}
}

func TestInvalidStateCannotResetCounters(t *testing.T) {
	for _, raw := range []string{"null", "{}", `{"version":1,"notified":null}`, `{"version":1,"notified":{},"starts":[{}]}`} {
		path := filepath.Join(t.TempDir(), "s.json")
		if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Open(path, DefaultPolicy(), nil, nil, time.Now()); err == nil {
			t.Fatalf("accepted malformed state %s", raw)
		}
	}
}

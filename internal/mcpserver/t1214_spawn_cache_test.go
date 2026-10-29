// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/eventlog"
	"github.com/marcelocantos/jevons/internal/spawnorder"
)

// Hold the second journal read after it has captured its rows. Both reads
// therefore predate the event, while publication happens after the release.
func t1214HoldJournalSnapshot(t *testing.T, s *Server) (<-chan struct{}, func()) {
	t.Helper()
	tail := s.eventLogTail
	captured, release := make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	s.eventLogTail = func(opt eventlog.TailOptions) ([]eventlog.Event, string, error) {
		events, path, err := tail(opt)
		if opt.Component == spawnorder.JournalComponent {
			once.Do(func() {
				close(captured)
				select {
				case <-release:
				case <-t.Context().Done():
				}
			})
		}
		return events, path, err
	}
	return captured, unblock
}

func t1214Wait(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
}

func TestT1214StartInvalidatesInflightJournalSnapshot(t *testing.T) {
	for _, callback := range []bool{false, true} {
		for _, correlated := range []bool{false, true} {
			name := "direct"
			if callback {
				name = "logger_callback"
			}
			if correlated {
				name += "/correlated"
			} else {
				name += "/uncorrelated"
			}
			t.Run(name, func(t *testing.T) {
				s := t762Server(t)
				if callback {
					// jevonsd wires the HTTP server's dual-write logger here.
					// Keep a real journal, without launching a provider or daemon.
					s.SetEventLogger(func(component, decision string, fields map[string]any) {
						_ = eventlog.Log(s.eventJournal, component, decision, fields)
					})
				}
				t762Call(t, s, map[string]any{"action": "declare", "id": "fenced", "parent": "jevons-po", "actor": "supervisor", "seats": "seed:grok, later:grok, absent:grok"})
				s.LogEvent(spawnorder.JournalComponent, spawnorder.JournalStart, map[string]any{"name": "seed", "order_id": "fenced", "outcome": "ok"})
				before := s.spawnOrderJournalEvidence(time.Now())
				if len(before.Attempts) != 1 || before.ReadErr != "" {
					t.Fatalf("initial evidence = %+v", before)
				}
				captured, release := t1214HoldJournalSnapshot(t, s)
				done := s.kickSpawnOrderEvidenceRefresh()
				t.Cleanup(func() { release(); <-done })
				t1214Wait(t, captured)
				// The held scan cannot complete until below. An expired-cache
				// reader must still return its previous snapshot immediately.
				stale := s.spawnOrderJournalEvidence(time.Now().Add(spawnOrderAttemptsTTL))
				if len(stale.Attempts) != 1 || stale.Attempts[0].Name != "seed" {
					t.Fatalf("expired-cache reader lost its snapshot: %+v", stale)
				}
				if correlated {
					t762Start(t, s, mintOK, map[string]any{"name": "later", "provider": "grok", "order_id": "fenced"})
				} else {
					s.logLifecycle(compAgentLifecycle, "start", "ok", map[string]any{"name": "later", "provider": "grok"})
				}
				release()
				t1214Wait(t, done)
				s.spawnOrderMu.RLock()
				readAt := s.spawnOrderReadAt
				s.spawnOrderMu.RUnlock()
				if !readAt.IsZero() {
					t.Fatal("pre-event snapshot was published as fresh after the event")
				}
				out := t762Call(t, s, map[string]any{"action": "status", "id": "fenced"})
				want := "later (grok) unknown: a daemon start for this name was observed"
				if correlated {
					want = "order fenced: 2/3 minted (INCOMPLETE)"
				}
				for _, fragment := range []string{want, "absent (grok) not_attempted: no matching daemon start observed"} {
					if !strings.Contains(out, fragment) {
						t.Errorf("status lacks %q:\n%s", fragment, out)
					}
				}
			})
		}
	}
}

func TestT1214SupersededSynchronousReadReportsUnknown(t *testing.T) {
	s := t762Server(t)
	captured, release := t1214HoldJournalSnapshot(t, s)
	result := make(chan spawnorder.Evidence, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		result <- s.spawnOrderJournalEvidence(time.Now())
	}()
	t.Cleanup(func() { release(); <-done })
	t1214Wait(t, captured)
	s.logLifecycle(compAgentLifecycle, "start", "ok", map[string]any{"name": "later"})
	release()
	t1214Wait(t, done)
	if ev := <-result; ev.ReadErr == "" {
		t.Fatalf("superseded read could classify missing attempts as not_attempted: %+v", ev)
	}
	if !s.spawnOrderReadAt.IsZero() {
		t.Fatal("superseded synchronous read became fresh")
	}
	if ev := s.spawnOrderJournalEvidence(time.Now()); ev.ReadErr != "" || len(ev.Attempts) != 1 || ev.Attempts[0].Name != "later" {
		t.Fatalf("next read did not recover the later journal event: %+v", ev)
	}
}

func TestT1214UnrelatedEventsPreserveFreshCache(t *testing.T) {
	s := t762Server(t)
	now := time.Now()
	s.spawnOrderJournalEvidence(now)
	s.eventLogTail = func(eventlog.TailOptions) ([]eventlog.Event, string, error) {
		t.Error("unrelated event caused a fresh journal snapshot to be reloaded")
		return nil, "", nil
	}
	s.logLifecycle(compAgentLifecycle, "stop", "ok", map[string]any{"name": "seat"})
	s.LogEvent(spawnorder.JournalComponent, "declare", nil)
	s.spawnOrderJournalEvidence(now.Add(spawnOrderAttemptsTTL / 2))
}

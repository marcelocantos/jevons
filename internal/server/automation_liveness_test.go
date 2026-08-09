// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

// 🎯T27.9 fault-injection oracle: a fake tracked automation's signal is
// frozen past cadence×grace; the running monitor must surface the stall
// in the aggregated model (hub snapshot + /api/automations) and fire an
// owner notification within the expected window (deadline + one check
// interval + scheduling slack). Injecting a fresh signal must clear the
// stall and notify recovery. Fully decidable — no human judgment.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/config"
	"github.com/marcelocantos/jevons/internal/provider"
)

func TestAutomationLivenessFaultInjection(t *testing.T) {
	s := New("test", t.TempDir())

	// Notification sink: the overseer notify path, captured at the
	// notifySender seam (the same seam worker-reply delivery tests use).
	var mu sync.Mutex
	var notes []string
	s.notifySender = func(text string) error {
		mu.Lock()
		defer mu.Unlock()
		notes = append(notes, text)
		return nil
	}
	hasNote := func(substr string) bool {
		mu.Lock()
		defer mu.Unlock()
		for _, n := range notes {
			if strings.Contains(n, substr) {
				return true
			}
		}
		return false
	}

	// The fake tracked automation: its liveness signal is a real file
	// mtime probed by the production file-mtime source.
	sig := filepath.Join(t.TempDir(), "automation.log")
	if err := os.WriteFile(sig, []byte("alive"), 0o644); err != nil {
		t.Fatal(err)
	}

	reg := provider.NewRegistry()
	hub := provider.NewFeedHub(provider.FeedHubArgs{Registry: reg})
	s.SetProviderFeeds(hub)

	const cadence = 80 * time.Millisecond // ×2 grace ⇒ stall deadline 160ms
	const interval = 20 * time.Millisecond
	mon := provider.NewLivenessMonitor(provider.LivenessMonitorArgs{
		Decls: []config.AutomationDecl{{
			ID: "fake-auto", Cadence: cadence.String(), Grace: 2,
			Source: config.AutomationSource{Kind: config.AutomationSourceFileMtime, Path: sig},
		}},
		Registry: reg,
		Interval: interval,
		OnEvent: func(ev provider.FeedEvent) {
			s.Broadcast(map[string]any{
				"type": "provider_event", "provider": provider.LivenessProviderID, "event": ev,
			})
		},
		OnNotice: func(st provider.AutomationStatus) {
			if err := s.SendToOverseer(provider.FormatAutomationNotice(st)); err != nil {
				t.Errorf("notify failed: %v", err)
			}
		},
	})
	s.SetAutomations(mon.Statuses)

	go mon.Run(t.Context())

	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	automationState := func() string {
		resp, err := http.Get(srv.URL + "/api/automations")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("GET /api/automations status=%d", resp.StatusCode)
		}
		var body struct {
			Automations []provider.AutomationStatus `json:"automations"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		for _, a := range body.Automations {
			if a.ID == "fake-auto" {
				return a.State
			}
		}
		return ""
	}
	waitFor := func(what string, window time.Duration, cond func() bool) {
		deadline := time.Now().Add(window)
		for time.Now().Before(deadline) {
			if cond() {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("%s: not observed within %s", what, window)
	}

	// FAULT INJECTION: the signal file is simply never touched again —
	// the automation stalls at deadline (160ms after the write above).
	// Expected window: deadline + one interval + generous CI slack.
	waitFor("stall surfaced on /api/automations", 3*time.Second, func() bool {
		return automationState() == provider.AutomationStalled
	})
	waitFor("stall notification fired", 3*time.Second, func() bool {
		return hasNote("Automation stall: fake-auto")
	})

	// The aggregated model (the same registry the feed hub snapshots for
	// client init frames) carries the liveness stall event.
	snap := hub.Snapshot()
	feed, ok := snap[provider.LivenessProviderID][provider.LivenessFeed]
	if !ok || feed.Last == nil || feed.Last.Kind != "stall" {
		t.Fatalf("aggregated model=%+v, want liveness stall event", snap)
	}
	if feed.Last.Data["automation"] != "fake-auto" {
		t.Fatalf("stall event data=%+v", feed.Last.Data)
	}

	// FRESH SIGNAL: keep the file's mtime current from here on (a touch
	// loop, so the automation cannot re-stall while we observe recovery).
	touchCtx := t.Context()
	go func() {
		tick := time.NewTicker(cadence / 2)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				now := time.Now()
				_ = os.Chtimes(sig, now, now)
			case <-touchCtx.Done():
				return
			}
		}
	}()

	waitFor("stall cleared on /api/automations", 3*time.Second, func() bool {
		return automationState() == provider.AutomationOK
	})
	waitFor("recovery notification fired", 3*time.Second, func() bool {
		return hasNote("Automation recovered: fake-auto")
	})
	snap = hub.Snapshot()
	if last := snap[provider.LivenessProviderID][provider.LivenessFeed].Last; last == nil || last.Kind != "clear" {
		t.Fatalf("aggregated model after recovery=%+v, want clear event", snap)
	}
}

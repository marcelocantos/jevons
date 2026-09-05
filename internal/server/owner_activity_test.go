// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/cost"
)

func TestT623_1OnlyOwnerSubmissionsRefreshActivity(t *testing.T) {
	s := &Server{notifySender: func(string) error { return nil }}
	var contacts atomic.Int32
	s.SetActivityHook(func() { contacts.Add(1) })
	for _, text := range []string{"budget warning", "[Agent worker responded]\nresult", "daemon restarted"} {
		if err := s.DeliverToOverseerAs(text, sendOriginAgent); err != nil {
			t.Fatal(err)
		}
	}
	if got := contacts.Load(); got != 0 {
		t.Fatalf("system notifications counted as %d owner contacts", got)
	}
	if err := s.SendToOverseer(userTurnPrefix + "owner request"); err != nil {
		t.Fatal(err)
	}
	if got := contacts.Load(); got != 1 {
		t.Fatalf("owner send recorded %d contacts; want 1", got)
	}
}

func TestT623_1BudgetNotificationDoesNotDeadlockNextOwnerSend(t *testing.T) {
	delivered := make(chan string, 2)
	s := &Server{notifySender: func(text string) error { delivered <- text; return nil }}
	// The real enforcer and the same synchronous server callback used by
	// cmd/jevonsd/cost.go: Act owns the cost lock while delivering a notice.
	notifyErrors := make(chan error, 1)
	e := cost.NewEnforcer(&cost.EnforcerArgs{
		Config: cost.DefaultBudgetConfig,
		Notify: func(_ cost.Level, text string) {
			notifyErrors <- s.DeliverToOverseerAs(text, sendOriginAgent)
		},
	})
	s.SetActivityHook(e.Heartbeat)
	done := make(chan error, 1)
	go func() {
		e.Act(&cost.Snapshot{Alerts: []cost.Alert{{Kind: cost.AlertProjection, Level: cost.LevelWarn, Detail: "budget warning"}}})
		done <- s.SendToOverseer(userTurnPrefix + "next owner request")
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("budget notification or subsequent owner send deadlocked")
	}
	if err := <-notifyErrors; err != nil {
		t.Fatal(err)
	}
	if got := <-delivered; got != "budget: budget warning" {
		t.Fatalf("budget notice missing: %q", got)
	}
	if got := <-delivered; !strings.HasSuffix(got, "next owner request") {
		t.Fatalf("owner send missing: %q", got)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T627.5: a stopped jevons-po with no open owner intent and zero work
// children is left stopped on the idle_nudge daemon-restarted path.
func TestT627_5SleepingPONotRehydratedOnDaemonRestart(t *testing.T) {
	s, inbox, skips, poSends := t627_5SleepingPOFixture(t)

	sid := s.registry.Def("jevons-po").SessionID
	s.NotifyDaemonRestarted("jevons", "jevons-po", t.TempDir())

	if n := poSends(); n != 0 {
		t.Fatalf("sleeping PO Send called %d times (would ACP session/load / rehydrate)", n)
	}
	got := inbox.snapshot()
	if msgs := got["jevons-po"]; len(msgs) != 0 {
		t.Fatalf("sleeping PO received daemon-restarted: %v", msgs)
	}
	skip := findSkip(skips(), "jevons-po")
	if skip == nil {
		t.Fatalf("skip decision missing from lifecycle log: %v", skips())
	}
	if skip["reason"] != reasonSleepingPO {
		t.Fatalf("skip reason=%v want %s", skip["reason"], reasonSleepingPO)
	}
	if skip["reason"] == "never_materialized" {
		t.Fatal("sleeping PO treated as never-materialized (🎯T629)")
	}
	if skip["open_intent"] != false {
		t.Fatalf("open_intent=%v want false", skip["open_intent"])
	}
	ov := got["jevons"]
	if len(ov) != 1 {
		t.Fatalf("overseer messages=%v", ov)
	}
	if !strings.Contains(ov[0], "[event: "+eventDaemonRestarted+"]") {
		t.Fatalf("overseer lost ordinary restart: %s", ov[0])
	}
	if strings.Contains(ov[0], "[event: "+eventOwnerIntentResume+"]") {
		t.Fatalf("no_chatlog overseer resumed owner intent: %s", ov[0])
	}
	def := s.registry.Def("jevons-po")
	if def == nil || def.SessionID != sid || !def.Materialized {
		t.Fatalf("sleeping skip mutated registry def: %+v", def)
	}
}

func TestT627_5OverseerResumeIndependentWhenResidualClosedOrStale(t *testing.T) {
	boot := time.Now().UTC().Truncate(time.Second)
	old := boot.Add(-2 * time.Hour)
	cases := []struct {
		name  string
		lines []string
		want  string
	}{
		{
			name: ResidualAnsweredOrClosed,
			lines: []string{
				t592UserLine("Please implement the original transcript correction.", old),
				`{"type":"assistant","timestamp":"` + old.Add(time.Minute).Format(time.RFC3339) + `","message":{"content":"The transcript correction is complete. Commit abc1234; go test ./... passed. Achieved T627."}}`,
			},
			want: ResidualAnsweredOrClosed,
		},
		{
			name: ResidualStaleChatlog,
			lines: []string{
				t592UserLine("Please run the monthly Cursor cycle now.", old),
				t592ProgressLine(old.Add(30 * time.Minute)),
				t592ProgressLine(old.Add(4 * time.Hour)),
			},
			want: ResidualStaleChatlog,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stateDir := t.TempDir()
			t592WriteChatlog(t, stateDir, tc.lines)
			got := applyRestartRecoveryCutoff(LoadOpenOwnerIntent(stateDir, "jevons"), boot)
			if got.Recoverable() {
				t.Fatalf("residual recoverable text=%q residual=%q", got.Text, got.Residual)
			}
			if got.Residual != tc.want {
				t.Fatalf("residual=%q want %q", got.Residual, tc.want)
			}
			s, inbox, skips, poSends := t627_5SleepingPOFixture(t)
			s.bootAt = boot
			s.NotifyDaemonRestarted("jevons", "jevons-po", stateDir)
			if n := poSends(); n != 0 {
				t.Fatalf("sleeping PO Send called %d times under residual %s", n, tc.want)
			}
			if findSkip(skips(), "jevons-po") == nil {
				t.Fatalf("PO skip missing under residual %s: %v", tc.want, skips())
			}
			ov := inbox.snapshot()["jevons"]
			if len(ov) != 1 || !strings.Contains(ov[0], "[event: "+eventDaemonRestarted+"]") {
				t.Fatalf("overseer resume not independently suppressed: %v", ov)
			}
			if strings.Contains(ov[0], "[event: "+eventOwnerIntentResume+"]") {
				t.Fatalf("residual %s leaked owner-intent-resume: %s", tc.want, ov[0])
			}
		})
	}
}

func TestT627_5StoppedPOWithWorkChildrenStillNotified(t *testing.T) {
	s, inbox, skips, poSends := t627_5SleepingPOFixture(t)
	if err := s.registry.Register(claudia.AgentDef{
		Name: "jv-t627.5-child", Parent: "jevons-po", Purpose: claudia.PurposeWork,
		WorkDir: t.TempDir(), SessionID: "s-child", Materialized: true, Provider: "claude",
		TargetID: "T627.5",
	}); err != nil {
		t.Fatal(err)
	}
	sid := s.registry.Def("jevons-po").SessionID
	s.NotifyDaemonRestarted("jevons", "jevons-po", t.TempDir())
	if n := poSends(); n != 1 {
		t.Fatalf("PO with work children Send called %d times, want 1 (fail-closed send, not skip)", n)
	}
	if findSkip(skips(), "jevons-po") != nil {
		t.Fatalf("PO with children was skipped: %v", skips())
	}
	po := inbox.snapshot()["jevons-po"]
	if len(po) != 1 || !strings.Contains(po[0], "[event: "+eventDaemonRestarted+"]") {
		t.Fatalf("PO with children not notified: %v", po)
	}
	def := s.registry.Def("jevons-po")
	if def.SessionID != sid || !def.Materialized {
		t.Fatalf("must-recover path reminted sleeping as never-materialized: %+v", def)
	}
}

func TestT627_5StoppedPOWithOpenIntentStillNotified(t *testing.T) {
	boot := time.Now().UTC().Truncate(time.Second)
	stateDir := t.TempDir()
	t592WriteChatlog(t, stateDir, []string{
		t592UserLine("Please implement the interrupted pre-restart transcript correction.", boot.Add(-time.Minute)),
	})
	s, inbox, skips, poSends := t627_5SleepingPOFixture(t)
	s.bootAt = boot
	s.NotifyDaemonRestarted("jevons", "jevons-po", stateDir)
	if n := poSends(); n != 1 {
		t.Fatalf("PO with open intent Send called %d times, want 1", n)
	}
	if findSkip(skips(), "jevons-po") != nil {
		t.Fatalf("open-intent PO skipped: %v", skips())
	}
	got := inbox.snapshot()
	if len(got["jevons-po"]) != 1 {
		t.Fatalf("open-intent PO not notified: %v", got["jevons-po"])
	}
	ov := got["jevons"]
	if len(ov) != 1 || !strings.Contains(ov[0], "[event: "+eventOwnerIntentResume+"]") {
		t.Fatalf("overseer did not resume open intent: %v", ov)
	}
}

type t627_5Life struct {
	mu     sync.Mutex
	events []map[string]any
}

func t627_5SleepingPOFixture(t *testing.T) (*Server, *t452Inbox, func() []map[string]any, func() int) {
	t.Helper()
	s, inbox := t452Fixture(t, "jevons", "session",
		claudia.AgentDef{Name: "jevons", Purpose: claudia.PurposeOverseer},
		claudia.AgentDef{Name: "jevons-po", Parent: "jevons", Purpose: claudia.PurposeWork, SessionID: "b54f134f-sleeping-po"},
	)
	life := &t627_5Life{}
	s.SetEventLogger(func(component, decision string, fields map[string]any) {
		row := map[string]any{"component": component, "decision": decision}
		for k, v := range fields {
			row[k] = v
		}
		life.mu.Lock()
		life.events = append(life.events, row)
		life.mu.Unlock()
	})
	var mu sync.Mutex
	var poSends int
	setObservedSenderResolver(s, func(name string) (agentSender, bool, error) {
		if name != "jevons-po" {
			return t452Seat{dest: name, inbox: inbox}, false, nil
		}
		return t627_5CountSend{dest: name, inbox: inbox, mu: &mu, n: &poSends}, false, nil
	})
	return s, inbox, func() []map[string]any {
			life.mu.Lock()
			defer life.mu.Unlock()
			out := make([]map[string]any, len(life.events))
			copy(out, life.events)
			return out
		}, func() int {
			mu.Lock()
			defer mu.Unlock()
			return poSends
		}
}

// t627_5CountSend is a liveness probe that does not count as rehydrate.
// liveSender may resolve it; only Send is the daemon-restarted deliver
// that would ACP session/load on the product path.
type t627_5CountSend struct {
	dest  string
	inbox *t452Inbox
	mu    *sync.Mutex
	n     *int
}

func (s t627_5CountSend) Alive() bool { return true }
func (s t627_5CountSend) Send(text string) error {
	s.mu.Lock()
	*s.n++
	s.mu.Unlock()
	s.inbox.put(s.dest, text)
	return nil
}
func (s t627_5CountSend) Interrupt() error { return nil }

func findSkip(events []map[string]any, target string) map[string]any {
	for _, e := range events {
		if e["component"] == compIdleNudge && e["decision"] == eventDaemonRestarted &&
			e["outcome"] == "skip" && e["target"] == target {
			return e
		}
	}
	return nil
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/statedb"
)

func TestRestartRecoveryExcludesPostBootOwnerRequests(t *testing.T) {
	boot := time.Now().UTC().Truncate(time.Second)
	for _, tc := range []struct {
		name        string
		at          time.Time
		wantResume  bool
		unknownBoot bool
	}{
		{"interrupted before boot", boot.Add(-time.Minute), true, false},
		{"accepted after boot", boot.Add(time.Second), false, false},
		{"accepted at boot", boot, false, false},
		{"unknown timestamp", time.Time{}, false, false},
		{"unknown boot", boot.Add(-time.Minute), false, true},
	} {
		for _, store := range []string{"legacy", "sqlite"} {
			t.Run(tc.name+"/"+store, func(t *testing.T) {
				stateDir := t.TempDir()
				text := "Please implement the requested owner transcript correction."
				t592WriteChatlog(t, stateDir, []string{t592UserLine(text, tc.at)})
				if store == "sqlite" {
					db, err := statedb.Open(statedb.DefaultPath(stateDir))
					if err != nil {
						t.Fatal(err)
					}
					if err := db.Upsert("jevons", []statedb.Event{{Index: 1, ID: "owner-1", Type: "user", Body: t592UserLine(text, tc.at), TS: tc.at.Format(time.RFC3339)}}); err != nil {
						t.Fatal(err)
					}
					if err := db.Close(); err != nil {
						t.Fatal(err)
					}
				}
				s, inbox := t452Fixture(t, "jevons", "recovery-session", claudia.AgentDef{Name: "jevons", Purpose: claudia.PurposeOverseer})
				s.bootAt = boot
				if tc.unknownBoot {
					s.bootAt = time.Time{}
				}
				s.NotifyDaemonRestarted("jevons", "", stateDir)
				messages := inbox.snapshot()["jevons"]
				if len(messages) != 1 {
					t.Fatalf("messages=%v", messages)
				}
				if got := strings.Contains(messages[0], "[event: "+eventOwnerIntentResume+"]"); got != tc.wantResume {
					t.Fatalf("resume=%v want %v: %s", got, tc.wantResume, messages[0])
				}
				if !tc.wantResume && strings.Contains(messages[0], text) {
					t.Fatalf("new intent leaked into restart payload: %s", messages[0])
				}
			})
		}
	}
}

type t627UpdatingSeat struct{ change func() }

func (s t627UpdatingSeat) Alive() bool       { return true }
func (s t627UpdatingSeat) Send(string) error { s.change(); return nil }
func (s t627UpdatingSeat) Interrupt() error  { return nil }

func TestRestartRecoveryReadsOwnerStateAfterPONotification(t *testing.T) {
	for _, state := range []string{"removed", "replaced", "answered"} {
		t.Run(state, func(t *testing.T) {
			stateDir := t.TempDir()
			boot := time.Now().UTC().Truncate(time.Second)
			old := t592UserLine("Please implement the original transcript correction.", boot.Add(-time.Minute))
			t592WriteChatlog(t, stateDir, []string{old})
			s, inbox := t452Fixture(t, "jevons", "session", claudia.AgentDef{Name: "jevons", Purpose: claudia.PurposeOverseer}, claudia.AgentDef{Name: "jevons-po", Parent: "jevons", Purpose: claudia.PurposeWork})
			s.bootAt = boot
			s.SetSenderResolver(func(name string) (agentSender, bool, error) {
				if name != "jevons-po" {
					return t452Seat{dest: name, inbox: inbox}, false, nil
				}
				return t627UpdatingSeat{change: func() {
					var lines []string
					switch state {
					case "replaced":
						lines = []string{old, t592UserLine("Please implement the different current request.", boot.Add(time.Second))}
					case "answered":
						lines = []string{old, `{"type":"assistant","message":{"content":"The transcript correction is complete. Commit abc1234; go test ./... passed."}}`}
					}
					t592WriteChatlog(t, stateDir, lines)
				}}, false, nil
			})
			s.NotifyDaemonRestarted("jevons", "jevons-po", stateDir)
			messages := inbox.snapshot()["jevons"]
			if len(messages) != 1 || strings.Contains(messages[0], "[event: "+eventOwnerIntentResume+"]") {
				t.Fatalf("recovered stale owner state after PO delivery: %v", messages)
			}
		})
	}
}

func TestApplyRestartRecoveryCutoff(t *testing.T) {
	boot := time.Date(2026, 9, 20, 15, 2, 0, 0, time.UTC)
	open := OpenOwnerIntent{Text: "Please implement the requested owner transcript correction.", TS: boot.Add(-time.Minute)}
	if got := applyRestartRecoveryCutoff(open, boot); !got.Recoverable() {
		t.Fatalf("pre-boot intent must remain recoverable, residual=%q", got.Residual)
	}
	open.TS = boot
	if got := applyRestartRecoveryCutoff(open, boot); got.Residual != ResidualPostBootIntent {
		t.Fatalf("at-boot residual=%q", got.Residual)
	}
	open.TS = boot.Add(time.Second)
	if got := applyRestartRecoveryCutoff(open, boot); got.Residual != ResidualPostBootIntent {
		t.Fatalf("post-boot residual=%q", got.Residual)
	}
	open.TS = time.Time{}
	if got := applyRestartRecoveryCutoff(open, boot); got.Residual != ResidualUnknownRestartBoundary {
		t.Fatalf("unknown instruction residual=%q", got.Residual)
	}
	open.TS = boot.Add(-time.Minute)
	if got := applyRestartRecoveryCutoff(open, time.Time{}); got.Residual != ResidualUnknownRestartBoundary {
		t.Fatalf("unknown boot residual=%q", got.Residual)
	}
	closed := OpenOwnerIntent{Residual: ResidualAnsweredOrClosed}
	if got := applyRestartRecoveryCutoff(closed, boot); got.Residual != ResidualAnsweredOrClosed {
		t.Fatalf("closed residual rewritten: %q", got.Residual)
	}
}

func TestDelayedRestartSweepIgnoresIntentAcceptedDuringSettle(t *testing.T) {
	prev := brokerHoldsFleet
	brokerHoldsFleet = func() bool { return false }
	t.Cleanup(func() { brokerHoldsFleet = prev })

	boot := time.Now().UTC().Truncate(time.Second)
	stateDir := t.TempDir()
	text := "Please implement the settle-window owner request."
	s, inbox := t452Fixture(t, "jevons", "session", claudia.AgentDef{Name: "jevons", Purpose: claudia.PurposeOverseer}, claudia.AgentDef{Name: "jevons-po", Parent: "jevons", Purpose: claudia.PurposeWork})
	s.bootAt = boot
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go StartIdleNudgeLoop(ctx, IdleNudgeLoopArgs{
		Server:       s,
		StateDir:     stateDir,
		PostDelay:    80 * time.Millisecond,
		OverseerName: "jevons",
		DefaultPO:    "jevons-po",
		Interval:     -1,
	})
	time.Sleep(20 * time.Millisecond)
	t592WriteChatlog(t, stateDir, []string{t592UserLine(text, boot.Add(time.Second))})

	msg := t627WaitOverseer(t, inbox, 2*time.Second)
	if strings.Contains(msg, "[event: "+eventOwnerIntentResume+"]") || strings.Contains(msg, text) {
		t.Fatalf("delayed sweep reissued post-boot settle intent: %s", msg)
	}
}

func TestDelayedRestartSweepResumesPreBootIntent(t *testing.T) {
	prev := brokerHoldsFleet
	brokerHoldsFleet = func() bool { return false }
	t.Cleanup(func() { brokerHoldsFleet = prev })

	boot := time.Now().UTC().Truncate(time.Second)
	stateDir := t.TempDir()
	text := "Please implement the interrupted pre-restart transcript correction."
	t592WriteChatlog(t, stateDir, []string{t592UserLine(text, boot.Add(-time.Minute))})
	s, inbox := t452Fixture(t, "jevons", "session", claudia.AgentDef{Name: "jevons", Purpose: claudia.PurposeOverseer}, claudia.AgentDef{Name: "jevons-po", Parent: "jevons", Purpose: claudia.PurposeWork})
	s.bootAt = boot
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go StartIdleNudgeLoop(ctx, IdleNudgeLoopArgs{
		Server:       s,
		StateDir:     stateDir,
		PostDelay:    40 * time.Millisecond,
		OverseerName: "jevons",
		DefaultPO:    "jevons-po",
		Interval:     -1,
	})
	msg := t627WaitOverseer(t, inbox, 2*time.Second)
	if !strings.Contains(msg, "[event: "+eventOwnerIntentResume+"]") || !strings.Contains(msg, text) {
		t.Fatalf("delayed sweep dropped genuine pre-boot intent: %s", msg)
	}
}

func TestRestartRecoveryUsesStateDBTimestampColumn(t *testing.T) {
	boot := time.Now().UTC().Truncate(time.Second)
	stateDir := t.TempDir()
	text := "Please implement the column-timestamp transcript correction."
	body := `{"type":"user","message":{"role":"user","content":"` + text + `"}}`
	db, err := statedb.Open(statedb.DefaultPath(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Upsert("jevons", []statedb.Event{{
		Index: 1, ID: "owner-col", Type: "user", Body: body, TS: boot.Add(-time.Minute).Format(time.RFC3339),
	}}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, inbox := t452Fixture(t, "jevons", "session", claudia.AgentDef{Name: "jevons", Purpose: claudia.PurposeOverseer})
	s.bootAt = boot
	s.NotifyDaemonRestarted("jevons", "", stateDir)
	msg := inbox.snapshot()["jevons"]
	if len(msg) != 1 || !strings.Contains(msg[0], "[event: "+eventOwnerIntentResume+"]") || !strings.Contains(msg[0], text) {
		t.Fatalf("column timestamp did not retain pre-boot recovery: %v", msg)
	}

	postDir := t.TempDir()
	postDB, err := statedb.Open(statedb.DefaultPath(postDir))
	if err != nil {
		t.Fatal(err)
	}
	if err := postDB.Upsert("jevons", []statedb.Event{{
		Index: 1, ID: "owner-post", Type: "user", Body: body, TS: boot.Add(time.Second).Format(time.RFC3339),
	}}); err != nil {
		t.Fatal(err)
	}
	if err := postDB.Close(); err != nil {
		t.Fatal(err)
	}
	s2, inbox2 := t452Fixture(t, "jevons", "session-2", claudia.AgentDef{Name: "jevons", Purpose: claudia.PurposeOverseer})
	s2.bootAt = boot
	s2.NotifyDaemonRestarted("jevons", "", postDir)
	msg2 := inbox2.snapshot()["jevons"]
	if len(msg2) != 1 || strings.Contains(msg2[0], "[event: "+eventOwnerIntentResume+"]") || strings.Contains(msg2[0], text) {
		t.Fatalf("column timestamp leaked post-boot intent: %v", msg2)
	}
}

func t627WaitOverseer(t *testing.T, inbox *t452Inbox, d time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		messages := inbox.snapshot()["jevons"]
		if len(messages) > 0 {
			return messages[len(messages)-1]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("overseer received no restart notify: %v", inbox.snapshot())
	return ""
}

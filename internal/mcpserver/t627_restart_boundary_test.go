// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
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

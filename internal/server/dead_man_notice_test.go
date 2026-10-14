// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/chatlog"
	"github.com/marcelocantos/jevons/internal/cost"
)

type deadManActions struct{ stops int }

func (*deadManActions) PauseWorker(string) error { return nil }
func (*deadManActions) KillWorker(string) error  { return nil }
func (a *deadManActions) StopFleet() error       { a.stops++; return nil }
func (*deadManActions) KillSwitch() error        { return nil }

// Exercise the real owner-journal channel, not a mock of notification text.
func TestDeadManTwoTierOwnerJournalDelivery(t *testing.T) {
	dir := t.TempDir()
	log, err := chatlog.Open(filepath.Join(dir, "chatlog", "jevons.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	srv := New("test", dir)
	srv.SetChatLog(log)
	actions := &deadManActions{}
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	now := start
	e := cost.NewEnforcer(&cost.EnforcerArgs{
		Config: cost.DefaultBudgetConfig, Actions: actions,
		Now: func() time.Time { return now }, OwnerNotice: srv.NotifyOwnerNote,
	})
	snap := &cost.Snapshot{FleetUSDPerHour: 1}
	now = start.Add(7 * time.Hour)
	e.Act(snap)
	if actions.stops != 0 {
		t.Fatal("first tier stopped the fleet")
	}
	now = start.Add(25 * time.Hour)
	e.Act(snap)
	if actions.stops != 1 {
		t.Fatalf("hard tier stops=%d, want 1", actions.stops)
	}
	var kinds []string
	if err := log.Replay(func(line string) error {
		var record struct {
			Type   string `json:"type"`
			Notice struct {
				Kind string `json:"kind"`
			} `json:"notice"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return err
		}
		if record.Type == "agent_note" {
			kinds = append(kinds, record.Notice.Kind)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(kinds) != 2 || kinds[0] != "dead-man-narrow" || kinds[1] != "dead-man-hard" {
		t.Fatalf("owner journal lacked both switch notices: %v", kinds)
	}
}

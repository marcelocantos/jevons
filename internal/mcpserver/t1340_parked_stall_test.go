package mcpserver

import (
	"github.com/marcelocantos/claudia"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

// A stopped registry seat without explicit Parked intent takes the ordinary
// no-live-process branch, not the stood-down branch of T1187.
func TestT1340StoppedWorkingIntentStallOncePerEntry(t *testing.T) {
	dir := t.TempDir()
	s, _, _ := t418Daemon(t, dir)
	now := time.Date(2026, 10, 30, 9, 0, 0, 0, time.UTC)
	capture := &slogCapture{}
	prev := slog.Default()
	slog.SetDefault(slog.New(capture))
	defer slog.SetDefault(prev)
	s.SetSweepClock(func() time.Time { return now })
	agent := "jv-t1340-parked"
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{Name: agent, SessionID: agent, WorkDir: dir, Provider: "grok", Purpose: claudia.PurposeWork}); err != nil {
		t.Fatal(err)
	}
	s.SetRegistry(reg)
	setObservedSenderResolver(s, func(string) (agentSender, bool, error) { return nil, false, nil })
	if _, _, err := s.sendQueue().Append(agent, "first", now.Add(-StalledBacklogAfter-time.Minute)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		s.SweepSendBacklogs()
	}
	if got := countStallWarns(capture.snapshot()); got != 1 {
		t.Fatalf("first entry: got %d stall WARNs, want 1; records=%+v backlog=%+v", got, capture.snapshot(), s.sendQueue())
	}
	// A second hold under the same name is an independent entry. The warning
	// key changes on a fresh head; repeated ticks of that head stay silent.
	rows, err := s.sendQueue().Snapshot(agent)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("queue: %+v", rows)
	}
	// The first entry stays queued; another entry behind it must not repeat
	// a warning for the same oldest hold.
	if _, _, err := s.sendQueue().Append(agent, "second", now.Add(-StalledBacklogAfter-time.Minute)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		s.SweepSendBacklogs()
	}
	if got := countStallWarns(capture.snapshot()); got != 1 {
		t.Fatalf("same hold: got %d stall WARNs", got)
	}
}

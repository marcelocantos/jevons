package mcpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/poownermission"
)

const t1195Ledger = `
schema_version: 1
project: test
targets: {}
`

func t1195Fixture(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	if err := os.MkdirAll(state, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bullseye.yaml"), []byte(t1195Ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, nil)
	s.SetRegistry(reg)
	s.SetAgentReportDir(state)
	s.idleActivity = NewIdleActivityTracker()
	if err := reg.Register(claudia.AgentDef{Name: "jevons-po", WorkDir: dir, SessionID: "po-1", Parent: "jevons", Purpose: purposeProductOwner}); err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func TestT1195RecordsOwnerBoundedMission(t *testing.T) {
	s, _ := t1195Fixture(t)
	text := "reconcile landed-but-open: 🎯T10 🎯T11"
	s.maybeRecordPOOwnerMission("jevons-po", text, "om-t1195", OriginOwner)
	store, err := poownermission.OpenStore(s.agentReportStateDir())
	if err != nil {
		t.Fatal(err)
	}
	m, ok, err := store.ForPO("jevons-po")
	if err != nil || !ok || m.RequestID != "om-t1195" || len(m.Items) != 2 {
		t.Fatalf("mission=%+v ok=%v err=%v", m, ok, err)
	}
}

func TestT1195WakesIdlePOWithOpenMission(t *testing.T) {
	s, dir := t1195Fixture(t)
	store, err := poownermission.OpenStore(s.agentReportStateDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Open("jevons-po", poownermission.BoundedRequest{
		RequestID: "om-t1195", ItemCount: 94,
	}, time.Date(2026, 10, 23, 22, 5, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	setPhase(s, "jevons-po", "idle", time.Time{})
	var delivered []string
	s.SetNotify(func(text string) { delivered = append(delivered, text) })
	t0 := time.Date(2026, 10, 23, 22, 6, 0, 0, time.UTC)
	_, _ = s.runSentinelCycle(SentinelLoopArgs{
		Server: s, Workdir: dir, DryRun: false,
		ProcessRunning: func(string) bool { return true },
		Now:            func() time.Time { return t0 },
	})
	past := t0.Add(DefaultSentinelMechanicalGrace + time.Minute)
	_, _ = s.runSentinelCycle(SentinelLoopArgs{
		Server: s, Workdir: dir, DryRun: false,
		ProcessRunning: func(string) bool { return true },
		Now:            func() time.Time { return past },
	})
	var wake string
	for _, msg := range delivered {
		if strings.Contains(msg, "T1195") && strings.Contains(msg, "om-t1195") {
			wake = msg
			break
		}
	}
	if wake == "" {
		t.Fatalf("expected PO wake, delivered=%v", delivered)
	}
}

func TestT1195ClosesOnPOFinishReport(t *testing.T) {
	s, _ := t1195Fixture(t)
	store, err := poownermission.OpenStore(s.agentReportStateDir())
	if err != nil {
		t.Fatal(err)
	}
	_, _ = store.Open("jevons-po", poownermission.BoundedRequest{RequestID: "om-x", Items: []string{"T10"}}, time.Now())
	s.storeAgentReport("jevons-po", declaredFinishReport("T1195", "mission done"))
	if _, ok, _ := store.ForPO("jevons-po"); ok {
		t.Fatal("mission should close on PO finish-report")
	}
}

package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

func TestT1340StartupReapsClosedParkedSeatsOnly(t *testing.T) {
	dir := t.TempDir()
	ledger := filepath.Join(dir, "bullseye.yaml")
	if err := os.WriteFile(ledger, []byte("targets:\n  T1:\n    status: achieved\n  T2:\n    status: set_aside\n  T3:\n    status: identified\n"), 0600); err != nil {
		t.Fatal(err)
	}
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	defs := []claudia.AgentDef{
		{Name: "jv-closed", TargetID: "T1", WorkDir: dir, Purpose: claudia.PurposeWork},
		{Name: "jv-aside", TargetID: "T2", WorkDir: dir, Purpose: claudia.PurposeWork},
		{Name: "jv-open", TargetID: "T3", WorkDir: dir, Purpose: claudia.PurposeWork},
		{Name: "jv-parent", TargetID: "T1", WorkDir: dir, Purpose: claudia.PurposeWork},
		{Name: "jv-child", TargetID: "T3", WorkDir: dir, Purpose: claudia.PurposeWork, Parent: "jv-parent"},
		{Name: "jv-durable", TargetID: "T1", WorkDir: dir, Purpose: claudia.PurposeWork, Role: "product-owner"},
		{Name: "jv-owner-note", TargetID: "T1", WorkDir: dir, Purpose: claudia.PurposeWork},
		{Name: "jv-working", TargetID: "T1", WorkDir: dir, Purpose: claudia.PurposeWork},
	}
	for _, d := range defs {
		d.SessionID = d.Name
		d.Provider = "grok"
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	s := New("test", dir)
	s.SetRegistry(reg)
	s.SetOverseerName("jevons")
	s.SetClosedTargetOwnerMessageGuard(func(name string) bool { return name == "jv-owner-note" })
	s.SweepParkedClosedTargetsAtStartup(func(name string) bool { return name != "jv-working" })
	for _, name := range []string{"jv-closed", "jv-aside"} {
		if reg.Def(name) != nil {
			t.Errorf("closed parked seat %s remains", name)
		}
	}
	for _, name := range []string{"jv-open", "jv-parent", "jv-child", "jv-durable", "jv-owner-note", "jv-working"} {
		if reg.Def(name) == nil {
			t.Errorf("seat %s removed", name)
		}
	}
	s.SetClosedTargetOwnerMessageGuard(func(name string) bool { return name == "jv-owner-note" })
	s.SweepParkedClosedTargetsAtStartup(func(name string) bool { return name != "jv-working" }) // idempotent
}

func TestT1340LedgerTransitionSweepsAlreadyParkedOnOtherClosedTargets(t *testing.T) {
	dir := t.TempDir()
	ledger := filepath.Join(dir, "bullseye.yaml")
	os.WriteFile(ledger, []byte("targets:\n  T1:\n    status: achieved\n  T2:\n    status: identified\n"), 0600)
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{{Name: "jv-old", TargetID: "T1", WorkDir: dir, Purpose: claudia.PurposeWork}, {Name: "jv-new", TargetID: "T2", WorkDir: dir, Purpose: claudia.PurposeWork}} {
		d.SessionID = d.Name
		d.Provider = "grok"
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	s := New("test", dir)
	s.SetRegistry(reg)
	s.seedAchievedFromLedger(ledger)
	if reg.Def("jv-old") != nil {
		t.Fatal("boot did not reap old closed target")
	}
	os.WriteFile(ledger, []byte("targets:\n  T1:\n    status: achieved\n  T2:\n    status: set_aside\n"), 0600)
	s.maybeReapOnLedgerAchieve(ledger)
	if reg.Def("jv-new") != nil {
		t.Fatal("transition did not reap parked seat")
	}
}

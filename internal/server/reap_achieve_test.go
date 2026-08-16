// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T195 hermetic: fixture worker engaged on target → ledger achieve →
// agent_list omits name (registry Def gone). PO stays. stop without kill OK.
func TestT195ReapOnTargetAchieveHermetic(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []claudia.AgentDef{
		{Name: "jevons", WorkDir: dir, SessionID: "s-o", Purpose: claudia.PurposeOverseer, Materialized: true, Provider: "grok"},
		{Name: "jevons-po", WorkDir: dir, SessionID: "s-po", Purpose: claudia.PurposeWork, Parent: "jevons", Materialized: true, Provider: "grok"},
		// Implementer bound to T195; name does not need to contain the id.
		{Name: "jv-t195-reap-achieve", WorkDir: dir, SessionID: "s-w", Purpose: claudia.PurposeWork, Parent: "jevons-po", Materialized: true, Provider: "grok", TargetID: "T195"},
		// Other mission — must stay when T195 is achieved.
		{Name: "jv-other-mission", WorkDir: dir, SessionID: "s-x", Purpose: claudia.PurposeWork, Parent: "jevons-po", Materialized: true, Provider: "grok", TargetID: "T1"},
	} {
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	isO := func(n string) bool { return n == "jevons" }

	// Residual: stop without kill keeps registration.
	reg.Stop("jv-t195-reap-achieve")
	if reg.Def("jv-t195-reap-achieve") == nil {
		t.Fatal("stop must not deregister")
	}

	// Achieve path: mission target achieved → reap engaged implementer.
	removed, err := ReapWorkAgentsOnTargetAchieve(reg, nil, "T195", dir, isO)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != "jv-t195-reap-achieve" {
		t.Fatalf("removed=%v want [jv-t195-reap-achieve]", removed)
	}
	if reg.Def("jv-t195-reap-achieve") != nil {
		t.Fatal("implementer still registered after achieve reap")
	}
	if reg.Def("jevons-po") == nil || reg.Def("jevons") == nil {
		t.Fatal("PO/overseer must remain")
	}
	if reg.Def("jv-other-mission") == nil {
		t.Fatal("unrelated TargetID agent must remain (multi-mission residual)")
	}
}

func TestT195AchieveDoesNotReapPOEvenWithTargetID(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jevons-po", WorkDir: dir, SessionID: "s-po", Purpose: claudia.PurposeWork,
		Parent: "jevons", Materialized: true, Provider: "grok", TargetID: "T195",
	}); err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-leaf", WorkDir: dir, SessionID: "s-l", Purpose: claudia.PurposeWork,
		Parent: "jevons-po", Materialized: true, Provider: "grok", TargetID: "T195",
	}); err != nil {
		t.Fatal(err)
	}
	removed, err := ReapWorkAgentsOnTargetAchieve(reg, nil, "T195", dir, func(n string) bool { return n == "jevons" })
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || removed[0] != "jv-leaf" {
		t.Fatalf("removed=%v want [jv-leaf]", removed)
	}
	if reg.Def("jevons-po") == nil {
		t.Fatal("PO must not auto-reap on achieve")
	}
}

func TestT195ListAndDiffNewlyAchieved(t *testing.T) {
	dir := t.TempDir()
	ledger := filepath.Join(dir, "bullseye.yaml")
	yaml1 := `schema_version: 1
targets:
  T1:
    name: one
    status: achieved
  T195:
    name: reap imperfect
    status: converging
  T2:
    name: two
    status: set_aside
`
	if err := os.WriteFile(ledger, []byte(yaml1), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := listAchievedTargetIDs(ledger)
	if err != nil {
		t.Fatal(err)
	}
	if !got["T1"] || got["T195"] || got["T2"] {
		t.Fatalf("achieved set=%v want T1 only", got)
	}

	yaml2 := `schema_version: 1
targets:
  T1:
    name: one
    status: achieved
  T195:
    name: reap imperfect
    status: achieved
  T2:
    name: two
    status: set_aside
`
	if err := os.WriteFile(ledger, []byte(yaml2), 0o644); err != nil {
		t.Fatal(err)
	}
	curr, err := listAchievedTargetIDs(ledger)
	if err != nil {
		t.Fatal(err)
	}
	newly := newlyAchievedTargetIDs(got, curr)
	if len(newly) != 1 || newly[0] != "T195" {
		t.Fatalf("newly=%v want [T195]", newly)
	}
}

// Product path: server seed + ledger transition reaps engaged implementer.
func TestT195MaybeReapOnLedgerAchieve(t *testing.T) {
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(claudia.AgentDef{
		Name: "jv-t195-reap-achieve", WorkDir: dir, SessionID: "s-w",
		Purpose: claudia.PurposeWork, Parent: "jevons-po", Materialized: true,
		Provider: "grok", TargetID: "T195",
	}); err != nil {
		t.Fatal(err)
	}
	ledger := filepath.Join(dir, "bullseye.yaml")
	if err := os.WriteFile(ledger, []byte(`schema_version: 1
targets:
  T195:
    name: work
    status: converging
`), 0o644); err != nil {
		t.Fatal(err)
	}

	s := New("test", dir)
	s.SetRegistry(reg)
	s.SetOverseerName("jevons")
	s.seedAchievedFromLedger(ledger)
	// Still converging: no reap.
	s.maybeReapOnLedgerAchieve(ledger)
	if reg.Def("jv-t195-reap-achieve") == nil {
		t.Fatal("must not reap before achieve")
	}

	if err := os.WriteFile(ledger, []byte(`schema_version: 1
targets:
  T195:
    name: work
    status: achieved
    attestation: hermetic T195
`), 0o644); err != nil {
		t.Fatal(err)
	}
	s.maybeReapOnLedgerAchieve(ledger)
	if reg.Def("jv-t195-reap-achieve") != nil {
		t.Fatal("implementer must be reaped after ledger achieve")
	}
}

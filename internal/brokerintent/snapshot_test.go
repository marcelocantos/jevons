// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package brokerintent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleetintent"
)

func readPolicy(t *testing.T, path string) policyDocument {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > maxBytes {
		t.Fatal("policy oversized")
	}
	var p policyDocument
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	if p.Version != 1 || p.Kind != "policy_hold" || !validTimestamp(p.UpdatedAt) {
		t.Fatalf("bad v1 contract: %+v", p)
	}
	if info, err := os.Stat(path); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%v", info.Mode())
	}
	return p
}

// T1253/T233: the existing Claudia parser sees only genuine intent;
// the second file carries transient policy holds, removed on lift.
func TestT1253TwoFileConsumerContractLift(t *testing.T) {
	dir := t.TempDir()
	w := New(dir)
	if w.Root() != filepath.Join(dir, "claudia-consumer") {
		t.Fatal(w.Root())
	}
	store, err := fleetintent.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for name, state := range map[string]fleetintent.State{"explicit": fleetintent.Parked, "completed": fleetintent.Reaped, "jv-t300-auto": fleetintent.Parked} {
		if err := store.SetAgent(name, state, "owner", "deliberate", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	defs := []claudia.AgentDef{{Name: "explicit"}, {Name: "jv-t100-auto", AutoStart: true, TargetID: "T100"}, {Name: "jv-t200-auto", AutoStart: true, TargetID: "T200"}, {Name: "jv-t300-auto", AutoStart: true}, {Name: "po", AutoStart: true}}
	paused := true
	hold := func(name string) string {
		if name == "jv-t200-auto" || name == "jv-t300-auto" {
			return achieved
		}
		if paused && name == "jv-t100-auto" {
			return "frontier_consume_disabled"
		}
		return ""
	}
	intentPath := filepath.Join(w.Root(), claudia.ConsumerIntentRelPath)
	policyPath := filepath.Join(w.Root(), "fleet", "policy-hold.json")
	for _, want := range [][]policyHold{
		{{Name: "jv-t100-auto", Reason: pausedReason()}, {Name: "jv-t200-auto", Reason: achieved}, {Name: "jv-t300-auto", Reason: achieved}},
		{{Name: "jv-t200-auto", Reason: achieved}, {Name: "jv-t300-auto", Reason: achieved}},
	} {
		if err := w.Write(defs, store.Snapshot(), hold); err != nil {
			t.Fatal(err)
		}
		got, err := claudia.StoodDownFromConsumerIntent(intentPath)
		if err != nil {
			t.Fatal(err)
		}
		if !got["explicit"] || !got["completed"] || !got["jv-t300-auto"] || got["po"] || got["jv-t100-auto"] || got["jv-t200-auto"] {
			t.Fatalf("real intent projection=%v", got)
		}
		p := readPolicy(t, policyPath)
		if !equalHolds(p.Holds, want) {
			t.Fatalf("policy=%+v want=%+v", p.Holds, want)
		}
		paused = false
	}
	for _, dir := range []string{w.Root(), filepath.Join(w.Root(), "fleet")} {
		if info, err := os.Stat(dir); err != nil {
			t.Fatal(err)
		} else if info.Mode().Perm() != 0700 {
			t.Fatalf("dir %s mode=%v", dir, info.Mode())
		}
	}
	if info, err := os.Stat(intentPath); err != nil {
		t.Fatal(err)
	} else if info.Mode().Perm() != 0600 {
		t.Fatalf("intent mode=%v", info.Mode())
	}
	before, _ := os.ReadFile(policyPath)
	if err := w.Write(defs, store.Snapshot(), hold); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(policyPath)
	if !bytes.Equal(before, after) {
		t.Fatal("unchanged policy snapshot churned")
	}
	real, err := claudia.StoodDownFromConsumerIntent(store.Path())
	if err != nil || real["jv-t100-auto"] || !real["completed"] {
		t.Fatalf("Jevons intent=%v err=%v", real, err)
	}
}
func pausedReason() string { return "frontier_consume_disabled" }

func TestT1253WriterRejectsBadOrOversizedSnapshots(t *testing.T) {
	w := New(t.TempDir())
	tests := []struct {
		name string
		defs []claudia.AgentDef
		hold func(string) string
	}{
		{"empty", []claudia.AgentDef{{AutoStart: true}}, func(string) string { return achieved }},
		{"duplicate", []claudia.AgentDef{{Name: "dup", AutoStart: true}, {Name: "dup", AutoStart: true}}, func(string) string { return achieved }},
		{"unknown reason", []claudia.AgentDef{{Name: "auto", AutoStart: true}}, func(string) string { return "wrong" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := w.Write(tc.defs, fleetintent.Snapshot{}, tc.hold); err == nil {
				t.Fatal("accepted invalid policy")
			}
		})
	}
	defs := make([]claudia.AgentDef, 4097)
	for i := range defs {
		defs[i] = claudia.AgentDef{Name: fmt.Sprintf("jv-t%d-auto", i), AutoStart: true}
	}
	if err := w.Write(defs, fleetintent.Snapshot{}, func(string) string { return achieved }); err == nil || !strings.Contains(err.Error(), "4096") {
		t.Fatalf("entry cap: %v", err)
	}
	huge := fleetintent.Snapshot{Agents: map[string]fleetintent.Record{strings.Repeat("x", maxBytes): {State: fleetintent.Parked}}}
	if err := w.Write(nil, huge, nil); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("byte cap: %v", err)
	}
	policyHuge := []claudia.AgentDef{{Name: strings.Repeat("y", maxBytes), AutoStart: true}}
	if err := w.Write(policyHuge, fleetintent.Snapshot{}, func(string) string { return achieved }); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("policy byte cap: %v", err)
	}
}

func TestT1253AtomicSnapshotsNeverPartial(t *testing.T) {
	w := New(t.TempDir())
	defs := []claudia.AgentDef{{Name: "jv-t100-auto", AutoStart: true}}
	intentPath := filepath.Join(w.Root(), claudia.ConsumerIntentRelPath)
	policyPath := filepath.Join(w.Root(), "fleet", "policy-hold.json")
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for n := 0; n < 30; n++ {
				hold := func(string) string {
					if i == 0 {
						return achieved
					}
					return ""
				}
				if err := w.Write(defs, fleetintent.Snapshot{}, hold); err != nil {
					t.Errorf("write: %v", err)
				}
				if _, err := claudia.StoodDownFromConsumerIntent(intentPath); err != nil {
					t.Errorf("partial intent: %v", err)
				}
				if data, err := os.ReadFile(policyPath); err != nil {
					t.Errorf("read policy: %v", err)
				} else {
					var p policyDocument
					if err := json.Unmarshal(data, &p); err != nil || p.Version != 1 || p.Kind != "policy_hold" {
						t.Errorf("partial policy: %v %+v", err, p)
					}
				}
			}
		}(i)
	}
	wg.Wait()
}

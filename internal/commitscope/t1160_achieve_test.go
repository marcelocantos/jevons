// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package commitscope

import (
	"strings"
	"testing"
)

func TestT1160WorkSeatRefusesLedgerAchieveCommit(t *testing.T) {
	head := []byte(seedLedger)
	staged := []byte(strings.Replace(seedLedger, "status: identified", "status: achieved", 1))
	refuse, msg := WorkerLedgerAchieveRefusal("jv-t958-auto", "worker", "work", head, staged)
	if !refuse {
		t.Fatal("work seat must not self-achieve")
	}
	for _, want := range []string{"jv-t958-auto", "T1160", "T1", "integrate"} {
		if !strings.Contains(msg, want) {
			t.Errorf("refusal %q missing %q", msg, want)
		}
	}
}

func TestT1160POSeatMayLedgerAchieve(t *testing.T) {
	head := []byte(seedLedger)
	staged := []byte(strings.Replace(seedLedger, "status: identified", "status: achieved", 1))
	refuse, _ := WorkerLedgerAchieveRefusal("jevons-po", "product-owner", "work", head, staged)
	if refuse {
		t.Fatal("PO seat may achieve")
	}
}

func TestT1160UnattributedAchieveAllowed(t *testing.T) {
	head := []byte(seedLedger)
	staged := []byte(strings.Replace(seedLedger, "status: identified", "status: achieved", 1))
	refuse, _ := WorkerLedgerAchieveRefusal("", "", "", head, staged)
	if refuse {
		t.Fatal("unattributed commit must not be refused by achieve guard")
	}
}

func TestT1160DecideRefusesWorkSeatAchieve(t *testing.T) {
	staged := []byte(strings.Replace(seedLedger, "status: identified", "status: achieved", 1))
	v := Decide(&Request{
		IndexFile: "/r/.git/next-index-9.lock",
		Staged:    []string{"bullseye.yaml"},
		Contents: []FileContent{{
			Path:   "bullseye.yaml",
			Head:   []byte(seedLedger),
			Staged: staged,
		}},
		Actor:   "jv-t1160-auto",
		Role:    "worker",
		Purpose: "work",
	})
	if !v.Refused {
		t.Fatalf("Decide = %+v, want refused achieve", v)
	}
	if !strings.Contains(v.Message, "T1160") {
		t.Fatalf("message %q", v.Message)
	}
}

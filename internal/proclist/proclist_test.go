// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package proclist

import (
	"os"
	"testing"
)

func TestAllIncludesSelf(t *testing.T) {
	procs, err := All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(procs) == 0 {
		t.Fatal("All returned no processes")
	}
	me := os.Getpid()
	parent := os.Getppid()
	var saw Proc
	for _, p := range procs {
		if p.PID == me {
			saw = p
			break
		}
	}
	if saw.PID != me {
		t.Fatalf("All (%d processes) did not include this test pid %d", len(procs), me)
	}
	if saw.PPID != parent {
		t.Errorf("self ppid = %d, want %d", saw.PPID, parent)
	}
	if saw.Command == "" && saw.Path == "" {
		t.Error("self has neither Command nor Path")
	}
}

func TestLookupSelf(t *testing.T) {
	me := os.Getpid()
	p, ok, err := Lookup(me)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !ok {
		t.Fatalf("Lookup(%d) reported not running", me)
	}
	if p.PID != me {
		t.Errorf("PID = %d, want %d", p.PID, me)
	}
	if p.PPID != os.Getppid() {
		t.Errorf("PPID = %d, want %d", p.PPID, os.Getppid())
	}
}

func TestLookupMissing(t *testing.T) {
	_, ok, err := Lookup(1 << 30)
	if err != nil {
		t.Fatalf("Lookup missing: %v", err)
	}
	if ok {
		t.Fatal("Lookup of a unused pid reported running")
	}
}

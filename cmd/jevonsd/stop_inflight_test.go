// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

func TestStopFleetForDrainOnEmptyRegistryStops(t *testing.T) {
	reg, err := claudia.NewRegistry(filepath.Join(t.TempDir(), "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	stopFleetForDrain(reg)
	if interruptLiveTurns(reg) != 0 {
		t.Fatal("empty registry interrupted a turn")
	}
	if registryTurnInFlight(reg) {
		t.Fatal("empty registry reported a turn in flight")
	}
}

func TestStopFleetForDrainNilRegistry(t *testing.T) {
	stopFleetForDrain(nil)
	if interruptLiveTurns(nil) != 0 {
		t.Fatal("nil registry interrupted a turn")
	}
	waitTurnsIdle(nil, time.Second)
}

func TestNormalShutdownDrainsBeforeStopAll(t *testing.T) {
	body, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(body)
	drain := strings.Index(src, "stopFleetForDrain(registry)")
	stop := strings.Index(src, "reg.StopAll()")
	if drain < 0 {
		t.Fatal("normal shutdown does not call stopFleetForDrain")
	}
	inflight, err := os.ReadFile("stop_inflight.go")
	if err != nil {
		t.Fatal(err)
	}
	helper := string(inflight)
	if !strings.Contains(helper, "interruptLiveTurns(reg)") || !strings.Contains(helper, "reg.StopAll()") {
		t.Fatal("stopFleetForDrain does not interrupt before StopAll")
	}
	interruptAt := strings.Index(helper, "interruptLiveTurns(reg)")
	stopAt := strings.Index(helper, "reg.StopAll()")
	if interruptAt < 0 || stopAt < 0 || interruptAt > stopAt {
		t.Fatalf("interrupt is not before StopAll: interrupt=%d stop=%d", interruptAt, stopAt)
	}
	if stop >= 0 && stop < drain {
		t.Fatal("main.go stops the fleet before the drain helper")
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"log/slog"
	"time"

	"github.com/marcelocantos/claudia"
)

// receiptDrainWait is how long a normal shutdown waits after interrupting
// an in-flight turn for that turn to publish a terminal receipt. The
// journey suite's own stop budget is a few seconds, so this stays inside it.
const receiptDrainWait = 1500 * time.Millisecond

// stopFleetForDrain is the normal-exit path. Interrupt a live turn first so
// the seat can finish its started receipt as cancelled, then stop the
// processes. An upgrade exit does not call this.
func stopFleetForDrain(reg *claudia.Registry) {
	if reg == nil {
		return
	}
	n := interruptLiveTurns(reg)
	if n > 0 {
		slog.Info("shutdown interrupted in-flight turns", "count", n)
		waitTurnsIdle(reg, receiptDrainWait)
	}
	reg.StopAll()
}

func interruptLiveTurns(reg *claudia.Registry) int {
	if reg == nil {
		return 0
	}
	n := 0
	for _, def := range reg.List() {
		agent := reg.Get(def.Name)
		if agent == nil || !agent.PromptInFlight() {
			continue
		}
		if err := agent.Interrupt(); err != nil {
			slog.Warn("shutdown interrupt failed", "name", def.Name, "err", err)
			continue
		}
		n++
	}
	return n
}

func waitTurnsIdle(reg *claudia.Registry, budget time.Duration) {
	if reg == nil || budget <= 0 {
		return
	}
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if !registryTurnInFlight(reg) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func registryTurnInFlight(reg *claudia.Registry) bool {
	for _, def := range reg.List() {
		agent := reg.Get(def.Name)
		if agent != nil && agent.PromptInFlight() {
			return true
		}
	}
	return false
}

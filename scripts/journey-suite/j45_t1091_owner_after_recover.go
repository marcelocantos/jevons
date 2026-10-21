// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// j45HoldLimit bounds the fleet turn J45 holds the overseer in. It is longer
// than J30's paint bound, so an owner turn left queued behind it fails.
const j45HoldLimit = 300

// J45 (🎯T1091): J20's exhaust-and-recover parks the overseer in the middle of
// a fleet-note turn and the recovery sweep re-mints it; then J30's packaged
// owner turn is sent without waiting for the overseer to go idle. The reply
// must paint within J30's bound: the owner turn preempts the turn it was
// queued behind (🎯T291) rather than waiting it out. In the 2026-10-20 J20→J30
// run it waited six minutes behind a research brief.
func (s *suite) jT1091OwnerTurnAfterRecover() error {
	release := filepath.Join(s.stateDir, "j45-release")
	s.j45HoldOverseer = true
	defer func() {
		s.j45HoldOverseer = false
		s.j45NoIdleWait = false
		if err := os.WriteFile(release, nil, 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "J45 release held turn: %v\n", err)
		}
	}()
	if err := s.j20PlanDest(); err != nil {
		return fmt.Errorf("J20 exhaust-and-recover: %w", err)
	}
	s.j45NoIdleWait = true
	return s.jPackagedReactScript("test.cjs", 4*time.Minute)
}

// j45HoldOverseerInFleetTurn has a fleet seat ask the overseer to run a
// shell hold, and returns once that turn is running it.
func (s *suite) j45HoldOverseerInFleetTurn(actor string) error {
	ready := filepath.Join(s.stateDir, "j45-ready")
	release := filepath.Join(s.stateDir, "j45-release")
	_ = os.Remove(ready)
	_ = os.Remove(release)
	quote := func(v string) string { return "'" + strings.ReplaceAll(v, "'", "'\"'\"'") + "'" }
	command := fmt.Sprintf("printf 'ready\\n' > %s; n=0; while [ ! -f %s ] && [ \"$n\" -lt %d ]; do sleep 1; n=$((n+1)); done; true",
		quote(ready), quote(release), j45HoldLimit)
	note := "Journey J45 fleet check. Use Bash to run exactly this command and wait for it to return. " +
		"Do not create the release file, background the command, or finish early:\n" + command + "\nThen reply with exactly: J45-HELD"
	if _, err := s.mcpText("jevons_agent_send", map[string]any{
		"name": overseerName, "actor": actor, "text": note,
	}); err != nil {
		return fmt.Errorf("fleet note to the overseer: %w", err)
	}
	deadline := time.Now().Add(3 * time.Minute)
	for {
		if body, err := os.ReadFile(ready); err == nil && string(body) == "ready\n" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the overseer never began the fleet turn's shell hold")
		}
		time.Sleep(500 * time.Millisecond)
	}
}

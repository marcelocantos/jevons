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
	logFrom := s.logSize()
	sent, err := s.mcpText("jevons_agent_send", map[string]any{
		"name": overseerName, "actor": actor, "text": note,
	})
	if err != nil {
		return fmt.Errorf("fleet note to the overseer: %w", err)
	}
	deadline := time.Now().Add(3 * time.Minute)
	for {
		if body, err := os.ReadFile(ready); err == nil && string(body) == "ready\n" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the overseer never began the fleet turn's shell hold; %s", s.j45HoldEvidence(sent, logFrom))
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// j45HoldEvidence says what happened to the hold note, for a failure message:
// J45 failed intermittently at this step and the shard's isolate went with it.
func (s *suite) j45HoldEvidence(sendResult string, logFrom int64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "send result: %q", trim(sendResult, 200))
	if agents, err := s.listAgentsHTTP(); err == nil {
		for _, a := range agents {
			if a.Name == overseerName {
				fmt.Fprintf(&b, "; overseer status=%s phase=%s", a.Status, a.Phase)
			}
		}
	}
	fmt.Fprintf(&b, "; overseer's last words: %s", trustLastAssistantWords(s, 3))
	if body, err := os.ReadFile(s.logPath); err == nil && int64(len(body)) > logFrom {
		var lines []string
		for _, ln := range strings.Split(string(body[logFrom:]), "\n") {
			if strings.Contains(ln, "name="+overseerName) || strings.Contains(ln, "agent="+overseerName) {
				if strings.Contains(ln, "GET /api") {
					continue
				}
				lines = append(lines, trim(ln, 240))
			}
		}
		if len(lines) > 12 {
			lines = lines[len(lines)-12:]
		}
		fmt.Fprintf(&b, "; daemon lines for %s since the send:\n  %s", overseerName, strings.Join(lines, "\n  "))
	}
	// The sidecar's own record of the seat: which provider it runs on and
	// whether a turn is open, which the daemon's view can disagree with
	// (🎯T1305).
	if spools, _ := filepath.Glob(filepath.Join(s.stateDir, "spool", "events-*.log")); len(spools) > 0 {
		if body, err := os.ReadFile(spools[len(spools)-1]); err == nil {
			var seat []string
			for _, ln := range strings.Split(string(body), "\n") {
				if strings.Contains(ln, `"seat":"`+overseerName+`"`) && !strings.Contains(ln, `"type":"text"`) {
					seat = append(seat, trim(ln, 220))
				}
			}
			if len(seat) > 12 {
				seat = seat[len(seat)-12:]
			}
			fmt.Fprintf(&b, "\n; sidecar events for %s:\n  %s", overseerName, strings.Join(seat, "\n  "))
		}
	}
	return b.String()
}

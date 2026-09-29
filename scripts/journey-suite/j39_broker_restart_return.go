// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/claudia/omp"
)

// jBrokerRestartReturn (🎯T925 / 🎯T935): a sidecar seat taken down by a
// Claudia broker restart comes back by itself, on its own conversation, and
// takes a turn — no kill, no remint, no send to wake it. On 2026-09-30
// jv-t928-mcp-attach sat stopped for twelve minutes after the broker
// restarted until the owner killed and reminted it by hand.
//
// The broker is the isolate's own and runs with -no-resume, so it does not
// bring the seat back itself: the daemon has to. The broker's sidecar is
// stopped with it, so the seat's process is gone, as the incident's was.
//
// The other T935 arm — a seat with no history anywhere relaunched on a fresh
// session — is not reachable here: the sidecar keeps each seat's
// conversation in its own store (claudia T151), and writes spool records for
// an unloaded seat that satisfy the broker's resume gate. It is pinned
// hermetically (internal/fleet TestT935AbsentHistoryRemints).
func (s *suite) jBrokerRestartReturn() error {
	return s.withIsolatedBroker((*suite).brokerRestartReturnWithBroker)
}

func (s *suite) brokerRestartReturnWithBroker() error {
	if s.broker == nil {
		return fmt.Errorf("J39 needs the isolate's own broker")
	}
	id := fmt.Sprintf("t935-broker-return-%d", time.Now().Unix()%100000)
	work := filepath.Join(s.stateDir, "broker-return-work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer func() {
		_, _ = s.mcpText("jevons_agent_kill", map[string]any{"name": id, "actor": "jevons", "force": true})
	}()
	if _, err := s.mcpText("jevons_agent_start", map[string]any{
		"name": id, "workdir": work, "actor": "jevons", "parent": "jevons", "purpose": "work",
		"provider": string(claudia.Provider(omp.Anthropic)), "owner_asked": true,
		"prompt": "Remember the word MARBLE39. Reply with exactly READY39 and nothing else. Use no tools.",
	}); err != nil {
		return fmt.Errorf("spawn sidecar seat: %w", err)
	}
	turns, err := s.waitHotMigrationReply(id, 0, "READY39", 180*time.Second)
	if err != nil {
		return fmt.Errorf("the seat never answered its first turn: %w", err)
	}
	before, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return err
	}

	if err := s.broker.restart(nil); err != nil {
		return fmt.Errorf("restart the isolate's broker: %w", err)
	}
	restartedAt := time.Now()

	// Back means running, with no one having sent to it: a send would
	// rehydrate it and prove nothing about the daemon's own recovery.
	deadline := time.Now().Add(4 * time.Minute)
	sawDown := false
	for {
		running, found := s.agentRunning(id)
		if !found {
			return fmt.Errorf("the seat left the registry after the broker restart")
		}
		if !running {
			sawDown = true
		}
		if running && sawDown {
			break
		}
		if time.Now().After(deadline) {
			if !sawDown {
				return fmt.Errorf("the seat never showed down after the broker restart: the journey did not take it down")
			}
			return fmt.Errorf("the seat was still down 4m after the broker restart: nothing brought it back")
		}
		time.Sleep(time.Second)
	}
	back := time.Since(restartedAt)
	after, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return err
	}

	if _, err := s.mcpText("jevons_agent_send", map[string]any{
		"name": id, "actor": "jevons",
		"text": "What word did I ask you to remember? Reply with that word and nothing else. Use no tools.",
	}); err != nil {
		return fmt.Errorf("send to the returned seat: %w", err)
	}
	if _, err := s.waitHotMigrationReply(id, turns, "MARBLE39", 180*time.Second); err != nil {
		return fmt.Errorf("the returned seat did not answer from its own conversation: %w", err)
	}
	fmt.Printf("J39: %s came back %s after the broker restart with nobody sending to it, session %s → %s, and answered MARBLE39 from its conversation\n",
		id, back.Round(time.Second), before[id].SessionID, after[id].SessionID)
	if raw, err := os.ReadFile(s.logPath); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.Contains(line, id) && (strings.Contains(line, "seat_stop") || strings.Contains(line, "re-launched") ||
				strings.Contains(line, "relaunched") || strings.Contains(line, "re-attached")) {
				fmt.Println("J39:", trim(line, 400))
			}
		}
	}
	return nil
}

// agentRunning reads name's running flag from /api/agents.
func (s *suite) agentRunning(name string) (running, found bool) {
	resp, err := http.Get("http://" + s.host + "/api/agents")
	if err != nil {
		return false, true // the daemon is busy, not the seat gone
	}
	defer resp.Body.Close()
	var rows []struct {
		Name    string `json:"name"`
		Running bool   `json:"running"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return false, true
	}
	for _, r := range rows {
		if r.Name == name {
			return r.Running, true
		}
	}
	return false, false
}

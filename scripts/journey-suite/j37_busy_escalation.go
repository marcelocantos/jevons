// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// j35InterruptAfter is the owner ladder this journey configures: short
// enough to observe, long enough that a busy turn is plainly still busy.
const j35InterruptAfter = 20 * time.Second

// jBusyEscalation (🎯T899 / claudia 🎯T138): an agent deep in its own work
// stays reachable. A worker starts a long tool call; an owner message steers
// into the turn and, because a steer cannot land mid-tool, the interrupt rung
// fires at its deadline and the worker answers it; a low-urgency message to
// the same busy worker waits for the turn boundary instead.
func (s *suite) jBusyEscalation() error {
	raw, err := os.ReadFile(s.cfgPath)
	if err != nil {
		return err
	}
	cfg := filepath.Join(s.stateDir, "config-j37.yaml")
	body := string(raw) + fmt.Sprintf("delivery_escalation:\n  owner:\n    first: steer\n    interrupt_after_seconds: %d\n", int(j35InterruptAfter.Seconds()))
	if err := os.WriteFile(cfg, []byte(body), 0o600); err != nil {
		return err
	}
	prev := s.cfgPath
	s.cfgPath = cfg
	defer func() { s.cfgPath = prev }()
	return s.withIsolatedBroker((*suite).busyEscalationWithBroker)
}

func (s *suite) busyEscalationWithBroker() error {
	id := fmt.Sprintf("orch-busy-esc-%d", time.Now().Unix()%100000)
	work := filepath.Join(s.stateDir, "busy-escalation-work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer func() {
		_, _ = s.mcpText("jevons_agent_kill", map[string]any{"name": id, "actor": "jevons", "force": true})
	}()
	if _, err := s.mcpText("jevons_agent_start", map[string]any{
		"name": id, "workdir": work, "actor": "jevons", "parent": "jevons", "purpose": "work",
		"provider": s.provider, "owner_asked": true,
		"prompt": "Run exactly this shell command with your Bash tool and wait for it to finish: sleep 150. " +
			"When it has finished, reply with exactly SLEPT. If a new message arrives from the owner, do what it says.",
	}); err != nil {
		return fmt.Errorf("spawn busy worker: %w", err)
	}
	if err := s.waitAgentPhase(id, func(p string) bool { return p != "" && p != "idle" }, 90*time.Second); err != nil {
		return fmt.Errorf("worker never became busy: %w", err)
	}
	// Give the model time to be inside the sleep, so nothing but the
	// interrupt can deliver a steered message.
	time.Sleep(15 * time.Second)
	payload, err := s.agentTranscriptHTTP(id)
	if err != nil {
		return err
	}
	base, _ := payload["turns"].([]any)

	low, err := s.ownerSurfaceSend(id, "Low-urgency note from a report: when you next reply, include the word LOWACK.", "agent")
	if err != nil {
		return fmt.Errorf("low-urgency send: %w", err)
	}
	if low.Status != "queued" {
		return fmt.Errorf("a low-urgency message to a busy worker was %q, want queued behind the turn: %+v", low.Status, low)
	}
	sent := time.Now()
	urgent, err := s.ownerSurfaceSend(id, "Owner here: stop what you are doing and reply with exactly URGENTACK.", "owner")
	if err != nil {
		return fmt.Errorf("owner send: %w", err)
	}
	if urgent.Status != "steered" || urgent.InterruptAfterMS != j35InterruptAfter.Milliseconds() {
		return fmt.Errorf("the owner's message to a busy worker was not escalated: %+v", urgent)
	}
	if _, err := s.waitHotMigrationReply(id, len(base), "URGENTACK", j35InterruptAfter+60*time.Second); err != nil {
		return fmt.Errorf("the worker did not answer the owner within the ladder: %w", err)
	}
	took := time.Since(sent)
	if took > j35InterruptAfter+60*time.Second {
		return fmt.Errorf("owner answered after %s, beyond the %s bound", took, j35InterruptAfter)
	}
	// The low-urgency message waited for the turn boundary and is taken
	// after it, not dropped.
	if _, err := s.waitHotMigrationReply(id, len(base), "LOWACK", 120*time.Second); err != nil {
		return fmt.Errorf("the held low-urgency message never reached the worker: %w", err)
	}
	fmt.Printf("J37: owner message answered %s after a send to a worker busy in sleep 150 (ladder: steer, interrupt after %s)\n",
		took.Round(time.Second), j35InterruptAfter)
	return nil
}

// sendResult is POST /api/agents/{name}/send's answer.
type sendResult struct {
	Status           string `json:"status"`
	Message          string `json:"message"`
	Mechanism        string `json:"mechanism"`
	InterruptAfterMS int64  `json:"interrupt_after_ms"`
}

// ownerSurfaceSend posts a message the way the cockpit does, as the owner
// or as an agent/system notification.
func (s *suite) ownerSurfaceSend(name, text, origin string) (sendResult, error) {
	body, _ := json.Marshal(map[string]string{"text": text, "origin": origin})
	resp, err := http.Post("http://"+s.host+"/api/agents/"+name+"/send", "application/json", bytes.NewReader(body))
	if err != nil {
		return sendResult{}, err
	}
	defer resp.Body.Close()
	var out sendResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("HTTP %d: %v (%+v)", resp.StatusCode, err, out)
	}
	return out, nil
}

// waitAgentPhase polls /api/agents until name's phase satisfies ok.
func (s *suite) waitAgentPhase(name string, ok func(string) bool, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	last := ""
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + s.host + "/api/agents")
		if err == nil {
			var rows []struct {
				Name  string `json:"name"`
				Phase string `json:"phase"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&rows)
			resp.Body.Close()
			for _, r := range rows {
				if r.Name == name {
					last = r.Phase
					if ok(r.Phase) {
						return nil
					}
				}
			}
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("phase still %q after %s", last, timeout)
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/agenterr"
)

// j21GoalContinuesAllBackends is the 🎯T510 product journey: a work mint
// must set AgentDef.Goal so the Session host starts a second turn after
// the first terminal, with no jevons_agent_send continue. Claude, Grok,
// and Codex each get a real worker. A missing CLI is OUTAGE (🎯T283),
// not skip-and-green. A backend that starts a first turn and then sits
// idle is a FAIL.
func (s *suite) j21GoalContinuesAllBackends() error {
	if s.host == "" || strings.HasSuffix(s.host, ":13705") {
		return fmt.Errorf("J21 refuses development port")
	}

	type backend struct {
		name     string
		provider string
	}
	all := []backend{
		{name: "claude", provider: string(claudia.ProviderClaude)},
		{name: "grok", provider: string(claudia.ProviderGrok)},
		{name: "codex", provider: string(claudia.ProviderCodex)},
	}

	// Backends run one after another: three concurrent launches made the
	// journey create its own load, which is what pushed a start past its
	// call deadline (🎯T625.8).
	var (
		ran     []string
		failed  []string
		outages []error
	)
	for _, b := range all {
		if err := backendCLIReady(b.provider); err != nil {
			outages = append(outages, &outageError{
				step:  "J21-" + b.name,
				class: agenterr.ClassBackendUnavailable,
				msg:   err.Error(),
			})
			continue
		}
		if err := s.goalContinueOneBackend(b.provider); err != nil {
			if isOutage(err) {
				outages = append(outages, err)
				continue
			}
			failed = append(failed, b.name+": "+err.Error())
			continue
		}
		ran = append(ran, b.name)
	}

	if len(failed) > 0 {
		return fmt.Errorf("goal continuation failed: %s", strings.Join(failed, "; "))
	}
	if len(ran) == 0 {
		if len(outages) == 0 {
			return fmt.Errorf("J21 ran no backends")
		}
		return outages[0]
	}
	return nil
}

func (s *suite) goalContinueOneBackend(provider string) error {
	name := fmt.Sprintf("jv-t510-%s-%d", provider, time.Now().UnixNano()%1e6)
	work := filepath.Join(s.stateDir, "t510-"+provider)
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	// Force: a worker mid-turn refuses a plain kill (🎯T664), and a survivor
	// keeps T510 engaged, so the next backend's start was refused.
	defer func() {
		_, _ = s.MCPToolCall("jevons_agent_kill", map[string]any{"name": name, "actor": "jevons", "force": true})
	}()

	// First user turn must not close the Goal.
	prompt := "Reply with exactly: ping. Do not emit any GOAL_STATUS line."
	_, err := s.MCPToolCall("jevons_agent_start", map[string]any{
		"name": name, "workdir": work, "actor": "jevons", "parent": "jevons",
		"purpose": "work", "provider": provider, "target_id": "T510",
		"prompt": prompt,
	})
	if err != nil && isStartPending(err) {
		// 🎯T792: the launch outlived this call's deadline and continues in
		// the daemon. That is "in flight", not failure: wait for the seat to
		// register as running (never re-send the brief).
		err = s.waitStartRunning(name)
	}
	if err != nil {
		if oe := asOutage("J21-"+provider, err); oe != nil {
			return oe
		}
		return fmt.Errorf("start %s: %w", provider, err)
	}

	deadline := time.Now().Add(turnTimeout + 30*time.Second)
	var (
		lastUsers  int
		sawWorking bool
		sawIdle    bool
		lastPhase  string
	)
	for time.Now().Before(deadline) {
		payload, err := s.agentTranscriptHTTP(name)
		if err != nil {
			return fmt.Errorf("%s transcript: %w", provider, err)
		}
		users := countTranscriptRole(payload, "user")
		lastUsers = users
		if users >= 2 {
			// Host issued the Goal continuation. Do not AgentSend.
			return nil
		}
		// A sidecar seat's transcript is built from its events, and the
		// host's own continuation prompt is not one, so the second turn
		// shows as assistant output after the first turn's terminal reply
		// (it may still be working: the continuation can be long). The
		// phase can go idle and working again between two polls. Claude is
		// excluded: one Claude turn can repeat its terminal message across
		// content blocks, and its transcript carries the prompt anyway.
		if provider != "claude" && repliesAfterFirstTerminal(payload) > 0 {
			return nil
		}
		// Codex (and any backend whose session is not a Claude JSONL)
		// leaves /transcript empty. Phase still moves on the live
		// event stream: working → idle → working is the second turn.
		phase := s.agentPhase(name)
		if phase != "" {
			lastPhase = phase
		}
		switch phase {
		case "working":
			if sawIdle {
				return nil
			}
			sawWorking = true
		case "idle":
			if sawWorking {
				sawIdle = true
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("%s: first turn ended (or never started) and no host continuation (user turns=%d phase=%q)", provider, lastUsers, lastPhase)
}

func (s *suite) agentPhase(name string) string {
	agents, err := s.ListAgentsHTTP()
	if err != nil {
		return ""
	}
	for _, a := range agents {
		if a.Name == name {
			return a.Phase
		}
	}
	return ""
}

func countTranscriptRole(payload map[string]any, role string) int {
	turns, _ := payload["turns"].([]any)
	n := 0
	for _, raw := range turns {
		turn, _ := raw.(map[string]any)
		if turn["role"] == role {
			n++
		}
	}
	return n
}

func backendCLIReady(provider string) error {
	switch provider {
	case string(claudia.ProviderClaude):
		if _, err := exec.LookPath("claude"); err != nil {
			return fmt.Errorf("claude not on PATH")
		}
		if _, err := exec.LookPath("tmux"); err != nil {
			return fmt.Errorf("tmux required for Claude Session")
		}
	case string(claudia.ProviderGrok):
		if _, err := exec.LookPath("grok"); err != nil {
			return fmt.Errorf("grok not on PATH")
		}
	case string(claudia.ProviderCodex):
		if _, err := exec.LookPath("codex"); err == nil {
			return nil
		}
		if _, err := os.Stat("/Applications/ChatGPT.app/Contents/Resources/codex"); err == nil {
			return nil
		}
		return fmt.Errorf("codex not on PATH or ChatGPT.app bundle")
	default:
		return fmt.Errorf("unknown provider %q", provider)
	}
	return nil
}

// startJoinTimeout bounds how long a detached start (🎯T792) may take to
// register the seat before the journey calls it failed.
const startJoinTimeout = 3 * time.Minute

// isStartPending reports whether err is the 🎯T792 pending result of
// jevons_agent_start (internal/mcpserver startPendingText): the launch is
// still running in the daemon past the call's deadline.
func isStartPending(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// MCPToolCall trims the error text, so match the early phrase too.
	return strings.Contains(msg, "still running past this call's deadline") ||
		strings.Contains(msg, "CONTINUES in the daemon") || strings.Contains(msg, "T792")
}

// waitStartRunning polls the fleet list until name is registered and
// running, observing the in-flight launch as the pending text directs.
func (s *suite) waitStartRunning(name string) error {
	deadline := time.Now().Add(startJoinTimeout)
	for time.Now().Before(deadline) {
		if agents, err := s.ListAgentsHTTP(); err == nil {
			for _, a := range agents {
				if a.Name == name && a.Status == "running" {
					return nil
				}
			}
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("detached start of %s never registered as running within %s", name, startJoinTimeout)
}

// repliesAfterFirstTerminal counts assistant rows after the first one that
// ends a turn: output of a later turn.
func repliesAfterFirstTerminal(payload map[string]any) int {
	turns, _ := payload["turns"].([]any)
	n, ended := 0, false
	for _, raw := range turns {
		turn, _ := raw.(map[string]any)
		if turn["role"] != "assistant" {
			continue
		}
		if ended {
			n++
			continue
		}
		ev, _ := turn["raw"].(map[string]any)
		msg, _ := ev["message"].(map[string]any)
		if stop, _ := msg["stop_reason"].(string); stop == "end_turn" {
			ended = true
		}
	}
	return n
}

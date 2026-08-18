// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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
		return fmt.Errorf("J21 refuses daily port")
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

	var (
		mu      sync.Mutex
		ran     []string
		failed  []string
		outages []error
		wg      sync.WaitGroup
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
		wg.Add(1)
		go func(b backend) {
			defer wg.Done()
			if err := s.goalContinueOneBackend(b.provider); err != nil {
				mu.Lock()
				defer mu.Unlock()
				if isOutage(err) {
					outages = append(outages, err)
					return
				}
				failed = append(failed, b.name+": "+err.Error())
				return
			}
			mu.Lock()
			ran = append(ran, b.name)
			mu.Unlock()
		}(b)
	}
	wg.Wait()

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
	defer func() {
		_, _ = s.AgentKill(name, "jevons")
	}()

	// First user turn must not close the Goal.
	prompt := "Reply with exactly: ping. Do not emit any GOAL_STATUS line."
	_, err := s.MCPToolCall("jevons_agent_start", map[string]any{
		"name": name, "workdir": work, "actor": "jevons", "parent": "jevons",
		"purpose": "work", "provider": provider, "target_id": "T510",
		"prompt": prompt,
	})
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

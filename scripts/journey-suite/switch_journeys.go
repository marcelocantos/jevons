// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cli"
)

// Model-switch journeys (claudia 🎯T195). A seat switches provider in place:
// it keeps its name, its session and its whole conversation, so the oracle is
// stronger than a migration's ever was — the session id must not change, and
// the agent must answer from the conversation itself, not from a summary.
//
// Each journey plants a fact in a turn on the source provider, switches, and
// asks for it on the destination. Nothing but that conversation records the
// fact, so a correct answer means the destination model read it.

// switchJourneyDestination is the other plan a journey moves to. Codex
// and Claude carry the moves, so the journeys do not spend Cursor.
func switchJourneyDestination(from claudia.Provider) claudia.Provider {
	if cli.PlanProvider(from) == claudia.ProviderCodex {
		return claudia.ProviderClaude
	}
	return claudia.ProviderCodex
}

// switchedInPlace checks the registry row after a switch: on the
// destination plan, on the same session.
func switchedInPlace(source, after claudia.AgentDef, to claudia.Provider) error {
	if cli.PlanProvider(after.Provider) != cli.PlanProvider(to) {
		return fmt.Errorf("seat is on %s, want %s", after.Provider, to)
	}
	if after.SessionID == "" || after.SessionID != source.SessionID {
		return fmt.Errorf("switch replaced the session: before %q, after %q", source.SessionID, after.SessionID)
	}
	return nil
}

// recallWithin asks id for want until a reply carries it.
func (s *suite) recallWithin(id, probe, want string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastReply string
	var lastErr error
	for time.Now().Before(deadline) {
		reply, err := s.mcpText("jevons_thread_direct", map[string]any{"id": id, "text": probe})
		if err == nil && strings.Contains(strings.ToUpper(reply), want) {
			return nil
		}
		lastReply, lastErr = reply, err
		time.Sleep(4 * time.Second)
	}
	return fmt.Errorf("no recall of %s within %s: reply=%q err=%v", want, timeout, trim(lastReply, 200), lastErr)
}

func (s *suite) jProviderSwitch() error {
	return s.withIsolatedBroker((*suite).providerSwitchWithBroker)
}

func (s *suite) jStoppedProviderSwitch() error {
	return s.withIsolatedBroker((*suite).stoppedProviderSwitchWithBroker)
}

func (s *suite) jOverseerSwitch() error {
	return s.withIsolatedBroker((*suite).overseerSwitchWithBroker)
}

// A running seat switches between turns and answers on the destination from
// its own conversation; a restart keeps it there.
func (s *suite) providerSwitchWithBroker() error {
	id := fmt.Sprintf("orch-switch-%d", time.Now().Unix()%100000)
	work := filepath.Join(s.stateDir, "switch-work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer func() { _, _ = s.mcpText("jevons_thread_remove", map[string]any{"id": id}) }()
	if _, err := s.mcpText("jevons_thread_spawn", map[string]any{
		"id": id, "workdir": work, "description": "journey switch worker",
		// The source request is the isolate's provider. An unsteerable
		// pin is dropped at mint (🎯T791); the switch below follows the
		// plan that actually started.
		"provider": string(cli.PlanProvider(s.provider)),
	}); err != nil {
		return fmt.Errorf("spawn: %w", err)
	}
	// One unhyphenated token: a hyphenated probe came back truncated at the
	// first hyphen on the Grok direct path (🎯T286).
	const passphrase = "BLUEOTTER42"
	if _, err := s.mcpText("jevons_thread_direct", map[string]any{
		"id": id, "text": "Remember this for later — the mission passphrase is " + passphrase + ". Reply with exactly: STORED",
	}); err != nil {
		return fmt.Errorf("plant fact: %w", err)
	}
	before, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return fmt.Errorf("source registry: %w", err)
	}
	source := before[id]
	if source.SessionID == "" {
		return fmt.Errorf("source identity is not the running provider: %+v", source)
	}
	if err := expectLandedProvider(s.provider, source.Provider); err != nil {
		return fmt.Errorf("source identity: %w", err)
	}
	to := switchJourneyDestination(cli.PlanProvider(source.Provider))
	// owner_asked: the journey plays the owner requesting this move (🎯T561
	// refuses an un-asked leave of a provider with weekly remaining).
	out, err := s.mcpText("jevons_agent_switch_model", map[string]any{
		"name": id, "provider": string(to), "owner_asked": true,
	})
	if err != nil {
		return fmt.Errorf("switch %s → %s: %w (%s)", s.provider, to, err, trim(out, 200))
	}
	if !strings.Contains(out, "in place") {
		return fmt.Errorf("a running seat did not switch in place: %s", trim(out, 200))
	}
	after, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return fmt.Errorf("destination registry: %w", err)
	}
	if err := switchedInPlace(source, after[id], to); err != nil {
		return err
	}
	const probe = "What is the mission passphrase? Reply with the passphrase only."
	if err := s.recallWithin(id, probe, passphrase, 2*time.Minute); err != nil {
		return fmt.Errorf("switched seat on %s: %w", to, err)
	}
	// Control: the plant must not be in the isolate persona, or the recall
	// proves nothing about the conversation.
	cfg, err := os.ReadFile(filepath.Join(s.stateDir, "config.yaml"))
	if err != nil {
		return fmt.Errorf("isolate config: %w", err)
	}
	if strings.Contains(strings.ToUpper(string(cfg)), passphrase) {
		return fmt.Errorf("passphrase leaked into isolate config — the probe proves nothing")
	}
	if err := s.bounceDrain(); err != nil {
		return fmt.Errorf("restart after switch: %w", err)
	}
	reopened, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return fmt.Errorf("reopened registry: %w", err)
	}
	if err := switchedInPlace(source, reopened[id], to); err != nil {
		return fmt.Errorf("after restart: %w", err)
	}
	if err := s.recallWithin(id, probe, passphrase, 90*time.Second); err != nil {
		return fmt.Errorf("after restart: %w", err)
	}
	return nil
}

// A stopped seat has its row rewritten on the same session; its next start
// resumes the conversation on the destination.
func (s *suite) stoppedProviderSwitchWithBroker() error {
	id := fmt.Sprintf("orch-stopped-switch-%d", time.Now().Unix()%100000)
	work := filepath.Join(s.stateDir, "stopped-switch-work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer func() { _, _ = s.mcpText("jevons_thread_remove", map[string]any{"id": id}) }()
	to := switchJourneyDestination(s.provider)
	if _, err := s.mcpText("jevons_thread_spawn", map[string]any{
		"id": id, "workdir": work, "description": "journey stopped switch worker",
		"provider": string(cli.PlanProvider(s.provider)),
	}); err != nil {
		return fmt.Errorf("spawn: %w", err)
	}
	const codeword = "AMBERPINE59"
	if _, err := s.mcpText("jevons_thread_direct", map[string]any{
		"id": id, "text": "Remember this mission codeword: " + codeword + ". Reply exactly: STORED",
	}); err != nil {
		return fmt.Errorf("plant codeword: %w", err)
	}
	before, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return err
	}
	source := before[id]
	if _, err := s.mcpText("jevons_agent_stop", map[string]any{
		"name": id, "force": true, "reason": "journey: test switching a stopped seat",
	}); err != nil {
		return fmt.Errorf("stop seat: %w", err)
	}
	out, err := s.mcpText("jevons_agent_switch_model", map[string]any{
		"name": id, "provider": string(to), "owner_asked": true,
	})
	if err != nil {
		return fmt.Errorf("switch stopped seat: %w (%s)", err, trim(out, 200))
	}
	if !strings.Contains(out, "while stopped") {
		return fmt.Errorf("a stopped seat did not switch as stopped: %s", trim(out, 200))
	}
	after, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return err
	}
	if err := switchedInPlace(source, after[id], to); err != nil {
		return err
	}
	// The direct rehydrates the stopped seat on the destination.
	return s.recallWithin(id, "What is the mission codeword? Reply with the codeword only.", codeword, 3*time.Minute)
}

// The overseer switches from the fleet tree's route and keeps answering on
// owner chat, which stays attached: there is no relaunch to re-attach.
func (s *suite) overseerSwitchWithBroker() error {
	to := switchJourneyDestination(s.provider)
	const codeword = "TANGERINEHARBOUR77"
	ctx, cancel := context.WithTimeout(context.Background(), 3*turnTimeout)
	defer cancel()
	conn, frames, err := dialChat(ctx, s.host)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	if _, err := drainReplay(frames, 800*time.Millisecond); err != nil {
		return err
	}
	plant := "Remember this for the rest of our work — the project codeword is " + codeword + ". Reply with exactly: NOTED"
	if err := conn.Write(ctx, websocket.MessageText, []byte(plant)); err != nil {
		return err
	}
	if _, _, terminal, err := waitTurn(ctx, frames, codeword, true); err != nil || !terminal {
		return fmt.Errorf("plant codeword: terminal=%v err=%v", terminal, err)
	}
	before, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{"provider": string(to)})
	if err != nil {
		return err
	}
	if err := postSwitchWhenSettled(ctx, "http://"+s.host+"/api/agents/switch-model", body); err != nil {
		return err
	}
	after, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return err
	}
	if err := switchedInPlace(before[overseerName], after[overseerName], to); err != nil {
		return fmt.Errorf("overseer: %w", err)
	}
	// The same chat connection reaches the switched overseer.
	for attempt := 1; attempt <= 5; attempt++ {
		if err := conn.Write(ctx, websocket.MessageText,
			[]byte("What is the project codeword? Reply with the codeword only.")); err != nil {
			return err
		}
		_, text, _, err := waitTurn(ctx, frames, "codeword", true)
		if err == nil && strings.Contains(strings.ToUpper(text), codeword) {
			return nil
		}
		time.Sleep(10 * time.Second)
	}
	return fmt.Errorf("overseer on %s never recalled the codeword after the switch", to)
}

// switchSettleRetry is how often a switch refused for a running turn is
// re-asked.
const switchSettleRetry = 2 * time.Second

// postSwitchWhenSettled asks the daemon to switch a seat and treats its own
// "turn in flight" refusal as the settle oracle (🎯T625.9). The plant turn
// ending does not mean the overseer is idle: a notice the daemon deferred
// behind that turn starts as it ends. Any other refusal fails at once, and a
// turn that never settles fails at ctx's deadline, so the guard itself stays
// under test.
func postSwitchWhenSettled(ctx context.Context, url string, body []byte) error {
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("switch: %w", err)
		}
		var out map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&out)
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return nil
		}
		msg := fmt.Sprint(out["error"])
		if resp.StatusCode != http.StatusConflict ||
			!(strings.Contains(msg, "turn in flight") || strings.Contains(msg, "turn is in flight")) {
			return fmt.Errorf("switch HTTP %d: %v", resp.StatusCode, out["error"])
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("switch: turn never settled: %s", msg)
		case <-time.After(switchSettleRetry):
		}
	}
}

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
	"strings"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/planusage"
	"github.com/marcelocantos/jevons/internal/seatprovider"
)

// A live seat on a hot provider must follow Claudia's placement verdict once,
// switching in place (claudia 🎯T195): same session, same conversation, and
// it stays on the chosen destination after the daemon restarts. The worker
// starts on Claude, which the T234 override makes hot. The destination is the
// isolate's own subscription provider (Grok or Codex), which stays healthy
// so the overseer does not move.
// Run it on whichever of the two has real allowance left: the override only
// shapes Claudia's placement, and the switched seat's turns spend the
// destination plan for real.
func (s *suite) jHotProviderSwitch() error {
	return s.withIsolatedBroker((*suite).hotProviderSwitchWithBroker)
}

func (s *suite) hotProviderSwitchWithBroker() error {
	dest := seatprovider.Resolve(s.provider).Plan.AsProvider()
	if dest != claudia.ProviderGrok && dest != claudia.ProviderCodex {
		return fmt.Errorf("J34 requires a Grok or Codex isolate, got %s", s.provider)
	}
	id := fmt.Sprintf("orch-hot-switch-%d", time.Now().Unix()%100000)
	work := filepath.Join(s.stateDir, "hot-switch-work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer func() {
		_, _ = s.mcpText("jevons_agent_kill", map[string]any{"name": id, "actor": "jevons", "force": true})
	}()
	const codeword = "COPPERFINCH83"
	if _, err := s.mcpText("jevons_agent_start", map[string]any{
		"name": id, "workdir": work, "actor": "jevons", "parent": "jevons", "purpose": "work",
		"provider": "claude", "model": "claude-sonnet-5", "owner_asked": true,
		"prompt": "Your continuing task is to remember the mission codeword " + codeword +
			" for a later question. Reply with exactly ACK " + codeword + " and remain available; do not declare the task complete.",
	}); err != nil {
		return fmt.Errorf("spawn Claude worker: %w", err)
	}
	if _, err := s.waitReplyContaining(id, 0, codeword, 90*time.Second); err != nil {
		return fmt.Errorf("worker did not acknowledge codeword: %w", err)
	}
	// The reply is visible, but a provider may still report the turn as in
	// flight. This disposable worker explicitly allows the host to end that
	// turn before switching; production seats default to waiting.
	if _, err := s.mcpText("jevons_agent_provider_policy", map[string]any{
		"name": id, "actor": "jevons", "allow_interrupt": true,
	}); err != nil {
		return fmt.Errorf("opt journey worker into interruption: %w", err)
	}
	before, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return err
	}
	source := before[id]
	if seatprovider.Resolve(source.Provider).Plan != seatprovider.Claude || source.SessionID == "" {
		return fmt.Errorf("worker did not start on Claude: %+v", source)
	}

	// Only Claude becomes hot; the isolate provider remains an eligible
	// destination for this worker and the healthy overseer does not need
	// to move. Seeded through Claudia T234 — not a fixture file the
	// production daemon refuses.
	if err := s.pushAndAwait(hotSwitchOverrideReadings(string(dest))); err != nil {
		return fmt.Errorf("seed hot-switch usage: %w", err)
	}
	resp, err := http.Get("http://" + s.host + "/api/plan-usage/decisions")
	if err != nil {
		return err
	}
	var decisions []planusage.PlanAction
	decodeErr := json.NewDecoder(resp.Body).Decode(&decisions)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || decodeErr != nil {
		return fmt.Errorf("placement decisions: HTTP %d: %v", resp.StatusCode, decodeErr)
	}
	var choice *planusage.PlanAction
	for i := range decisions {
		if decisions[i].Name == id {
			choice = &decisions[i]
			break
		}
	}
	// The cockpit's periodic sweep can carry the move out between the
	// readings landing and this read (gate 72e58bfb, 2026-10-29: the worker
	// read From:codex, Action:stay — already on the destination). Then the
	// evidence is the move itself: the daemon's own policy-switch line for
	// this seat and the same session on the destination plan.
	movedAlready := false
	if choice != nil && choice.Action != planusage.SeatMigrate &&
		seatprovider.Resolve(claudia.Provider(choice.From)).Plan.AsProvider() == dest {
		if now, err := bounceRegistrySnapshot(s.agentsPath()); err == nil && switchedInPlace(source, now[id], dest) == nil {
			if logs, err := os.ReadFile(s.logPath); err == nil &&
				strings.Contains(string(logs), `msg="plan policy switched" name=`+id+" ") {
				movedAlready = true
			}
		}
	}
	if !movedAlready && (choice == nil || choice.Author != claudia.DecisionAuthor || choice.Action != planusage.SeatMigrate ||
		seatprovider.Resolve(claudia.Provider(choice.To)).Plan.AsProvider() != dest || choice.Reason == "") {
		return fmt.Errorf("Claudia did not explain an eligible move: choice=%+v decisions=%+v", choice, decisions)
	}
	resp, err = http.Post("http://"+s.host+"/api/plan-usage/sweep", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	var actions []planusage.PlanAction
	decodeErr = json.NewDecoder(resp.Body).Decode(&actions)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || decodeErr != nil {
		return fmt.Errorf("placement sweep: HTTP %d: %v", resp.StatusCode, decodeErr)
	}
	// The cockpit's periodic SweepPlanPolicy can win the planSweepMu
	// race and execute the move before this POST returns the action
	// list (live 2026-10-28: sweep HTTP [] while the isolate log
	// already had plan policy switched / fleet_switch.switched).
	// The registry is the product outcome.
	after, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return err
	}
	destination := after[id]
	if err := switchedInPlace(source, destination, dest); err != nil {
		return fmt.Errorf("hot worker did not switch: sweep=%+v: %w", actions, err)
	}
	probe := "What is the mission codeword? Reply with the codeword only."
	// A probe sent while a turn is open is steered into it and answered as
	// part of that turn (2026-10-06 gate). Ask once the seat has settled.
	if err := s.waitAgentPhase(id, func(p string) bool { return p == "idle" }, 2*time.Minute); err != nil {
		return fmt.Errorf("hot worker never settled after the switch: %w", err)
	}
	payload, err := s.agentTranscriptHTTP(id)
	if err != nil {
		return err
	}
	beforeProbe, _ := payload["turns"].([]any)
	if _, err := s.mcpText("jevons_agent_send", map[string]any{
		"name": id, "actor": "jevons", "text": probe,
	}); err != nil {
		return fmt.Errorf("ask switched worker: %w", err)
	}
	if _, err := s.waitReplyContaining(id, len(beforeProbe), codeword, 90*time.Second); err != nil {
		return fmt.Errorf("switched worker lost context: %w", err)
	}
	if err := s.bounceDrain(); err != nil {
		return fmt.Errorf("restart after hot switch: %w", err)
	}
	reopened, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return err
	}
	if got := reopened[id]; got.SessionID != destination.SessionID ||
		seatprovider.Resolve(got.Provider).Plan.AsProvider() != dest {
		return fmt.Errorf("hot seat moved again after restart: destination=%+v reopened=%+v", destination, got)
	}
	// The broker resumes the seat with a restart nudge, so a turn is open
	// right after the restart. A probe sent now is steered into that turn
	// with the standing brief, and the answer is to the nudge. This journey
	// guards context retention, not steering (J35 covers that): ask once the
	// restart turn is over.
	if err := s.waitAgentPhase(id, func(p string) bool { return p == "idle" }, 2*time.Minute); err != nil {
		return fmt.Errorf("hot destination never settled after restart: %w", err)
	}
	payload, err = s.agentTranscriptHTTP(id)
	if err != nil {
		return err
	}
	beforeProbe, _ = payload["turns"].([]any)
	if _, err := s.mcpText("jevons_agent_send", map[string]any{
		"name": id, "actor": "jevons", "text": probe,
	}); err != nil {
		return fmt.Errorf("ask switched worker after restart: %w", err)
	}
	if _, err := s.waitReplyContaining(id, len(beforeProbe), codeword, 90*time.Second); err != nil {
		return fmt.Errorf("hot destination lost context after restart: %w", err)
	}
	return nil
}

func (s *suite) waitReplyContaining(name string, after int, want string, timeout time.Duration) (int, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		payload, err := s.agentTranscriptHTTP(name)
		if err != nil {
			return 0, err
		}
		turns, _ := payload["turns"].([]any)
		if after > len(turns) {
			return 0, fmt.Errorf("transcript shrank from %d to %d turns", after, len(turns))
		}
		for _, item := range turns[after:] {
			turn, _ := item.(map[string]any)
			if turn["role"] != "assistant" && turn["role"] != "agent_note" {
				continue
			}
			raw, _ := json.Marshal(turn["raw"])
			if strings.Contains(strings.ToUpper(string(raw)), strings.ToUpper(want)) {
				return len(turns), nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return 0, fmt.Errorf("no assistant reply containing %q within %s", want, timeout)
}

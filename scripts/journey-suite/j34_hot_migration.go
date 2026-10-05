// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/cli"
	"github.com/marcelocantos/jevons/internal/planusage"
)

// A live seat on a hot provider must follow Claudia's placement verdict once,
// retain its predecessor context, and stay on the chosen destination after
// the daemon restarts. The destination is the isolate's own subscription
// provider (Grok or Codex), which stays healthy so the overseer does not move.
// Run it on whichever of the two has real allowance left: the fixture only
// shapes Claudia's placement, and the transfer and successor turns spend the
// destination plan for real.
func (s *suite) jHotProviderMigration() error {
	return s.withIsolatedBroker((*suite).hotProviderMigrationWithBroker)
}

func (s *suite) hotProviderMigrationWithBroker() error {
	dest := cli.PlanProvider(s.provider)
	if dest != claudia.ProviderGrok && dest != claudia.ProviderCodex {
		return fmt.Errorf("J34 requires a Grok or Codex isolate, got %s", s.provider)
	}
	id := fmt.Sprintf("orch-hot-mig-%d", time.Now().Unix()%100000)
	work := filepath.Join(s.stateDir, "hot-migrate-work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer func() {
		_, _ = s.mcpText("jevons_agent_kill", map[string]any{"name": id, "actor": "jevons", "force": true})
	}()
	const codeword = "COPPERFINCH83"
	if _, err := s.mcpText("jevons_agent_start", map[string]any{
		"name": id, "workdir": work, "actor": "jevons", "parent": "jevons", "purpose": "work",
		"provider": "cursor", "model": "composer-2.5", "owner_asked": true,
		"prompt": "Your continuing task is to remember the mission codeword " + codeword +
			" for a later question. Reply with exactly ACK " + codeword + " and remain available; do not declare the task complete.",
	}); err != nil {
		return fmt.Errorf("spawn Cursor worker: %w", err)
	}
	if _, err := s.waitHotMigrationReply(id, 0, codeword, 90*time.Second); err != nil {
		return fmt.Errorf("predecessor did not acknowledge codeword: %w", err)
	}
	// The reply is visible, but a provider may still report the turn as in
	// flight. This disposable worker explicitly allows the host to finish
	// that turn before migration; production seats default to waiting.
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
	if cli.PlanProvider(source.Provider) != claudia.ProviderCursor || source.SessionID == "" {
		return fmt.Errorf("worker did not start on Cursor: %+v", source)
	}

	// The isolate already reads this file on each placement check. Only
	// Cursor becomes hot; the isolate provider remains an eligible destination for this
	// worker and the healthy overseer does not need to move.
	hot := planFixtureSnapshot("cursor", 0, 100)
	green := planFixtureSnapshot(string(dest), defaultPlanRemaining, defaultPlanUsed)
	hot.Backends = append(hot.Backends, green.Backends...)
	raw, err := json.Marshal(hot)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(s.stateDir, planFixtureFile), raw, 0o600); err != nil {
		return err
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
	if choice == nil || choice.Author != claudia.DecisionAuthor || choice.Action != planusage.SeatMigrate ||
		cli.PlanProvider(claudia.Provider(choice.To)) != dest || choice.Reason == "" {
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
	moved := false
	for _, a := range actions {
		if a.Name == id && a.Execution == "migrated" && a.Author == claudia.DecisionAuthor {
			moved = true
		}
	}
	if !moved {
		return fmt.Errorf("hot worker did not migrate: %+v", actions)
	}
	after, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return err
	}
	destination := after[id]
	if cli.PlanProvider(destination.Provider) != dest ||
		destination.SessionID == "" || destination.SessionID == source.SessionID {
		return fmt.Errorf("migration did not persist a distinct %s destination: source=%+v destination=%+v", dest, source, destination)
	}
	if _, err := os.Stat(s.handoverPath(id)); err == nil {
		return fmt.Errorf("broker-owned migration wrote a Jevons handover")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	probe := "What is the mission codeword? Reply with the codeword only."
	payload, err := s.agentTranscriptHTTP(id)
	if err != nil {
		return err
	}
	beforeProbe, _ := payload["turns"].([]any)
	if _, err := s.mcpText("jevons_agent_send", map[string]any{
		"name": id, "actor": "jevons", "text": probe,
	}); err != nil {
		return fmt.Errorf("ask hot successor: %w", err)
	}
	if _, err := s.waitHotMigrationReply(id, len(beforeProbe), codeword, 90*time.Second); err != nil {
		return fmt.Errorf("hot successor lost context: %w", err)
	}
	if err := s.bounceDrain(); err != nil {
		return fmt.Errorf("restart after hot migration: %w", err)
	}
	reopened, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return err
	}
	if got := reopened[id]; got.SessionID != destination.SessionID ||
		cli.PlanProvider(got.Provider) != dest {
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
		return fmt.Errorf("ask hot successor after restart: %w", err)
	}
	if _, err := s.waitHotMigrationReply(id, len(beforeProbe), codeword, 90*time.Second); err != nil {
		return fmt.Errorf("hot destination lost context after restart: %w", err)
	}
	return nil
}

func (s *suite) waitHotMigrationReply(name string, after int, want string, timeout time.Duration) (int, error) {
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

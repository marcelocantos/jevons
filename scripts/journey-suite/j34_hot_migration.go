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

	"github.com/marcelocantos/jevons/internal/planusage"
)

// A live seat on a hot provider must follow Claudia's placement verdict once,
// retain its predecessor context, and stay on the chosen destination after
// the daemon restarts. The overseer stays on the healthy Codex provider.
func (s *suite) jHotProviderMigration() error {
	return s.withIsolatedBroker((*suite).hotProviderMigrationWithBroker)
}

func (s *suite) hotProviderMigrationWithBroker() error {
	if claudia.PlanProvider(s.provider) != claudia.ProviderCodex {
		return fmt.Errorf("J34 requires a Codex isolate, got %s", s.provider)
	}
	id := fmt.Sprintf("orch-hot-mig-%d", time.Now().Unix()%100000)
	work := filepath.Join(s.stateDir, "hot-migrate-work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer func() { _, _ = s.mcpText("jevons_thread_remove", map[string]any{"id": id}) }()
	if _, err := s.mcpText("jevons_thread_spawn", map[string]any{
		"id": id, "workdir": work, "description": "journey hot provider worker",
		"provider": "cursor", "model": "composer-2.5", "owner_asked": true,
	}); err != nil {
		return fmt.Errorf("spawn Cursor worker: %w", err)
	}
	const codeword = "COPPERFINCH83"
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
	if claudia.PlanProvider(source.Provider) != claudia.ProviderCursor || source.SessionID == "" {
		return fmt.Errorf("worker did not start on Cursor: %+v", source)
	}

	// The isolate already reads this file on each placement check. Only
	// Cursor becomes hot; Codex remains an eligible destination for this
	// worker and the healthy overseer does not need to move.
	hot := planFixtureSnapshot("cursor", 0, 100)
	green := planFixtureSnapshot("codex", defaultPlanRemaining, defaultPlanUsed)
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
	if choice == nil || choice.Author != claudia.DecisionAuthor || choice.Action != claudia.SeatMigrate ||
		claudia.PlanProvider(claudia.Provider(choice.To)) != claudia.ProviderCodex || choice.Reason == "" {
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
	if claudia.PlanProvider(destination.Provider) != claudia.ProviderCodex ||
		destination.SessionID == "" || destination.SessionID == source.SessionID {
		return fmt.Errorf("migration did not persist a distinct Codex destination: source=%+v destination=%+v", source, destination)
	}
	if _, err := os.Stat(s.handoverPath(id)); err == nil {
		return fmt.Errorf("broker-owned migration wrote a Jevons handover")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	probe := "What is the mission codeword? Reply with the codeword only."
	for attempt := 0; attempt < 12; attempt++ {
		reply, err := s.mcpText("jevons_thread_direct", map[string]any{"id": id, "text": probe})
		if err == nil && strings.Contains(strings.ToUpper(reply), codeword) {
			break
		}
		if attempt == 11 {
			return fmt.Errorf("hot successor lost context: reply=%q err=%v", trim(reply, 200), err)
		}
		time.Sleep(5 * time.Second)
	}
	if err := s.bounceDrain(); err != nil {
		return fmt.Errorf("restart after hot migration: %w", err)
	}
	reopened, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return err
	}
	if got := reopened[id]; got.SessionID != destination.SessionID ||
		claudia.PlanProvider(got.Provider) != claudia.ProviderCodex {
		return fmt.Errorf("hot seat moved again after restart: destination=%+v reopened=%+v", destination, got)
	}
	for attempt := 0; attempt < 3; attempt++ {
		reply, err := s.mcpText("jevons_thread_direct", map[string]any{"id": id, "text": probe})
		if err == nil && strings.Contains(strings.ToUpper(reply), codeword) {
			return nil
		}
		if attempt == 2 {
			return fmt.Errorf("hot destination lost context after restart: reply=%q err=%v", trim(reply, 200), err)
		}
		time.Sleep(3 * time.Second)
	}
	return nil
}

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

// 🎯T877: a running Grok seat stored as xai-oauth vacates once to the
// eligible Claude destination when Grok is hot, Codex is ahead, and the
// daemon default is still Grok. The seat keeps its session and conversation.
func (s *suite) jT877HotGrokSidecar() error {
	if seatprovider.Resolve(s.provider).Plan != seatprovider.Grok {
		fmt.Printf("J48-t877 skipped: requires PROVIDER=grok (got %s)\n", s.provider)
		return nil
	}
	if _, err := t877ObserveDevelopmentFleet(); err != nil {
		return err
	}
	return s.withIsolatedBroker((*suite).t877HotGrokSidecarWithBroker)
}

func t877PlanFixture() planusage.Snapshot {
	now := time.Now().UTC()
	week := planusage.DefaultWeeklyWindowSeconds
	lim := week
	reading := func(provider string, used, remaining float64, reset time.Time) planusage.Backend {
		return planusage.Backend{
			Provider: provider, Status: planusage.StatusAvailable, FetchedAt: now,
			Windows: []planusage.Window{{
				Name: planusage.WindowWeekly, UsedPercent: &used,
				RemainingPercent: &remaining, ResetsAt: &reset, LimitWindowSeconds: &lim,
			}},
		}
	}
	return planusage.Snapshot{At: now, Backends: []planusage.Backend{
		reading("grok", 87, 13, now.Add(7*24*time.Hour/2)),
		reading("codex", 30, 70, now.Add(7*24*time.Hour-time.Hour)),
		reading("claude", 93, 7, now.Add(4*time.Hour)),
	}}
}

func (s *suite) t877HotGrokSidecarWithBroker() error {
	id := fmt.Sprintf("orch-t877-%d", time.Now().Unix()%100000)
	work := filepath.Join(s.stateDir, "t877-work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer func() {
		_, _ = s.mcpText("jevons_agent_kill", map[string]any{"name": id, "actor": "jevons", "force": true})
	}()
	const codeword = "SILVERKITE19"
	if _, err := s.mcpText("jevons_agent_start", map[string]any{
		"name": id, "workdir": work, "actor": "jevons", "parent": "jevons", "purpose": "work",
		"provider": "grok", "owner_asked": true,
		"prompt": "Remember the mission codeword " + codeword +
			" for a later question. Reply with exactly ACK " + codeword + " and stay available.",
	}); err != nil {
		return fmt.Errorf("spawn grok worker: %w", err)
	}
	if _, err := s.waitReplyContaining(id, 0, codeword, 90*time.Second); err != nil {
		return fmt.Errorf("worker did not acknowledge codeword: %w", err)
	}
	before, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return err
	}
	source := before[id]
	if seatprovider.Resolve(source.Provider).Plan != seatprovider.Grok || source.SessionID == "" {
		return fmt.Errorf("worker must run on Grok sidecar identity, got %+v", source)
	}
	if _, err := s.mcpText("jevons_agent_provider_policy", map[string]any{
		"name": id, "actor": "jevons", "allow_interrupt": true,
	}); err != nil {
		return fmt.Errorf("opt journey worker into interruption: %w", err)
	}
	snap := t877PlanFixture()
	raw, err := json.Marshal(snap)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(s.stateDir, planFixtureFile), raw, 0o600); err != nil {
		return err
	}
	resp, err := http.Get("http://" + s.host + "/api/migrate/options")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("migrate/options HTTP %d", resp.StatusCode)
	}
	var menu struct {
		Providers []struct {
			Provider string `json:"provider"`
		} `json:"providers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&menu); err != nil {
		return fmt.Errorf("migrate/options decode: %w", err)
	}
	seen := map[string]int{}
	for _, row := range menu.Providers {
		seen[strings.ToLower(row.Provider)]++
	}
	if seen["xai-oauth"] > 0 {
		return fmt.Errorf("migrate menu invented unpublished xai-oauth row: %+v", menu.Providers)
	}
	for p, n := range seen {
		if n > 1 {
			return fmt.Errorf("duplicate migrate menu row for %s (%d)", p, n)
		}
	}
	resp, err = http.Get("http://" + s.host + "/api/plan-usage/decisions")
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
	if choice == nil || choice.Action != planusage.SeatMigrate || choice.To != "claude" ||
		choice.Author != claudia.DecisionAuthor {
		return fmt.Errorf("Claudia did not choose Claude for hot Grok seat: choice=%+v", choice)
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
	switched := 0
	for _, a := range actions {
		if a.Name == id && a.Execution == "switched" {
			switched++
		}
	}
	if switched != 1 {
		return fmt.Errorf("hot Grok seat must switch exactly once: %+v", actions)
	}
	resp, err = http.Post("http://"+s.host+"/api/plan-usage/sweep", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		return err
	}
	_ = json.NewDecoder(resp.Body).Decode(&actions)
	resp.Body.Close()
	for _, a := range actions {
		if a.Name == id && a.Execution == "switched" {
			return fmt.Errorf("second sweep moved the seat again: %+v", actions)
		}
	}
	after, err := bounceRegistrySnapshot(s.agentsPath())
	if err != nil {
		return err
	}
	destination := after[id]
	if err := switchedInPlace(source, destination, claudia.ProviderClaude); err != nil {
		return fmt.Errorf("hot Grok seat: %w", err)
	}
	probe := "What is the mission codeword? Reply with the codeword only."
	if err := s.waitAgentPhase(id, func(p string) bool { return p == "idle" }, 2*time.Minute); err != nil {
		return fmt.Errorf("seat never settled after switch: %w", err)
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
		return fmt.Errorf("switched worker lost handover context: %w", err)
	}
	return nil
}

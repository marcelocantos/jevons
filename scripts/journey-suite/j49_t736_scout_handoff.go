// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/marcelocantos/jevons/internal/agentreport"
	"github.com/marcelocantos/jevons/internal/envelope"
)

const t736JourneyTarget = "T736J"

// jT736ScoutHandoff (🎯T736): a phase-implement spawn-brief from the PO is
// delivered through jevons_agent_send on a running seat whose latest stored
// report is a scout-report for the same target — not only admitted by the
// phaseAdvanceHandoff classifier in mcpserver tests.
func (s *suite) jT736ScoutHandoff() error {
	return s.withIsolatedBroker((*suite).t736ScoutHandoffWithBroker)
}

func (s *suite) t736ScoutHandoffWithBroker() error {
	id := fmt.Sprintf("t736-handoff-%d", time.Now().Unix()%100000)
	work := filepath.Join(s.stateDir, "t736-scout-handoff-work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer func() {
		_, _ = s.mcpText("jevons_agent_kill", map[string]any{"name": id, "actor": "jevons-po", "force": true})
	}()

	const readyToken = "T736-PROBE-READY"
	if _, err := s.mcpText("jevons_agent_start", map[string]any{
		"name": id, "workdir": work, "actor": "jevons-po", "parent": "jevons-po", "purpose": "work",
		"provider": s.provider, "owner_asked": true,
		"prompt": "Reply with exactly: " + readyToken + " — then wait for further messages. Do not start any work.",
	}); err != nil {
		return fmt.Errorf("start probe seat: %w", err)
	}
	if err := s.waitJournalContains(id, readyToken, 150*time.Second); err != nil {
		return fmt.Errorf("probe seat never became ready: %w", err)
	}

	scoutText := t736JourneyScoutReport(t736JourneyTarget)
	if _, err := agentreport.Save(s.stateDir, id, scoutText, time.Now()); err != nil {
		return fmt.Errorf("plant scout-report: %w", err)
	}
	touch := filepath.Join(work, "scout.touch")
	if err := os.WriteFile(touch, []byte("scout\n"), 0o644); err != nil {
		return err
	}

	handoffToken := "T736-J49-" + uuid.NewString()[:8]
	impl := envelope.Format(&envelope.Message{
		Kind:         envelope.KindSpawnBrief,
		Target:       t736JourneyTarget,
		Phase:        envelope.PhaseImplement,
		SilentLedger: envelope.SilentLedgerEmpty,
		Payload:      handoffToken,
	})
	sendBody := "Implement brief for 🎯" + t736JourneyTarget + " — build from the scout ledger.\n\n" + impl

	sendOut, err := s.AgentSendAs("jevons-po", id, sendBody)
	if err != nil {
		return fmt.Errorf("handoff send: %w", err)
	}
	if strings.Contains(sendOut, "re-brief refused") {
		return fmt.Errorf("handoff was refused under T597: %s", trim(sendOut, 600))
	}
	if strings.Contains(strings.ToLower(sendOut), "error") && strings.Contains(sendOut, "not running") {
		return fmt.Errorf("handoff send failed before delivery: %s", trim(sendOut, 600))
	}

	if err := s.waitUserMessageContains(id, handoffToken, 150*time.Second); err != nil {
		return fmt.Errorf("handoff never landed in %s's transcript: %w", id, err)
	}

	wrong := "Fresh mission.\n\n" + envelope.Format(&envelope.Message{
		Kind:         envelope.KindSpawnBrief,
		Target:       "T999",
		Phase:        envelope.PhaseImplement,
		SilentLedger: envelope.SilentLedgerEmpty,
		Payload:      "wrong-target",
	})
	refuseOut, err := s.AgentSendAs("jevons-po", id, wrong)
	if err == nil && !strings.Contains(refuseOut, "re-brief refused") {
		return fmt.Errorf("wrong-target implement brief should still be refused: %s", trim(refuseOut, 400))
	}

	fmt.Printf("J49: scout-to-implement handoff delivered to live seat %s (token %s in user transcript)\n", id, handoffToken)
	return nil
}

func t736JourneyScoutReport(target string) string {
	fence := envelope.Format(&envelope.Message{
		Kind:         envelope.KindScoutReport,
		Target:       target,
		Phase:        envelope.PhaseScout,
		SilentLedger: envelope.SilentLedgerEmpty,
		Payload:      "Journey scout map for handoff gate.",
	})
	return "Scout thinking before the fence.\n\n" + fence +
		"\nClosing prose mentions a ```jevons ` fence, e.g. T582's shape — not a real opener.\n"
}

func (s *suite) waitJournalContains(name, needle string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		payload, err := s.agentTranscriptHTTP(name)
		if err != nil {
			last = err.Error()
		} else if journal, _ := payload["journal"].(string); strings.Contains(journal, needle) {
			return nil
		} else {
			last = fmt.Sprintf("journal len=%d", len(journal))
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timeout waiting for %q in journal (%s)", needle, last)
}

func (s *suite) waitUserMessageContains(name, needle string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		payload, err := s.agentTranscriptHTTP(name)
		if err != nil {
			lastErr = err
		} else if ok, lerr := userTranscriptContains(payload, needle); lerr != nil {
			lastErr = lerr
		} else if ok {
			return nil
		} else {
			lastErr = fmt.Errorf("token not yet in user turns")
		}
		time.Sleep(2 * time.Second)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("timeout")
	}
	return lastErr
}

func userTranscriptContains(payload map[string]any, needle string) (bool, error) {
	turns, _ := payload["turns"].([]any)
	for _, item := range turns {
		turn, _ := item.(map[string]any)
		role, _ := turn["role"].(string)
		if role != "user" {
			continue
		}
		raw, _ := json.Marshal(turn["raw"])
		if strings.Contains(string(raw), needle) {
			return true, nil
		}
	}
	return false, nil
}

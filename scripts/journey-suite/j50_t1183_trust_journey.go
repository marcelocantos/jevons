// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/gate"
	"github.com/marcelocantos/jevons/internal/worktree"
)

const trustOwnerPrompt = "Add a function Double(x int) int to pkg/calc that returns 2*x, with a test. " +
	"Land it through go run ./cmd/integrate with a GREEN bin/gate -clean covering pkg/calc. " +
	"When finished, reply here with one sentence that includes the landed commit SHA."

// jT1183TrustJourney (🎯T1183): one owner chat message → overseer → PO →
// worktree worker → integrate + gate → sealed owner reply citing SHA.
func (s *suite) jT1183TrustJourney() error {
	return s.withTrustJourneyIsolate(trustJourneyWithBroker)
}

func trustJourneyWithBroker(iso *suite, repo string) error {
	gateDir := filepath.Join(iso.stateDir, "gates")
	defer func() {
		_, _ = iso.mcpText("jevons_agent_kill", map[string]any{"name": "jevons-po", "actor": overseerName, "force": true})
	}()

	muxCtx, muxCancel := context.WithTimeout(context.Background(), trustJourneyBudget)
	defer muxCancel()

	conn, frames, err := dialOwnerMux(muxCtx, iso.host)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	if _, err := collectOwnerMuxReplay(muxCtx, frames); err != nil {
		return err
	}

	if err := trustEnsureRepoPO(iso, repo); err != nil {
		return err
	}

	// One owner message through the owner chat path.
	if err := writeOwnerMux(muxCtx, conn, "send", map[string]string{"text": trustOwnerPrompt}); err != nil {
		return err
	}

	pollDeadline := time.Now().Add(trustJourneyBudget)
	var (
		landedSHA string
		gateID    string
		worker    string
		poSpawn   bool
		gateRec   *gate.Record
	)
	for time.Now().Before(pollDeadline) {
		fault, err := worktree.ReadTrunkFault(repo)
		if err != nil {
			return fmt.Errorf("read fixture trunk fault: %w", err)
		}
		if fault != nil && fault.Kind == worktree.TrunkRed {
			// The PO owns the worker name; discover and stop its seats before
			// the deferred PO teardown. Do not leave token-burning turns alive.
			agents, err := iso.listAgentsHTTP()
			if err != nil {
				return err
			}
			for _, a := range agents {
				if a.Parent == "jevons-po" {
					if _, err := iso.mcpText("jevons_agent_kill", map[string]any{"name": a.Name, "actor": overseerName, "force": true}); err != nil {
						return fmt.Errorf("stop worker %s after trunk_red incident %s: %w", a.Name, fault.ID, err)
					}
				}
			}
			return fmt.Errorf("J50 worker integration refused trunk_red incident %s; fixture trunk failing tests: %s", fault.ID, strings.Join(fault.Tests, ", "))
		}
		agents, err := iso.listAgentsHTTP()
		if err != nil {
			return err
		}
		if trustPOSpawned(agents) {
			poSpawn = true
		}
		if name, ok := trustWorkerUnderPO(agents); ok {
			worker = name
		}
		if poSpawn && worker != "" && trustRepoHasDouble(repo) {
			if rec, sha, err := trustFindGreenCalcGate(gateDir, repo); err == nil {
				landedSHA = sha
				gateRec = rec
				gateID = rec.ID
				break
			}
		}
		time.Sleep(2 * time.Second)
	}
	if !poSpawn {
		return fmt.Errorf("jevons-po never appeared in fleet registry")
	}
	if worker == "" {
		return fmt.Errorf("no work seat spawned under jevons-po")
	}
	if landedSHA == "" || !trustRepoHasDouble(repo) || gateRec == nil {
		return fmt.Errorf("pkg/calc Double never landed with a GREEN pkg/calc gate")
	}

	if err := trustNudgeOverseerOwnerReply(iso, landedSHA, gateID); err != nil {
		return err
	}

	if err := waitOwnerMuxReplyMatching(muxCtx, frames, trustOwnerPrompt, landedSHA, func(text string) bool {
		return trustTextCitesSHA(text, landedSHA)
	}, nil); err != nil {
		return fmt.Errorf("owner chat sealed reply citing SHA %s (gate %s): %w; overseer's last words: %s",
			landedSHA, gateID, err, trustLastAssistantWords(iso, 3))
	}

	if err := iso.waitAgentPhase(overseerName, func(p string) bool { return p == "idle" || p == "" }, 2*time.Minute); err != nil {
		return fmt.Errorf("overseer settle after trust journey: %w", err)
	}
	transcript, err := iso.agentTranscriptHTTP(overseerName)
	if err != nil {
		return err
	}
	if err := assertTrustOwnerChat(transcript, trustOwnerPrompt, landedSHA, true); err != nil {
		return err
	}

	fmt.Printf("J50: trust journey landed %s worker=%s po_spawn=%v GATE id=%s GREEN pkg/calc provider=%s\n",
		landedSHA, worker, poSpawn, gateID, claudia.ProviderCursor)
	// Stash for finish-report citation when this file is run standalone.
	_ = os.WriteFile(filepath.Join(iso.stateDir, "trust-journey-result.txt"),
		[]byte(fmt.Sprintf("sha=%s gate=%s\n", landedSHA, gateID)), 0o600)
	return nil
}

func trustNudgeOverseerOwnerReply(iso *suite, landedSHA, gateID string) error {
	text := fmt.Sprintf(
		"[silent] T1183 trust journey: pkg/calc Double is on master at %s with GREEN gate %s. "+
			"Seal the owner chat turn now: exactly one sentence to the owner that includes that full SHA.",
		landedSHA, gateID,
	)
	_, err := iso.mcpText("jevons_agent_send", map[string]any{
		"name":  overseerName,
		"actor": overseerName,
		"text":  text,
	})
	return err
}

// trustLastAssistantWords returns the overseer's last n assistant texts, for
// a failure message. J50 failed twice at the owner-reply step and the
// isolate's state went with it; the gate log keeps only what the error says.
func trustLastAssistantWords(iso *suite, n int) string {
	transcript, err := iso.agentTranscriptHTTP(overseerName)
	if err != nil {
		return "(transcript unreadable: " + err.Error() + ")"
	}
	turns, _ := transcript["turns"].([]any)
	var said []string
	for _, raw := range turns {
		turn, _ := raw.(map[string]any)
		if role, _ := turn["role"].(string); role != "assistant" {
			continue
		}
		if text := strings.TrimSpace(transcriptTurnText(turn)); text != "" {
			said = append(said, trim(text, 300))
		}
	}
	if len(said) == 0 {
		return "(none)"
	}
	if len(said) > n {
		said = said[len(said)-n:]
	}
	return strings.Join(said, " | ")
}

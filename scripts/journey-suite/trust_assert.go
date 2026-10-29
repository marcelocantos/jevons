// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/marcelocantos/jevons/internal/gate"
	"github.com/marcelocantos/jevons/internal/silentresponse"
)

func trustFindGreenCalcGate(gateDir, repo string) (*gate.Record, string, error) {
	head, err := trustRepoHead(repo)
	if err != nil {
		return nil, "", err
	}
	store, err := gate.OpenStore(gateDir)
	if err != nil {
		return nil, "", err
	}
	recs, err := store.Recent(64)
	if err != nil {
		return nil, "", err
	}
	for _, rec := range recs {
		if rec == nil || rec.Verdict != gate.VerdictGreen || !rec.StatusKnown || rec.ExitStatus != 0 {
			continue
		}
		if rec.Tree == nil || !rec.Tree.Clean {
			continue
		}
		if !gitIsAncestor(repo, rec.Tree.Commit) {
			continue
		}
		cmd := strings.Join(rec.Command, " ")
		if !strings.Contains(cmd, "pkg/calc") && !strings.Contains(cmd, "./pkg/calc") {
			continue
		}
		return rec, head, nil
	}
	return nil, "", fmt.Errorf("no GREEN clean gate on master history covering pkg/calc (HEAD %s)", head)
}

// muxSealed is true when waitOwnerMuxReplyMatching already saw exactly one
// completed owner-chat assistant frame citing landedSHA — the wire oracle for
// 🎯T1183. The statedb transcript may still hold interim overseer prose that
// never painted in owner chat; do not fail on that after mux sealed.
func assertTrustOwnerChat(transcript map[string]any, ownerPrompt, landedSHA string, muxSealed bool) error {
	turns, _ := transcript["turns"].([]any)
	var ownerCount int
	var assistantAfter []string
	pastOwner := false
	for _, raw := range turns {
		turn, _ := raw.(map[string]any)
		role, _ := turn["role"].(string)
		text := strings.TrimSpace(transcriptTurnText(turn))
		switch role {
		case "user":
			if strings.Contains(text, "Double") || strings.Contains(text, ownerPrompt) {
				ownerCount++
				pastOwner = true
			}
		case "assistant":
			if pastOwner && text != "" {
				assistantAfter = append(assistantAfter, text)
			}
		case "agent_note":
			if pastOwner && silentresponse.IsRoutineNoise(text) {
				return fmt.Errorf("routine machine note would paint in owner chat: %s", trim(text, 200))
			}
		}
	}
	if ownerCount != 1 {
		return fmt.Errorf("want exactly one owner coding turn in overseer transcript, got %d", ownerCount)
	}
	journal, _ := transcript["journal"].(string)
	marksSHA := trustTextCitesSHA
	var visible []string
	for _, text := range assistantAfter {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if silentresponse.Is(text) || silentresponse.IsRoutineNoise(text) {
			return fmt.Errorf("routine overseer reply in owner chat: %s", trim(text, 200))
		}
		if strings.Contains(text, "[silent]") {
			return fmt.Errorf("silent marker leaked to owner chat: %s", trim(text, 120))
		}
		visible = append(visible, text)
	}
	if !marksSHA(journal, landedSHA) && !trustAnyCitesSHA(visible, landedSHA) {
		return fmt.Errorf("overseer transcript does not cite landed SHA %s", landedSHA)
	}
	var sealed, interim int
	for _, text := range visible {
		if marksSHA(text, landedSHA) {
			sealed++
			continue
		}
		interim++
	}
	if !muxSealed && interim > 0 {
		return fmt.Errorf("want one sealed owner reply (no interim overseer chat), got %d interim message(s)", interim)
	}
	if sealed > 1 {
		return fmt.Errorf("multiple overseer replies cite landed SHA")
	}
	if sealed == 0 && marksSHA(journal, landedSHA) {
		return nil
	}
	if sealed != 1 {
		return fmt.Errorf("want exactly one overseer reply citing landed SHA, got %d parsed", sealed)
	}
	return nil
}

func trustTextCitesSHA(text, landedSHA string) bool {
	if landedSHA == "" {
		return false
	}
	if strings.Contains(text, landedSHA) {
		return true
	}
	if len(landedSHA) >= 12 && strings.Contains(text, landedSHA[:12]) {
		return true
	}
	return false
}

func trustAnyCitesSHA(texts []string, landedSHA string) bool {
	for _, t := range texts {
		if trustTextCitesSHA(t, landedSHA) {
			return true
		}
	}
	return false
}

func transcriptTurnText(turn map[string]any) string {
	raw, _ := turn["raw"].(map[string]any)
	if raw == nil {
		return ""
	}
	msg, _ := raw["message"].(map[string]any)
	if msg != nil {
		if t := strings.TrimSpace(journeyContentText(msg["content"])); t != "" {
			return t
		}
	}
	return strings.TrimSpace(journeyContentText(raw["content"]))
}

func trustWorkerUnderPO(agents []AgentInfo) (string, bool) {
	for _, a := range agents {
		if a.Parent == "jevons-po" && strings.HasPrefix(a.Name, "jv-") {
			return a.Name, true
		}
	}
	return "", false
}

func trustPOSpawned(agents []AgentInfo) bool {
	for _, a := range agents {
		if a.Name == "jevons-po" {
			return true
		}
	}
	return false
}

func gitIsAncestor(repo, sha string) bool {
	cmd := exec.Command("git", "merge-base", "--is-ancestor", sha, "HEAD")
	cmd.Dir = repo
	return cmd.Run() == nil
}

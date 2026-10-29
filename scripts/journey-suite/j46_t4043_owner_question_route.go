// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/envelope"
)

// jT4043OwnerQuestionRoute (🎯T404.3): a real fleet agent's typed owner
// decision reaches the owner, and the owner's answer reaches that same
// agent — end to end, not just the hermetic fakes TestT404AnswerRoutesBackToAsker
// already covers.
//
// The agent is given the exact finish-report envelope text to emit verbatim
// (the question contract itself, 🎯T509/🎯T404, is not something an LLM is
// asked to free-form compose correctly); what this journey proves live is
// that the daemon's own intake, /api/reviews projection, and answer-routing
// — including the 🎯T416 payload-match landing in the asker's own
// transcript as a user message — all actually happen against a real seat.
func (s *suite) jT4043OwnerQuestionRoute() error {
	return s.withIsolatedBroker((*suite).ownerQuestionRouteWithBroker)
}

func (s *suite) ownerQuestionRouteWithBroker() error {
	id := fmt.Sprintf("orch-t4043-%d", time.Now().Unix()%100000)
	work := filepath.Join(s.stateDir, "t4043-owner-question-work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer func() {
		_, _ = s.mcpText("jevons_agent_kill", map[string]any{"name": id, "actor": "jevons", "force": true})
	}()

	const (
		qTarget  = "T4043J"
		qID      = "j46"
		qVersion = "1"
	)
	wantMsg := &envelope.Message{
		Kind:            envelope.KindFinishReport,
		Target:          qTarget,
		Status:          envelope.ProgressBlocked,
		Blocker:         "owner-decision-on-j46-journey",
		BlockClass:      envelope.BlockOwnerDecision,
		QuestionRepo:    work,
		QuestionID:      qID,
		QuestionVersion: qVersion,
		Question:        "For the J46 journey: red or blue?",
		QuestionAsker:   id,
		AnswerRoute:     id,
		QuestionOptions: []string{"red", "blue"},
		QuestionDefault: "red unless the owner says otherwise",
		SilentLedger:    envelope.SilentLedgerEmpty,
	}
	wantText := envelope.Format(wantMsg)

	if _, err := s.mcpText("jevons_agent_start", map[string]any{
		"name": id, "workdir": work, "actor": "jevons", "parent": "jevons", "purpose": "work",
		"provider": s.provider, "owner_asked": true,
		"prompt": "Reply with EXACTLY the following text and nothing else — no greeting, no " +
			"explanation, no markdown around it, nothing before or after it. The fenced block " +
			"below must be the very first thing in your reply:\n\n" + wantText,
	}); err != nil {
		return fmt.Errorf("spawn asking worker: %w", err)
	}

	// The daemon's intake runs on the worker's own turn-end notify, which
	// stores the report and folds the typed owner-decision block into the
	// durable owner-question index (ownerquestion.FromBlockedReport).
	deadline := time.Now().Add(150 * time.Second)
	var listText string
	for time.Now().Before(deadline) {
		out, err := s.mcpText("jevons_owner_questions", map[string]any{"op": "list"})
		if err == nil && strings.Contains(out, qID) && strings.Contains(out, id) {
			listText = out
			break
		}
		time.Sleep(2 * time.Second)
	}
	if listText == "" {
		return fmt.Errorf("the real agent %s's owner-decision block never reached jevons_owner_questions list", id)
	}
	if !strings.Contains(listText, `"silence_outcome"`) {
		return fmt.Errorf("owner-questions list is missing silence_outcome:\n%s", trim(listText, 2000))
	}
	if !strings.Contains(listText, `"red"`) || !strings.Contains(listText, `"blue"`) {
		return fmt.Errorf("owner-questions list is missing the red|blue options:\n%s", trim(listText, 2000))
	}

	// GET /api/reviews projects the same question with a default_on_silence.
	reviews, err := s.getReviews()
	if err != nil {
		return fmt.Errorf("GET /api/reviews: %w", err)
	}
	var found map[string]any
	for _, r := range reviews {
		if askID, _ := r["ask_id"].(string); askID == qID {
			found = r
			break
		}
	}
	if found == nil {
		return fmt.Errorf("the J46 question is not in GET /api/reviews: %d reviews", len(reviews))
	}
	if def, _ := found["default_on_silence"].(string); !strings.Contains(def, "red") {
		return fmt.Errorf("GET /api/reviews default_on_silence does not name the asker's stated default: %q", def)
	}

	// The "owner" answers through the same tool the cockpit uses.
	answerOut, err := s.mcpText("jevons_owner_questions", map[string]any{
		"op": "answer", "repo": work, "target": qTarget, "id": qID, "version": qVersion,
		"answer": "blue", "actor": "jevons",
	})
	if err != nil {
		return fmt.Errorf("op=answer: %w", err)
	}
	if !strings.Contains(answerOut, "routed:") {
		return fmt.Errorf("answer result does not say where the answer was routed: %s", answerOut)
	}

	// 🎯T416: the answer must land in the asker's OWN transcript as a real
	// user message — payload-match, not merely "the tool said sent".
	deadline = time.Now().Add(150 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		payload, err := s.agentTranscriptHTTP(id)
		if err != nil {
			lastErr = err
		} else if landed, lerr := ownerAnswerLandedAsUserMessage(payload); lerr == nil && landed {
			lastErr = nil
			break
		} else {
			lastErr = lerr
			if lastErr == nil {
				lastErr = fmt.Errorf("answer not yet seen as a user message in %s's transcript", id)
			}
		}
		time.Sleep(2 * time.Second)
	}
	if lastErr != nil {
		return fmt.Errorf("the owner's answer never landed in %s's transcript: %w", id, lastErr)
	}

	// The question is now answered in the durable view, and gone from the
	// outbox (both acceptance criteria 8's "route-back" and "landed").
	allOut, err := s.mcpText("jevons_owner_questions", map[string]any{"op": "all"})
	if err != nil {
		return fmt.Errorf("op=all: %w", err)
	}
	if !strings.Contains(allOut, qID) || !strings.Contains(allOut, `"state": "answered"`) {
		return fmt.Errorf("the J46 question is not recorded as answered:\n%s", trim(allOut, 2000))
	}
	openOut, err := s.mcpText("jevons_owner_questions", map[string]any{"op": "list"})
	if err != nil {
		return fmt.Errorf("op=list (open) after answer: %w", err)
	}
	if strings.Contains(openOut, qID) {
		return fmt.Errorf("the answered J46 question is still listed open:\n%s", trim(openOut, 2000))
	}

	fmt.Printf("J46: real agent %s's typed owner question reached jevons_owner_questions/api/reviews, and the owner's answer landed back in its own transcript\n", id)
	return nil
}

// getReviews GETs /api/reviews on the isolate.
func (s *suite) getReviews() ([]map[string]any, error) {
	resp, err := http.Get("http://" + s.host + "/api/reviews")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("/api/reviews HTTP %d", resp.StatusCode)
	}
	var rows []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// ownerAnswerLandedAsUserMessage is the 🎯T416 payload-match check: the
// routed answer must appear as an authored user-role turn (never merely
// inferred from growth or from the receiver's own quoted pane capture).
func ownerAnswerLandedAsUserMessage(payload map[string]any) (bool, error) {
	turns, _ := payload["turns"].([]any)
	for _, item := range turns {
		turn, _ := item.(map[string]any)
		role, _ := turn["role"].(string)
		if role != "user" {
			continue
		}
		raw, _ := json.Marshal(turn["raw"])
		text := string(raw)
		if strings.Contains(text, "Owner answer to your") && strings.Contains(text, "j46") && strings.Contains(text, ": blue") {
			return true, nil
		}
	}
	return false, nil
}

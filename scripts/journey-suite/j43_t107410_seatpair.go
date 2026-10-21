// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/marcelocantos/jevons/internal/seatpair"
)

// jSDKSeatPair is the 🎯T1074.10 live clause. The overseer is the SDK-backed
// seat. Its host tool call commits on that seat's pair, and the daemon
// executor runs the capture once.
func (s *suite) jSDKSeatPair() error {
	ctx, cancel := context.WithTimeout(context.Background(), turnTimeout)
	defer cancel()
	conn, frames, err := dialOwnerMux(ctx, s.host)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	if _, err := collectOwnerMuxReplay(ctx, frames); err != nil {
		return err
	}
	token := "journey-seatpair-" + uuid.NewString()
	before, err := s.journeyIdeas(ctx)
	if err != nil {
		return err
	}
	for _, record := range before {
		if record.Text == token {
			return fmt.Errorf("tool effect exists before the request")
		}
	}
	prompt := "Use the attached jevons_idea_capture MCP tool exactly once with text " +
		fmt.Sprintf("%q", token) + " and source mcp. This is a disposable verification record; do not triage it, spawn work, use a shell, or call the HTTP API. Do not narrate or announce your actions. After the tool succeeds, your only visible reply must be exactly two words separated by one space: the text token, then the idea ID returned by the capture tool."
	if err := writeOwnerMux(ctx, conn, "send", map[string]string{"text": prompt}); err != nil {
		return err
	}
	sawCall := false
	var returnedID string
	err = waitOwnerMuxReplyMatching(ctx, frames, prompt, token, func(text string) bool {
		_, id, ok := strings.Cut(text, token+" ")
		if !ok || strings.Count(text, token) != 1 || id == "" || strings.ContainsAny(id, " \t\r\n") {
			return false
		}
		returnedID = id
		return true
	}, func(frame ownerMuxFrame) {
		if matchingIdeaToolCall(frame, token) {
			sawCall = true
		}
	})
	if err != nil {
		return fmt.Errorf("tool turn: %w", err)
	}
	if !sawCall {
		return fmt.Errorf("completed reply had no matching jevons_idea_capture MCP call")
	}
	after, err := s.journeyIdeas(ctx)
	if err != nil {
		return err
	}
	id, err := uniqueIdeaEffect(after, token)
	if err != nil {
		return fmt.Errorf("tool effect API: %w", err)
	}
	if returnedID != id {
		return fmt.Errorf("tool reply identity %q differs from captured identity %q", returnedID, id)
	}
	if err := s.assertSeatPairCapture(overseerName, token, id); err != nil {
		return err
	}
	logs, err := os.ReadFile(s.logPath)
	if err != nil {
		return err
	}
	return queueJourneyProvider(logs, overseerName, string(s.provider))
}

func (s *suite) assertSeatPairCapture(seat, token, ideaID string) error {
	root := filepath.Join(s.stateDir, seatpair.DirName)
	intents, outcomes, err := seatpair.Inspect(root, seat)
	if err != nil {
		return fmt.Errorf("seat pair inspect: %w", err)
	}
	var matched []seatpair.Intent
	for _, intent := range intents {
		if intent.Tool == "jevons_idea_capture" && strings.Contains(intent.ArgsJSON, token) {
			matched = append(matched, intent)
		}
	}
	if len(matched) != 1 {
		return fmt.Errorf("seat %s pair intents for the capture = %d, want 1", seat, len(matched))
	}
	intent := matched[0]
	if intent.Generation <= 0 {
		return fmt.Errorf("capture intent generation = %d", intent.Generation)
	}
	res, ok := outcomes[intent.OperationID]
	if !ok {
		return fmt.Errorf("capture operation %s has no outcome", intent.OperationID)
	}
	if res.Outcome != seatpair.OutcomeAccepted || res.Reason != "ok" || !strings.Contains(res.Body, ideaID) {
		return fmt.Errorf("capture outcome = %+v, want accepted ok body containing %s", res, ideaID)
	}
	gen, have, err := seatpair.CurrentGeneration(root, seat)
	if err != nil {
		return err
	}
	if !have || gen != intent.Generation {
		return fmt.Errorf("seat generation = %d have=%v, intent generation %d", gen, have, intent.Generation)
	}
	return nil
}

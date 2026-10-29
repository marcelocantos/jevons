// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// jT1140OwnerReplyWire (🎯T1140): on an isolate, an owner question gets an
// unenveloped overseer answer on the owner mux, while a fleet-triggered
// routine acknowledgment stays off the owner wire.
func (s *suite) jT1140OwnerReplyWire() error {
	return s.withIsolatedBroker((*suite).t1140OwnerReplyWithBroker)
}

func (s *suite) t1140OwnerReplyWithBroker() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*turnTimeout)
	defer cancel()

	conn, frames, err := dialOwnerMux(ctx, s.host)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	if _, err := collectOwnerMuxReplay(ctx, frames); err != nil {
		return err
	}

	routineToken := "T1140-ROUTINE-" + uuid.NewString()
	answerToken := "T1140-ANSWER-" + uuid.NewString()

	leak := make(chan string, 1)
	readCtx, readCancel := context.WithCancel(ctx)
	go s.watchOwnerMuxAssistantSubstring(readCtx, frames, routineToken, leak)

	fleetPrompt := "A worker finish-report landed (no owner action needed). " +
		"Acknowledge for the fleet only — your reply must be exactly this one sentence, " +
		"with no jevons fence and no [silent]: " + routineToken
	if _, err := s.mcpText("jevons_agent_send", map[string]any{
		"name": overseerName, "actor": "jevons", "text": fleetPrompt,
	}); err != nil {
		readCancel()
		return fmt.Errorf("fleet notify to overseer: %w", err)
	}
	if err := s.waitAgentPhase(overseerName, func(p string) bool { return p == "" || p == "idle" }, 2*turnTimeout); err != nil {
		readCancel()
		return fmt.Errorf("overseer fleet chew: %w", err)
	}
	readCancel()
	select {
	case bad := <-leak:
		return fmt.Errorf("routine fleet reply leaked to owner mux: %s", trim(bad, 500))
	default:
	}

	ownerPrompt := "You are answering an owner message in main chat. " +
		"Reply with exactly " + answerToken + " and nothing else — no greeting, " +
		"no markdown, no ```jevons fence, no [silent]."
	if err := writeOwnerMux(ctx, conn, "send", map[string]string{"text": ownerPrompt}); err != nil {
		return err
	}
	if err := waitOwnerMuxReplyMatching(ctx, frames, ownerPrompt, answerToken, func(text string) bool {
		return strings.Contains(text, answerToken)
	}, nil); err != nil {
		return fmt.Errorf("owner-turn unenveloped answer: %w", err)
	}
	fmt.Printf("J47: fleet routine suppressed; owner question answered with %s\n", answerToken)
	return nil
}

func (s *suite) watchOwnerMuxAssistantSubstring(ctx context.Context, frames <-chan []byte, needle string, leak chan<- string) {
	for {
		select {
		case data, ok := <-frames:
			if !ok {
				return
			}
			env, frame, err := decodeOwnerMux(data)
			if err != nil || env.Channel != ownerMuxChannel || env.Type != "frame" {
				continue
			}
			if frame.Type != "assistant" {
				continue
			}
			text := journeyContentText(frame.Event.Message.Content)
			if strings.Contains(text, needle) {
				select {
				case leak <- text:
				default:
				}
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

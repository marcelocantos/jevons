// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/marcelocantos/jevons/scripts/journey-suite/portguard"
)

// t1120Quiet is how long after the first sealed reply we watch for a
// second assistant bubble. The 2026-10-21 specimen's duplicate landed
// about 30s later; 20s is enough to catch a racing re-offer or a fleet
// nudge answered in the owner thread without stretching the isolate.
const t1120Quiet = 20 * time.Second

// jT1120OneOwnerAnswer (🎯T1120): on an isolate — never :13705 — one
// owner send yields exactly one assistant bubble, and the mux phase the
// cockpit reduces returns to idle once that answer seals.
func (s *suite) jT1120OneOwnerAnswer() error {
	if err := portguard.RefuseDevelopment(s.port); err != nil {
		return err
	}
	if err := s.waitAgentPhase(overseerName, func(p string) bool { return p == "" || p == "idle" }, overseerIdleBefore); err != nil {
		return fmt.Errorf("overseer never settled before T1120: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), turnTimeout+t1120Quiet+30*time.Second)
	defer cancel()
	conn, frames, err := dialOwnerMux(ctx, s.host)
	if err != nil {
		return err
	}
	defer conn.CloseNow()
	if _, err := collectOwnerMuxReplay(ctx, frames); err != nil {
		return err
	}

	token := "T1120-ANSWER-" + uuid.NewString()
	prompt := "Reply with exactly: " + token
	if err := writeOwnerMux(ctx, conn, "send", map[string]string{"text": prompt}); err != nil {
		return err
	}
	if err := waitOwnerMuxOneReplyThenQuiet(ctx, frames, prompt, token, t1120Quiet); err != nil {
		return err
	}
	if err := s.waitAgentPhase(overseerName, func(p string) bool { return p == "" || p == "idle" }, 45*time.Second); err != nil {
		return fmt.Errorf("overseer phase after the answer: %w", err)
	}
	logs, err := os.ReadFile(s.logPath)
	if err != nil {
		return err
	}
	fmt.Printf("J52: one owner question produced one answer %s; mux phase idle through %s quiet\n", token, t1120Quiet)
	// The shared overseer's whole-log launch history includes the backends
	// earlier journeys moved it to; this journey's evidence is the backend it
	// runs on now, which the suite restores before each journey (🎯T1301).
	return latestLaunchProvider(logs, overseerName, s.provider)
}

// waitOwnerMuxOneReplyThenQuiet waits for this request's sealed reply,
// refuses a second non-empty assistant terminal after the owner echo
// (the 2026-10-21 duplicate-bubble specimen), and requires the mux phase
// to be idle by the end of quiet.
func waitOwnerMuxOneReplyThenQuiet(ctx context.Context, frames <-chan []byte, prompt, expected string, quiet time.Duration) error {
	ownerIndex := 0
	ended := map[string]bool{}
	bubbles := 0
	var first string
	idleSeen := false
	var quietC <-chan time.Time

	for {
		select {
		case data, ok := <-frames:
			if !ok {
				return fmt.Errorf("mux closed (owner index=%d bubbles=%d)", ownerIndex, bubbles)
			}
			env, frame, err := decodeOwnerMux(data)
			if err != nil {
				return err
			}
			if env.Channel != ownerMuxChannel {
				continue
			}
			if env.Type == "meta" {
				if p, ok := ownerMuxPhase(env.Body); ok {
					if p == "idle" || p == "" {
						idleSeen = true
					} else if bubbles >= 1 {
						idleSeen = false
					}
				}
				continue
			}
			if env.Type != "frame" {
				continue
			}
			text := journeyContentText(frame.Event.Message.Content)
			if frame.Type == "user" && (frame.Event.Origin == "owner" || frame.Event.Origin == "") && text == prompt {
				ownerIndex = frame.Index
			}
			if frame.Type != "assistant" || ownerIndex == 0 || frame.Index <= ownerIndex || ended[frame.ID] {
				continue
			}
			stop := frame.Event.Message.Stop
			if stop != "end_turn" && stop != "stop_sequence" && stop != "max_tokens" {
				continue
			}
			ended[frame.ID] = true
			if strings.TrimSpace(text) == "" {
				continue
			}
			if outage := replyOutage("owner mux reply", text); outage != nil {
				return outage
			}
			if stop == "max_tokens" {
				if strings.Contains(text, expected) {
					return fmt.Errorf("requested mux reply was truncated")
				}
				continue
			}
			bubbles++
			if bubbles == 1 {
				if !strings.Contains(text, expected) {
					return fmt.Errorf("first assistant bubble %q does not contain %s", trim(text, 200), expected)
				}
				first = text
				idleSeen = false // only idle AFTER this answer counts
				t := time.NewTimer(quiet)
				defer t.Stop()
				quietC = t.C
				continue
			}
			return fmt.Errorf("second assistant bubble after one owner send: first=%q second=%q", trim(first, 160), trim(text, 160))
		case <-quietC:
			if bubbles != 1 {
				return fmt.Errorf("quiet elapsed with %d assistant bubbles, want 1 (owner index=%d)", bubbles, ownerIndex)
			}
			if !idleSeen {
				return fmt.Errorf("mux phase never returned to idle after the answer")
			}
			return nil
		case <-ctx.Done():
			if bubbles == 0 {
				return fmt.Errorf("owner mux reply (owner index=%d): %w", ownerIndex, ctx.Err())
			}
			return fmt.Errorf("quiet watch cut short (bubbles=%d idle=%v): %w", bubbles, idleSeen, ctx.Err())
		}
	}
}

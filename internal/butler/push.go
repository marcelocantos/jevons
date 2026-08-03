// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package butler

import (
	"fmt"
	"strings"
)

// FormatEventPush builds the wire text pushed into an agent when an
// observed event fires (🎯T34). The event source is explicit so the
// agent can distinguish owner directs from automated triggers.
func FormatEventPush(source, text string) string {
	src := strings.TrimSpace(source)
	if src == "" {
		src = "unknown"
	}
	body := strings.TrimSpace(text)
	return fmt.Sprintf("[event: %s] %s", src, body)
}

// PushEvent delivers an event-driven message into a target participant
// (butler thread or fleet agent). Rehydrates a stopped process and never
// silently fails: undeliverable targets return a typed error.
//
// This is the generic butler mediation point: any event source (CI,
// dependency landed, worker finished, timer, mnemo note) calls
// PushEvent instead of hard-coding a delivery path (🎯T34 / 🎯T114).
// Uses Deliver so agent-only names (agents.json, no threads.json row)
// succeed instead of "no thread" (🎯T111.2).
func (b *Butler) PushEvent(targetID, source, text string) (string, error) {
	if strings.TrimSpace(targetID) == "" {
		return "", fmt.Errorf("push: target id is required")
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("push: text is required")
	}
	msg := FormatEventPush(source, text)
	reply, err := b.Deliver(targetID, msg)
	if err != nil {
		return "", fmt.Errorf("push %q (event %q): %w", targetID, strings.TrimSpace(source), err)
	}
	return reply, nil
}

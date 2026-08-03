// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package agenterr classifies agent send/delivery errors without assuming
// a single provider's wire strings (🎯T214 residual J6).
package agenterr

import (
	"strings"
)

// IsPromptBusy reports whether err means a concurrent prompt/turn is in
// progress and the caller should queue or interrupt — not treat the failure
// as a hard terminal send error.
//
// Matches provider-agnostic phrasing:
//   - Grok ACP: "grok acp: prompt already in flight"
//   - Claudia Task: "task <id> is busy"
//   - Common session wording: "prompt in progress", "session busy"
//
// Claude Session Send is fire-and-type (no ACP busy semaphore); when it
// returns a busy-shaped error it is still classified here. Residual: if
// Claude never returns busy, the queue path only engages on explicit errors.
func IsPromptBusy(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if msg == "" {
		return false
	}
	switch {
	case strings.Contains(msg, "already in flight"):
		return true
	case strings.Contains(msg, " is busy"):
		return true
	case strings.Contains(msg, "prompt in progress"):
		return true
	case strings.Contains(msg, "session busy"):
		return true
	case strings.Contains(msg, "turn in progress"):
		return true
	default:
		return false
	}
}

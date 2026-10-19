// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"

	"github.com/marcelocantos/claudia"
)

// Prompt causes a plan-only reply must not earn (🎯T869). Owner turns are
// not in this set: the owner can always speak.
const (
	promptRestartNudge = "restart-nudge"
	promptSentinel     = "sentinel"
	promptAgentForward = "agent-forward"
	promptImpatience   = "impatience"
)

// earnsAnotherPrompt reports whether a seat whose latest completed turn
// called toolCalls tools and said text should be given another daemon
// prompt of cause. Assistant prose and no tool call does not.
func earnsAnotherPrompt(toolCalls int, text, cause string) bool {
	if toolCalls > 0 || strings.TrimSpace(text) == "" {
		return true
	}
	switch cause {
	case promptRestartNudge, promptSentinel, promptAgentForward, promptImpatience:
		return false
	default:
		return true
	}
}

// shouldForwardAgentReply reports whether a completed turn is delivered
// to the parent as a fresh user turn. toolCalls < 0 means the caller does
// not know (the legacy notify path) and the text is forwarded. A sentence
// that opens with a future commitment is not a reason to drop the turn
// (🎯T766.4).
func shouldForwardAgentReply(text string, toolCalls int) bool {
	if toolCalls < 0 || toolCalls > 0 {
		return true
	}
	return !barePlanSentence(text)
}

// barePlanSentence reports a declared plan. A future-commitment sentence
// is not that declaration. There is no plan envelope kind, so no reply
// is a bare plan (🎯T766.4).
func barePlanSentence(string) bool {
	return false
}

// turnCountedTool reports one tool call in a seat event, in any backend's
// shape. A terminal prose event is not one.
func turnCountedTool(ev claudia.Event) bool {
	if ev.Type == "assistant" && ev.StopReason == "tool_use" {
		return true
	}
	return ev.ProgressType == claudia.ProgressToolUse ||
		strings.TrimSpace(ev.ToolCallID) != "" ||
		strings.TrimSpace(ev.ToolTitle) != ""
}

// withholdsPlanOnlyPrompt reports that name's latest turn was plan-only,
// so cause must not be submitted to it.
func (s *Server) withholdsPlanOnlyPrompt(name, cause string) bool {
	if s == nil || s.idleActivity == nil {
		return false
	}
	act := s.idleActivity.Get(name)
	if !act.PlanOnly {
		return false
	}
	return !earnsAnotherPrompt(act.LastToolCalls, act.LastTerminal, cause)
}

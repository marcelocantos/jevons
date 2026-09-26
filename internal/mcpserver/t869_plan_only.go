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
// to the parent as a fresh user turn. A bare plan ("I'll inspect…", no
// tool call) is not. A finish, a question, and an ordinary status report
// still are. toolCalls < 0 means the caller does not know (the legacy
// notify path) and the text is forwarded.
func shouldForwardAgentReply(text string, toolCalls int) bool {
	if toolCalls < 0 || toolCalls > 0 {
		return true
	}
	return !barePlanSentence(text)
}

// barePlanSentence is the 2026-09-26 shape: the reply opens with a future
// commitment and does not ask, finish, or report status.
func barePlanSentence(text string) bool {
	s := strings.TrimSpace(text)
	if s == "" || strings.Contains(s, "?") {
		return false
	}
	if LooksLikeFinishedWorkReport(s) || ReportAwaitsOverseer(s) {
		return false
	}
	lower := strings.ToLower(s)
	switch {
	case strings.HasPrefix(lower, "i'll "),
		strings.HasPrefix(lower, "i will "),
		strings.HasPrefix(lower, "i’ll "),
		strings.HasPrefix(lower, "i'm going to "),
		strings.HasPrefix(lower, "i am going to "):
		return true
	default:
		return false
	}
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

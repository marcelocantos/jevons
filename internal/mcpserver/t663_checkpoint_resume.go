// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/turndepth"
)

// 🎯T663 — a worker that ends its turn at the depth-ceiling checkpoint is
// resumed by the daemon.
//
// Two counters can ask a seat to checkpoint: the daemon's own
// (observeTurnDepth, which sets State.Requested and resumes through
// scheduleCheckpointResume) and the PreToolUse hook every seat working in
// this repo runs (scripts/hooks/turndepth). When the hook asks, the worker
// obeys and its terminal report says so — "depth-ceiling checkpoint",
// "ending this turn at the depth ceiling" — but the daemon counter never
// Requested, so nothing resumed it. On 2026-09-15 the eventlog carried 27
// such skipped reaps and zero checkpoint_resume decisions; the seats sat
// idle until a rehydrate gave them another eight calls.
//
// The terminal report is the evidence that survives both counters. When it
// carries the ceiling's own vocabulary and the daemon did not already ask,
// the daemon resumes the seat itself, marked source=hook.

// hookCheckpointMarkers are the ceiling-specific phrases a worker echoes
// when the hook asked it to stop. Deliberately narrower than
// checkpointMarkers: "ending this turn" alone is any checkpoint, and a plain
// checkpoint may be waiting on the overseer rather than on a resume.
var hookCheckpointMarkers = []string{
	"depth-ceiling checkpoint",
	"depth ceiling checkpoint",
	"t392.4 depth ceiling",
	"t392.4: depth ceiling",
	"at the depth ceiling",
	"hit the depth ceiling",
	"reached the depth ceiling",
	"turn-depth ceiling",
	"depth ceiling: this turn has made",
}

// IsDepthCeilingCheckpoint reports whether a terminal report says the turn
// ended because the depth ceiling asked it to.
func IsDepthCeilingCheckpoint(report string) bool {
	lower := strings.ToLower(report)
	for _, m := range hookCheckpointMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// HookResumePrompt is the successor turn's opener when the hook, not the
// daemon counter, asked for the checkpoint. The call count is unknown here.
func HookResumePrompt() string {
	return "🎯T392.4: your previous turn ended at the depth-ceiling checkpoint. " +
		"Continue from where you left off — the work is not finished until you report it so. " +
		"This is a new turn; the ceiling is reset. (🎯T663 resume, source=hook)"
}

// checkpointResumeSource names which counter asked for the checkpoint.
const (
	checkpointSourceDaemon = "daemon"
	checkpointSourceHook   = "hook"
)

// noteCheckpointResumePending records that name is between a checkpoint
// and its resume, so agent_list can say "checkpointed" rather than idle.
func (s *Server) noteCheckpointResumePending(name string) {
	if s == nil || name == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.checkpointResumePending == nil {
		s.checkpointResumePending = map[string]time.Time{}
	}
	s.checkpointResumePending[name] = time.Now()
}

// clearCheckpointResumePending forgets the pending resume: the seat's next
// turn began, or it left the fleet.
func (s *Server) clearCheckpointResumePending(name string) {
	if s == nil || name == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.checkpointResumePending, name)
}

// checkpointResumePendingFor reports whether name is awaiting its resume.
func (s *Server) checkpointResumePendingFor(name string) bool {
	if s == nil || name == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.checkpointResumePending[name]
	return ok
}

// resumeHookCheckpoint is the 🎯T663 path: the reap classifier kept name on a
// checkpoint-shaped report that names the depth ceiling, and the daemon's own
// counter did not ask for it. Resume the seat the way the daemon counter
// would have.
func (s *Server) resumeHookCheckpoint(name, report string) {
	if s == nil || name == "" {
		return
	}
	prompt := HookResumePrompt()
	s.noteCheckpointResumePending(name)
	s.logLifecycle(compTurnDepth, "checkpoint_resume", "ok", map[string]any{
		"agent": name, "source": checkpointSourceHook, "report_len": len(report),
	})
	slog.Info("🎯T663 resuming seat after a hook-driven depth-ceiling checkpoint",
		"agent", name)
	s.mu.Lock()
	fn := s.turnDepthResume
	s.mu.Unlock()
	if fn != nil {
		fn(name, prompt)
		return
	}
	go s.sendCheckpointResume(name, prompt)
}

// resumePromptForState is the daemon-counter prompt, kept here so both
// sources are read from one file.
func resumePromptForState(st turndepth.State) string {
	return turndepth.ResumePrompt(st)
}

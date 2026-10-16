// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"log/slog"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/ownerquestions"
	"github.com/marcelocantos/jevons/internal/ownerquestionview"
)

// DefaultOwnerQuestionSilence is the compiled silence window when config
// does not set owner_questions.silence_window (🎯T404.2).
const DefaultOwnerQuestionSilence = ownerquestionview.DefaultSilenceWindow

// SetOwnerQuestionSilenceWindow sets how long an unanswered question with a
// default waits before the fleet proceeds on that default. window <= 0
// disables auto-retire. Empty/zero at boot uses DefaultOwnerQuestionSilence.
func (s *Server) SetOwnerQuestionSilenceWindow(window time.Duration) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ownerQuestionSilence = window
}

func (s *Server) ownerQuestionSilenceWindow() time.Duration {
	if s == nil {
		return DefaultOwnerQuestionSilence
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ownerQuestionSilence == 0 {
		return DefaultOwnerQuestionSilence
	}
	if s.ownerQuestionSilence < 0 {
		return 0 // disabled
	}
	return s.ownerQuestionSilence
}

// SweepOwnerQuestionSilence retires open questions that carried a default
// past the silence window, tells each asker to proceed on it, and clears
// their notification outbox entries (🎯T404.2). Questions with no default
// stay open with their STALLS mark. Safe to call on every digest tick.
func (s *Server) SweepOwnerQuestionSilence(now time.Time) int {
	if s == nil || strings.TrimSpace(s.ownerQuestionsDir) == "" {
		return 0
	}
	window := s.ownerQuestionSilenceWindow()
	if window <= 0 {
		return 0
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	retired, err := ownerquestionview.New(s.ownerQuestionsDir).RetireOnSilence(now, window)
	if err != nil {
		slog.Warn("T404.2 silence retire failed", "err", err)
		return 0
	}
	if len(retired) == 0 {
		return 0
	}
	outbox := ownerquestions.New(s.ownerQuestionsDir)
	for _, q := range retired {
		note := q.Resolution
		if note == "" {
			note = ownerquestionview.ProceededOnDefaultNote
		}
		routed := s.routeQuestionOutcome(s.overseerName(), q, ownerquestionview.Moot, note)
		slog.Info("T404.2 silence retired question",
			"repo", q.Identity.Repo, "target", q.Identity.Target,
			"id", q.Identity.ID, "version", q.Identity.Version,
			"default", q.Default, "routed", routed)
		key := q.Identity.Repo + "#" + q.Identity.Target + "#" + q.Identity.ID
		if err := outbox.Resolve(key); err != nil {
			slog.Warn("T404.2 silence outbox clear failed", "key", key, "err", err)
		}
	}
	return len(retired)
}

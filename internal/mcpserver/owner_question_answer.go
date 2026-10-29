// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"strings"

	"github.com/marcelocantos/jevons/internal/ownerquestions"
	"github.com/marcelocantos/jevons/internal/ownerquestionview"
)

// AnswerOwnerQuestionIdentity records an owner answer and routes it to the
// asker (🎯T404 / 🎯T404.1 cockpit POST). The durable write happens before
// any seat is told the question closed.
func (s *Server) AnswerOwnerQuestionIdentity(id ownerquestionview.Identity, answer, actor string) (routed string, err error) {
	st, err := s.ownerQuestionStore()
	if err != nil {
		return "", err
	}
	asked, err := findOwnerQuestion(st, id)
	if err != nil {
		return "", err
	}
	id = asked.Identity
	outcomeNote := strings.TrimSpace(answer)
	if outcomeNote == "" {
		return "", fmt.Errorf("answer required")
	}
	if len(asked.Options) > 0 && !matchesQuestionOption(asked.Options, outcomeNote) {
		outcomeNote = outcomeNote + " (free-form; options were " + strings.Join(asked.Options, " | ") + ")"
	}
	if err := st.Resolve(id, ownerquestionview.Answered, outcomeNote); err != nil {
		return "", err
	}
	if err := s.confirmReleaseQuestion(asked, answer); err != nil {
		return "", fmt.Errorf("answer saved but release confirmation failed: %w", err)
	}
	if actor == "" {
		actor = s.overseerName()
	}
	routed = s.routeQuestionOutcome(actor, asked, ownerquestionview.Answered, outcomeNote)
	key := id.Repo + "#" + id.Target + "#" + id.ID
	remaining, err := st.List(true)
	if err != nil {
		return routed, fmt.Errorf("lifecycle saved but open-list failed: %w", err)
	}
	anyOpen := false
	for _, q := range remaining {
		if q.Identity.Repo == id.Repo && q.Identity.Target == id.Target && q.Identity.ID == id.ID {
			anyOpen = true
			break
		}
	}
	if !anyOpen {
		if err := ownerquestions.New(s.ownerQuestionsDir).Resolve(key); err != nil {
			return routed, fmt.Errorf("lifecycle saved but notification close failed: %w", err)
		}
	}
	return routed, nil
}

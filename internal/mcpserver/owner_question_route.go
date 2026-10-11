// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0
package mcpserver

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/marcelocantos/jevons/internal/ownerquestionview"
)

// An owner's answer routes back to the asker without the overseer relaying
// it by hand (🎯T404). 🎯T378 showed what a judgement-call relay does under
// load: the answer was obtained, the relay was planned, and a hundred no-op
// turns later nothing had reached the seat that asked. The relay is plumbing.

// agentNameShape is a registry-style seat name: one token, no spaces, not an
// MCP tool spelling. Asker and answer-route slots are free text and have
// carried "jevons-po / T1049 scout" and "jevons_owner_gate op=answer".
var agentNameShape = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]*$`)

// questionRouteCandidates lists, in order, the seats an outcome is offered
// to: the asker as written, then the asker's leading token when the slot
// carried a role suffix, then the answer route. Tool spellings and prose are
// skipped; a candidate that is not running is skipped at send time.
func questionRouteCandidates(q ownerquestionview.Question) []string {
	var out []string
	seen := map[string]bool{}
	add := func(raw string) {
		name := strings.TrimSpace(raw)
		if name == "" || strings.HasPrefix(name, "jevons_") || !agentNameShape.MatchString(name) || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	add(q.Asker)
	if first := strings.FieldsFunc(q.Asker, func(r rune) bool { return r == ' ' || r == '/' || r == ',' || r == '(' }); len(first) > 0 {
		add(first[0])
	}
	add(q.AnswerRoute)
	if first := strings.Fields(q.AnswerRoute); len(first) > 0 {
		add(first[0])
	}
	return out
}

// formatQuestionOutcome is the message the asking seat receives. It names
// the exact question version so a seat holding several knows which closed,
// and it restates the question so the seat need not look it up.
func formatQuestionOutcome(q ownerquestionview.Question, state ownerquestionview.State, note string) string {
	ref := fmt.Sprintf("%s/%s question %s@%s", filepath.Base(q.Identity.Repo), q.Identity.Target, q.Identity.ID, q.Identity.Version)
	switch state {
	case ownerquestionview.Answered:
		return fmt.Sprintf("Owner answer to your %s (%q): %s\nThis closes the question; proceed on the answer, not the default.", ref, q.Text, note)
	case ownerquestionview.Moot:
		return fmt.Sprintf("Your %s (%q) was retired as moot and needs no answer: %s\nIf you were waiting on it, proceed on your stated default.", ref, q.Text, note)
	default:
		return fmt.Sprintf("Your %s (%q) was marked %s: %s", ref, q.Text, state, note)
	}
}

// routeQuestionOutcome hands the outcome to the first candidate seat that
// accepts it and says where it went. A seat that is gone is not an error:
// the outcome is in the durable index, and the one-line result names the
// gap so the relayer can decide whether anyone else needs telling.
func (s *Server) routeQuestionOutcome(actor string, q ownerquestionview.Question, state ownerquestionview.State, note string) string {
	text := formatQuestionOutcome(q, state, note)
	candidates := questionRouteCandidates(q)
	if len(candidates) == 0 {
		return fmt.Sprintf("routed: nowhere — asker %q and answer route %q name no seat; outcome kept in the durable index", q.Asker, q.AnswerRoute)
	}
	var tried []string
	for _, name := range candidates {
		res, err := s.sendToAgentAs(actor, name, text, false)
		if err == nil {
			return fmt.Sprintf("routed: %s to %s (%s)", state, name, res.Status)
		}
		tried = append(tried, name+": "+err.Error())
	}
	return "routed: nowhere — " + strings.Join(tried, "; ") + "; outcome kept in the durable index"
}

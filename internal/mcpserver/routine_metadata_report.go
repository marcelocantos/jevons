// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"regexp"
	"strings"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/envelope"
)

var (
	routineTarget = regexp.MustCompile(`^(?:[A-Za-z][A-Za-z0-9_-]*/)?T[0-9]+(?:\.[0-9]+)*$`)
	routineSHA    = regexp.MustCompile(`^[a-fA-F0-9]{7,40}$`)
	routineGateID = regexp.MustCompile(`^[a-fA-F0-9]{8,64}$`)
)

// routineMetadataReport recognises only an entirely machine-readable worker
// completion sent to a non-overseer parent. It is deliberately NOT a sentiment
// or keyword classifier: any payload, decision, fog note, unknown slot, or
// malformed envelope is owner-visible, even if the worker claims GREEN.
// Parent delivery must independently succeed before this result is used.
func (s *Server) routineMetadataReport(agentName, text string) bool {
	if s.registry == nil || s.registryParent(agentName) == "" {
		return false
	}
	def := s.registry.Def(agentName)
	if def == nil || def.Purpose != claudia.PurposeWork {
		return false
	}
	// No daemon prefix or leading prose is permitted on the suppressible wire.
	if !strings.HasPrefix(text, "```jevons\n") {
		return false
	}
	m, err := envelope.Parse(text)
	if err != nil || m == nil || strings.TrimSpace(m.Payload) != "" || len(m.Extra) != 0 ||
		!routineTarget.MatchString(m.Target) || m.SilentLedger != envelope.SilentLedgerEmpty ||
		len(m.Decisions) != 0 || len(m.FogKnown)+len(m.FogUnknown)+len(m.FogBlindspot) != 0 ||
		len(m.GateRoles) != 0 || m.Blocker != "" || m.BlockClass != "" ||
		m.Question != "" || m.QuestionID != "" || m.QuestionRepo != "" ||
		m.QuestionVersion != "" || m.QuestionAsker != "" || m.AnswerRoute != "" ||
		m.Name != "" || m.Daily != "" || m.Risk.IsAccepted() {
		return false
	}
	if m.SHA != "" && !routineSHA.MatchString(m.SHA) || m.GateID != "" && !routineGateID.MatchString(m.GateID) {
		return false
	}
	switch m.Kind {
	case envelope.KindFinishReport:
		return m.Verdict == envelope.VerdictGreen && m.HasOracle() &&
			(m.Status == envelope.ProgressLanded || m.Status == envelope.ProgressShipped) && m.Phase != envelope.PhaseScout
	case envelope.KindScoutReport:
		return m.Phase == envelope.PhaseScout && m.Verdict == envelope.VerdictNone &&
			m.Status == envelope.ProgressNone && m.SHA == "" && m.GateID == ""
	default:
		return false
	}
}

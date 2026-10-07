// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import (
	"fmt"
	"strings"
)

// Validate checks required slots for the claimed kind. Unenveloped
// messages never reach here; callers fall back to prose.
func Validate(m *Message) error {
	if m == nil {
		return nil
	}
	if m.Kind == "" {
		return fmt.Errorf("missing %s kind", Sigil)
	}
	if _, ok := ParseKind(string(m.Kind)); !ok {
		return fmt.Errorf("unknown kind %q", m.Kind)
	}
	switch m.Kind {
	case KindFinishReport:
		if strings.TrimSpace(m.Target) == "" {
			return fmt.Errorf("finish-report requires target")
		}
		if m.Status == ProgressBlocked {
			// 🎯T938: a blocked report claims no completion, so it owes no
			// oracle — it owes the name of what it waits on.
			if strings.TrimSpace(m.Blocker) == "" {
				return fmt.Errorf("finish-report status blocked requires blocker (what the seat waits on)")
			}
		} else if !m.HasOracle() && !m.HasRisk() {
			return fmt.Errorf("finish-report requires oracle (sha or gate-id) or risk")
		}
		if m.BlockClass == BlockOwnerDecision {
			if m.Status != ProgressBlocked || strings.TrimSpace(m.QuestionRepo) == "" || strings.TrimSpace(m.QuestionID) == "" || strings.TrimSpace(m.QuestionVersion) == "" || strings.TrimSpace(m.Question) == "" || strings.TrimSpace(m.QuestionAsker) == "" || strings.TrimSpace(m.AnswerRoute) == "" {
				return fmt.Errorf("owner-decision block requires blocked status, question-repo, question-id, question-version, question, question-asker and answer-route")
			}
		} else if m.QuestionRepo != "" || m.QuestionID != "" || m.QuestionVersion != "" || m.Question != "" || m.QuestionAsker != "" || m.AnswerRoute != "" {
			return fmt.Errorf("owner question slots require block-class owner-decision")
		}
		// 🎯T536.1: silent-decision ledger is present or explicitly empty.
		if err := validateSilentLedger(m); err != nil {
			return err
		}
	case KindScoutReport:
		// 🎯T536.3: scout handoff needs target + ledger; no oracle required
		// (scout may have zero implementation commits).
		if strings.TrimSpace(m.Target) == "" {
			return fmt.Errorf("scout-report requires target")
		}
		if m.Phase != PhaseNone && !m.Phase.IsScout() {
			return fmt.Errorf("scout-report phase must be scout (or omitted)")
		}
		if err := validateSilentLedger(m); err != nil {
			return err
		}
	case KindSpawnBrief:
		if strings.TrimSpace(m.Target) == "" {
			return fmt.Errorf("spawn-brief requires target")
		}
		// Inherited pre-build ledger is optional; when present it must be
		// well-formed (🎯T536.3 implementer inherits scout decisions).
		if m.HasSilentLedger() {
			if err := validateSilentLedger(m); err != nil {
				return err
			}
		}
	case KindStatusPing:
		if m.Status == ProgressNone {
			return fmt.Errorf("status-ping requires status")
		}
	case KindEscalation:
		if strings.TrimSpace(m.Target) == "" {
			return fmt.Errorf("escalation requires target")
		}
	case KindTargetFileRequest:
		if strings.TrimSpace(m.Target) == "" && strings.TrimSpace(m.Name) == "" {
			return fmt.Errorf("target-file-request requires target or name")
		}
	case KindAck:
		// kind alone is enough
	}
	return nil
}

func validateSilentLedger(m *Message) error {
	if m == nil {
		return nil
	}
	if !m.HasSilentLedger() {
		return fmt.Errorf("requires silent-ledger (none|ranked)")
	}
	if m.SilentLedger == SilentLedgerRanked {
		if len(m.Decisions) == 0 {
			return fmt.Errorf("silent-ledger ranked requires at least one silent-decision")
		}
		if !decisionsLeastConfidentFirst(m.Decisions) {
			return fmt.Errorf("silent-decision list must be least-confident first")
		}
	}
	if m.SilentLedger == SilentLedgerEmpty && len(m.Decisions) > 0 {
		return fmt.Errorf("silent-ledger none must not carry silent-decision slots")
	}
	return nil
}

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
		if !m.HasOracle() && !m.HasRisk() {
			return fmt.Errorf("finish-report requires oracle (sha or gate-id) or risk")
		}
		// 🎯T536.1: silent-decision ledger is present or explicitly empty.
		if !m.HasSilentLedger() {
			return fmt.Errorf("finish-report requires silent-ledger (none|ranked)")
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
	case KindSpawnBrief:
		if strings.TrimSpace(m.Target) == "" {
			return fmt.Errorf("spawn-brief requires target")
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

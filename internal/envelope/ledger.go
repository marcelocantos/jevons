// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// SilentLedgerState is whether a finish-report carries the 🎯T536.1
// silent-decision ledger: absent (missing — flagged), empty (explicit
// "no silent decisions"), or ranked (least-confident first).
type SilentLedgerState int

const (
	// SilentLedgerAbsent: no silent-ledger slot. A claimed-done
	// finish-report in this state is incomplete under 🎯T536.1.
	SilentLedgerAbsent SilentLedgerState = iota
	// SilentLedgerEmpty: explicit none — the worker asserts the spec
	// was not silent on anything material.
	SilentLedgerEmpty
	// SilentLedgerRanked: one or more silent decisions, least-confident
	// first.
	SilentLedgerRanked
)

func (s SilentLedgerState) String() string {
	switch s {
	case SilentLedgerEmpty:
		return "none"
	case SilentLedgerRanked:
		return "ranked"
	default:
		return ""
	}
}

// SilentDecision is one choice made where the mission brief / acceptance
// was silent. Confidence is in [0,1]; lower means less confident.
type SilentDecision struct {
	Confidence float64
	Choice     string
	Why        string
}

// HasSilentLedger is true when the envelope carries an explicit empty
// marker or a ranked decision list (🎯T536.1).
func (m *Message) HasSilentLedger() bool {
	if m == nil {
		return false
	}
	return m.SilentLedger == SilentLedgerEmpty || m.SilentLedger == SilentLedgerRanked
}

// SilentDecisions returns the ranked ledger when present. Nil for empty
// or absent. The gate / auditor reads this field rather than the diff.
func (m *Message) SilentDecisions() []SilentDecision {
	if m == nil || m.SilentLedger != SilentLedgerRanked {
		return nil
	}
	out := make([]SilentDecision, len(m.Decisions))
	copy(out, m.Decisions)
	return out
}

// ReadSilentLedger is the gate helper: parse text and return the ledger
// state + decisions when an envelope is present. ok is false when there
// is no envelope (caller falls back to prose).
func ReadSilentLedger(text string) (state SilentLedgerState, decisions []SilentDecision, ok bool) {
	m, _ := Parse(text)
	if m == nil {
		return SilentLedgerAbsent, nil, false
	}
	return m.SilentLedger, m.SilentDecisions(), true
}

// MissingSilentLedger reports whether a finish-report or scout-report
// claims a load-bearing terminal without an explicit silent-ledger.
func MissingSilentLedger(m *Message) bool {
	if m == nil {
		return false
	}
	switch m.Kind {
	case KindFinishReport:
		if m.HasSilentLedger() {
			return false
		}
		// Claimed-done shape: target plus oracle/risk, or any finish-report
		// that already failed Validate for other reasons still needs the
		// ledger when it looks like a terminal claim.
		return m.HasOracle() || m.HasRisk() || strings.TrimSpace(m.Target) != ""
	case KindScoutReport:
		return !m.HasSilentLedger() && strings.TrimSpace(m.Target) != ""
	default:
		return false
	}
}

func parseSilentLedger(raw string) (SilentLedgerState, error) {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, "_", "-")
	switch s {
	case "none", "empty", "no", "no-silent-decisions", "no-silent-decision":
		return SilentLedgerEmpty, nil
	case "ranked", "present", "yes":
		return SilentLedgerRanked, nil
	default:
		return SilentLedgerAbsent, fmt.Errorf("unknown silent-ledger %q (want none|ranked)", raw)
	}
}

func parseSilentDecision(raw string) (SilentDecision, error) {
	d := SilentDecision{Confidence: -1}
	for _, tok := range splitDecisionTokens(raw) {
		k, v, cut := strings.Cut(tok, "=")
		if !cut {
			return d, fmt.Errorf("silent-decision token %q is not key=value", tok)
		}
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		switch k {
		case "confidence", "conf", "c":
			f, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return d, fmt.Errorf("silent-decision confidence %q: %w", v, err)
			}
			if f < 0 || f > 1 {
				return d, fmt.Errorf("silent-decision confidence %v out of [0,1]", f)
			}
			d.Confidence = f
		case "choice", "decision", "what":
			d.Choice = unquoteSlot(v)
		case "why", "reason", "because":
			d.Why = unquoteSlot(v)
		default:
			return d, fmt.Errorf("unknown silent-decision key %q", k)
		}
	}
	if d.Confidence < 0 {
		return d, fmt.Errorf("silent-decision requires confidence=")
	}
	if strings.TrimSpace(d.Choice) == "" {
		return d, fmt.Errorf("silent-decision requires choice=")
	}
	return d, nil
}

// splitDecisionTokens splits on spaces outside of double quotes so
// choice="optimistic concurrency" stays one token.
func splitDecisionTokens(raw string) []string {
	var out []string
	var b strings.Builder
	inQuote := false
	for _, r := range raw {
		switch {
		case r == '"':
			inQuote = !inQuote
			b.WriteRune(r)
		case unicode.IsSpace(r) && !inQuote:
			if b.Len() > 0 {
				out = append(out, b.String())
				b.Reset()
			}
		default:
			b.WriteRune(r)
		}
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

func unquoteSlot(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

func formatSilentDecision(d SilentDecision) string {
	var b strings.Builder
	b.WriteString("confidence=")
	b.WriteString(strconv.FormatFloat(d.Confidence, 'f', -1, 64))
	b.WriteString(" choice=")
	b.WriteString(quoteIfNeeded(d.Choice))
	if w := strings.TrimSpace(d.Why); w != "" {
		b.WriteString(" why=")
		b.WriteString(quoteIfNeeded(w))
	}
	return b.String()
}

func quoteIfNeeded(s string) string {
	if strings.ContainsAny(s, " \t\"'") {
		return strconv.Quote(s)
	}
	return s
}

func decisionsLeastConfidentFirst(ds []SilentDecision) bool {
	for i := 1; i < len(ds); i++ {
		if ds[i].Confidence < ds[i-1].Confidence {
			return false
		}
	}
	return true
}

func ledgerFingerprint(m *Message) string {
	if m == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("silent-ledger=")
	b.WriteString(m.SilentLedger.String())
	for _, d := range m.Decisions {
		b.WriteByte('|')
		b.WriteString(formatSilentDecision(d))
	}
	return b.String()
}

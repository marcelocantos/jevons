// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package poauditor detects when a product owner and its auditor seat are both
// idle while work children hold typed finish-reports on still-open targets
// (🎯T1192). The handoff after 🎯T1182 has no owner when both sides go idle.
package poauditor

import (
	"fmt"
	"strings"
)

// Handoff is one child finish-report the PO still owes integrate/achieve or an
// auditor gate on.
type Handoff struct {
	Agent  string
	Target string
}

// POObs is the product owner side of the mutual-wait read.
type POObs struct {
	Name         string
	Alive        bool
	Phase        string
	GraceElapsed bool
}

// AuditorObs is the PO's auditor child.
type AuditorObs struct {
	Name       string
	Alive      bool
	Phase      string
	QueueDepth int
}

// Verdict classifies one PO/auditor pair against pending handoffs.
type Verdict string

const (
	VerdictOK          Verdict = "ok"
	VerdictWithinGrace Verdict = "within_grace"
	VerdictNoHandoffs  Verdict = "no_handoffs"
	VerdictAuditorBusy Verdict = "auditor_busy"
	VerdictPOWorking   Verdict = "po_working"
	VerdictMutualWait  Verdict = "mutual_wait"
)

// IsFault reports whether the verdict must surface as a typed stall.
func IsFault(v Verdict) bool { return v == VerdictMutualWait }

// Classify names the stall from daemon-held observations only — no prose
// matching on report bodies.
func Classify(po POObs, aud AuditorObs, handoffs []Handoff) (Verdict, string) {
	if !po.Alive || !aud.Alive {
		return VerdictOK, ""
	}
	if !phaseIdle(po.Phase) {
		return VerdictPOWorking, ""
	}
	if !phaseIdle(aud.Phase) {
		return VerdictAuditorBusy, ""
	}
	if aud.QueueDepth > 0 {
		return VerdictAuditorBusy, ""
	}
	if len(handoffs) == 0 {
		return VerdictNoHandoffs, ""
	}
	if !po.GraceElapsed {
		return VerdictWithinGrace, ""
	}
	return VerdictMutualWait, formatDetail(po.Name, aud.Name, handoffs)
}

func phaseIdle(phase string) bool {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "", "idle":
		return true
	default:
		return false
	}
}

func formatDetail(po, auditor string, handoffs []Handoff) string {
	var b strings.Builder
	fmt.Fprintf(&b, "po=%s auditor=%s idle handoffs=%d", po, auditor, len(handoffs))
	for _, h := range handoffs {
		if strings.TrimSpace(h.Agent) == "" {
			continue
		}
		if t := strings.TrimSpace(h.Target); t != "" {
			fmt.Fprintf(&b, " %s:🎯%s", h.Agent, strings.TrimPrefix(t, "🎯"))
		} else {
			fmt.Fprintf(&b, " %s", h.Agent)
		}
	}
	return b.String()
}

// FormatHandoffList is the owner-facing enumeration for agent_list / wake text.
func FormatHandoffList(handoffs []Handoff) string {
	if len(handoffs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(handoffs))
	for _, h := range handoffs {
		agent := strings.TrimSpace(h.Agent)
		if agent == "" {
			continue
		}
		if t := strings.TrimSpace(h.Target); t != "" {
			parts = append(parts, fmt.Sprintf("%s (🎯%s)", agent, strings.TrimPrefix(t, "🎯")))
		} else {
			parts = append(parts, agent)
		}
	}
	return strings.Join(parts, ", ")
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package pochildwake decides when an idle product owner still owes action on
// a child's typed terminal report (🎯T1193). Generalizes 🎯T1192 to reaped
// children and does not require an auditor seat.
package pochildwake

import (
	"fmt"
	"strings"
	"time"
)

// Report is one child terminal report the PO has not yet acted on.
type Report struct {
	Agent    string
	Target   string
	Kind     string
	ReportID string
	StoredAt time.Time
}

// POObs is the product-owner side of the wake read.
type POObs struct {
	Name          string
	Alive         bool
	Phase         string
	GraceElapsed  bool
	LastTurnEnded time.Time
}

// Verdict classifies one PO against pending child reports.
type Verdict string

const (
	VerdictOK          Verdict = "ok"
	VerdictWithinGrace Verdict = "within_grace"
	VerdictPOWorking   Verdict = "po_working"
	VerdictNoReports   Verdict = "no_reports"
	VerdictWake        Verdict = "wake"
)

// IsFault reports whether the PO should be woken.
func IsFault(v Verdict) bool { return v == VerdictWake }

// Unacted returns reports stored after the PO's last turn ended and not yet
// covered by a prior wake offer for that report id.
func Unacted(po POObs, reports []Report, offered func(reportID string) bool) []Report {
	if offered == nil {
		offered = func(string) bool { return false }
	}
	var out []Report
	for _, r := range reports {
		if strings.TrimSpace(r.Agent) == "" || strings.TrimSpace(r.ReportID) == "" {
			continue
		}
		if !r.StoredAt.IsZero() && !po.LastTurnEnded.IsZero() && !r.StoredAt.After(po.LastTurnEnded) {
			continue
		}
		if offered(r.ReportID) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// Classify names whether the PO should be woken from daemon-held observations
// only — no prose matching on report bodies.
func Classify(po POObs, pending []Report) (Verdict, string) {
	if !po.Alive {
		return VerdictOK, ""
	}
	if !phaseIdle(po.Phase) {
		return VerdictPOWorking, ""
	}
	if len(pending) == 0 {
		return VerdictNoReports, ""
	}
	if !po.GraceElapsed {
		return VerdictWithinGrace, ""
	}
	return VerdictWake, formatDetail(po.Name, pending)
}

func phaseIdle(phase string) bool {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "", "idle":
		return true
	default:
		return false
	}
}

func formatDetail(po string, reports []Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "po=%s unacted_child_reports=%d", po, len(reports))
	for _, r := range reports {
		agent := strings.TrimSpace(r.Agent)
		if agent == "" {
			continue
		}
		kind := strings.TrimSpace(r.Kind)
		if kind == "" {
			kind = "terminal"
		}
		id := strings.TrimSpace(r.ReportID)
		if t := strings.TrimSpace(r.Target); t != "" {
			fmt.Fprintf(&b, " %s kind=%s target=🎯%s id=%s", agent, kind, strings.TrimPrefix(t, "🎯"), id)
		} else {
			fmt.Fprintf(&b, " %s kind=%s id=%s", agent, kind, id)
		}
	}
	return b.String()
}

// FormatWakeBody is the owner-facing enumeration for the PO wake message.
func FormatWakeBody(reports []Report) string {
	if len(reports) == 0 {
		return ""
	}
	lines := make([]string, 0, len(reports))
	for _, r := range reports {
		agent := strings.TrimSpace(r.Agent)
		if agent == "" {
			continue
		}
		kind := strings.TrimSpace(r.Kind)
		if kind == "" {
			kind = "terminal"
		}
		id := strings.TrimSpace(r.ReportID)
		if t := strings.TrimSpace(r.Target); t != "" {
			lines = append(lines, fmt.Sprintf("- agent=%s target=🎯%s kind=%s report_id=%s",
				agent, strings.TrimPrefix(t, "🎯"), kind, id))
		} else {
			lines = append(lines, fmt.Sprintf("- agent=%s kind=%s report_id=%s", agent, kind, id))
		}
	}
	return strings.Join(lines, "\n")
}

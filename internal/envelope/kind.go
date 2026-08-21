// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package envelope is the typed fleet-message schema (🎯T509).
//
// Load-bearing agent-to-agent claims travel as a fenced `jevons` block of
// machine-checkable slots wrapping a free-prose payload. The controlled
// vocabulary (kinds, GREEN/SUSPECT, in-progress vs live, class-3 / residual)
// lives here — instruction files reference this package rather than restating
// the enums. YAML front matter is not this format.
package envelope

import "strings"

// Kind is a recurring agent-to-agent message kind.
type Kind string

const (
	// KindSpawnBrief is a mission opening sent to a newly started (or
	// reminted) agent.
	KindSpawnBrief Kind = "spawn-brief"
	// KindFinishReport is a worker/PO terminal report. Required for work
	// agents claiming done (🎯T31).
	KindFinishReport Kind = "finish-report"
	// KindStatusPing is a mid-work progress note. Chatter-capped.
	KindStatusPing Kind = "status-ping"
	// KindEscalation is a blocked / needs-owner / anomaly raise.
	KindEscalation Kind = "escalation"
	// KindAck is a short acknowledgement. Chatter-capped.
	KindAck Kind = "ack"
	// KindTargetFileRequest asks the parent/overseer to file a bullseye
	// target (🎯T130 ceremony).
	KindTargetFileRequest Kind = "target-file-request"
)

// allKinds is the canonical list. Instruction files must not grow a parallel
// copy; AllKinds is the source of truth.
var allKinds = []Kind{
	KindSpawnBrief,
	KindFinishReport,
	KindStatusPing,
	KindEscalation,
	KindAck,
	KindTargetFileRequest,
}

// AllKinds returns every known kind in definition order.
func AllKinds() []Kind { return append([]Kind(nil), allKinds...) }

// ParseKind maps a slot value onto a Kind. Unknown names fail.
func ParseKind(raw string) (Kind, bool) {
	k := Kind(strings.ToLower(strings.TrimSpace(raw)))
	for _, known := range allKinds {
		if k == known {
			return k, true
		}
	}
	return "", false
}

func (k Kind) String() string { return string(k) }

// LoadBearing reports whether a claimed kind must carry its required slots
// rather than travelling as loose prose.
func (k Kind) LoadBearing() bool {
	switch k {
	case KindFinishReport, KindSpawnBrief, KindEscalation, KindTargetFileRequest:
		return true
	default:
		return false
	}
}

// ChatterCapped reports whether identical repeats of this kind are
// rate-capped and deduped (the Aug 2026 chatter-blowout class).
func (k Kind) ChatterCapped() bool {
	return k == KindStatusPing || k == KindAck
}

// Verdict is a gate/status verdict a message may claim. GREEN is the only
// pass; SUSPECT is a zero exit whose output contradicts it (🎯T386 / 🎯T396).
type Verdict string

const (
	VerdictNone    Verdict = ""
	VerdictGreen   Verdict = "GREEN"
	VerdictSuspect Verdict = "SUSPECT"
	VerdictRed     Verdict = "RED"
	VerdictUnknown Verdict = "UNKNOWN"
)

// ParseVerdict maps a slot value onto a Verdict. Unknown names fail.
func ParseVerdict(raw string) (Verdict, bool) {
	v := Verdict(strings.ToUpper(strings.TrimSpace(raw)))
	switch v {
	case VerdictGreen, VerdictSuspect, VerdictRed, VerdictUnknown:
		return v, true
	case VerdictNone:
		return VerdictNone, true
	default:
		return "", false
	}
}

func (v Verdict) String() string { return string(v) }

// IsPass is true only for GREEN — the sole verdict that may be cited as a
// pass (🎯T386).
func (v Verdict) IsPass() bool { return v == VerdictGreen }

// Progress is the 🎯T176 status-language vocabulary.
type Progress string

const (
	ProgressNone       Progress = ""
	ProgressInProgress Progress = "in-progress"
	ProgressLive       Progress = "live"
	ProgressLanded     Progress = "landed"
	ProgressShipped    Progress = "shipped"
)

// ParseProgress maps a slot value onto Progress. Spaces and underscores
// collapse to a hyphen so "in progress" matches the canonical form.
func ParseProgress(raw string) (Progress, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.Join(strings.Fields(s), "-")
	p := Progress(s)
	switch p {
	case ProgressInProgress, ProgressLive, ProgressLanded, ProgressShipped:
		return p, true
	case ProgressNone:
		return ProgressNone, true
	default:
		return "", false
	}
}

func (p Progress) String() string { return string(p) }

// ProductVisible is true for live/landed/shipped — words that require
// owner-visible product evidence (🎯T176).
func (p Progress) ProductVisible() bool {
	return p == ProgressLive || p == ProgressLanded || p == ProgressShipped
}

// Risk is the accepted-risk / residual vocabulary (🎯T31.1).
type Risk string

const (
	RiskNone     Risk = ""
	RiskNoneSlot Risk = "none"
	RiskClass3   Risk = "class-3"
	RiskResidual Risk = "residual"
	RiskAccepted Risk = "accepted-risk"
)

// ParseRisk maps a slot value onto Risk.
func ParseRisk(raw string) (Risk, bool) {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.Join(strings.Fields(s), "-")
	switch s {
	case "", "none":
		if strings.TrimSpace(raw) == "" {
			return RiskNone, true
		}
		return RiskNoneSlot, true
	case "class-3", "class3":
		return RiskClass3, true
	case "residual":
		return RiskResidual, true
	case "accepted-risk", "acceptedrisk":
		return RiskAccepted, true
	default:
		return "", false
	}
}

func (r Risk) String() string { return string(r) }

// IsAccepted is true when the slot is an explicit accepted-risk / class-3
// residual path, not an empty or "none" marker.
func (r Risk) IsAccepted() bool {
	return r == RiskClass3 || r == RiskResidual || r == RiskAccepted
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package handover carries an agent's working context across a provider
// switch (🎯T285).
//
// A session cannot move between backends: Grok's ACP session and Claude's
// JSONL transcript are different stores, and claudia fails closed rather
// than mint a replacement for a session id it cannot find. Switching an
// existing agent to another provider therefore always means a NEW
// conversation for it — the question is only what the successor is told.
//
// The successor is handed a bounded brief distilled from that file,
// not an instruction to walk it. Nothing is asked of the outgoing
// session — it may already be dead or out of tokens (🎯T285.1). A
// handover that depends on interviewing a corpse fails when it is
// needed most.
package handover

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// TranscriptFormat describes the predecessor's on-disk transcript so the
// successor knows what it is opening. Both are JSONL; the shapes differ.
func TranscriptFormat(fromProvider string) string {
	switch fromProvider {
	case "claude":
		return "Claude Code session JSONL — one JSON object per line, newest last"
	case "grok":
		return "Grok chat history JSONL — one JSON object per line, newest last"
	case "":
		return "JSONL — one JSON object per line, newest last"
	default:
		return fmt.Sprintf("%s session JSONL — one JSON object per line, newest last", fromProvider)
	}
}

// Rotation kinds. Only migrate is a provider switch. Compact/upgrade
// kinds remain on disk from earlier remints; they must not produce a
// migrate seed.
const (
	KindMigrate = "migrate"
	KindCompact = "compact"
	KindUpgrade = "upgrade"
)

// ProviderSwitch reports a real backend change. Same-provider pairs
// (including grok → grok) are never a migrate, even if a caller labelled
// them that way — that is what painted today's compact as a handover.
func ProviderSwitch(from, to string) bool {
	a := strings.ToLower(strings.TrimSpace(from))
	b := strings.ToLower(strings.TrimSpace(to))
	return a != "" && b != "" && a != b
}

// SeedMessage is the one-off prompt handed to a migrate successor.
// Same-provider arguments produce nothing: that is a restart, not a
// switch (🎯T40.2). Empty path also yields "" — the caller must refuse
// or cold-start deliberately.
func SeedMessage(fromProvider, toProvider, transcriptPath string) string {
	return ComposeSeed(Pending{
		From:           fromProvider,
		To:             toProvider,
		TranscriptPath: transcriptPath,
		Kind:           KindMigrate,
	})
}

// ComposeSeed is the shipped migrate seed. It is empty unless this is
// a real provider switch with a brief or a transcript to distill.
func ComposeSeed(p Pending) string {
	if !ProviderSwitch(p.From, p.To) {
		return ""
	}
	brief := strings.TrimSpace(p.Brief)
	if brief == "" {
		brief = Distill(p.TranscriptPath)
	}
	if brief == "" && strings.TrimSpace(p.TranscriptPath) == "" && strings.TrimSpace(p.Brief) == "" {
		return ""
	}
	if brief == "" {
		brief = "(no distillable turns — honour any in-flight work you can see, and do not reconstruct from the predecessor file.)"
	}
	return fmt.Sprintf(
		"[Provider switch — %s → %s. This is a new conversation on the new backend. "+
			"The previous session cannot be resumed there. The outgoing provider was not asked to summarise: it may be dead or out of tokens.]\n\n"+
			"What was in flight:\n\n%s\n\n"+
			"Continue from that. Do not read the predecessor transcript. Acknowledge the switch in ONE short sentence, then work.",
		providerLabel(p.From), providerLabel(p.To), brief)
}

// LooksLikeSeed reports a migrate seed so a successor's first turn is
// not mistaken for owner work.
func LooksLikeSeed(text string) bool {
	low := strings.ToLower(text)
	return strings.Contains(low, "[provider switch") ||
		strings.Contains(low, "[session handover") ||
		strings.Contains(low, "predecessor brief:") ||
		strings.Contains(low, "what was in flight:")
}

// AssignedReadAssignment is the retired T285 instruction, kept as a
// named mutation so tests can rebuild it against a fixture. Production
// seeds must not contain these phrases.
const AssignedReadAssignment = "Read it before doing anything else. Start at the END and work backwards until you have the current picture — it may be large, so do not read it whole."

func providerLabel(p string) string {
	if s := strings.TrimSpace(p); s != "" {
		return s
	}
	return "unknown"
}

// Pending is the record kept between rotating an agent onto a new session
// and seeding its successor.
//
// It exists because the rotation destroys the only pointer: the registry
// row's session id is overwritten with the new one, and with it the way
// to find the predecessor's transcript. Launch takes seconds and can
// fail, so the pointer is written to disk first — otherwise a daemon that
// dies mid-switch leaves an agent that cannot be told where it came from.
type Pending struct {
	Agent          string `json:"agent"`
	From           string `json:"from"`            // outgoing provider id
	To             string `json:"to"`              // incoming provider id
	Kind           string `json:"kind,omitempty"`  // migrate | compact | upgrade
	OldSessionID   string `json:"old_session_id"`  // predecessor's session
	TranscriptPath string `json:"transcript_path"` // resolved before rotation
	CreatedAt      string `json:"created_at"`      // RFC3339, informational
	// Brief is the text the work session receives (self-brief, Distill,
	// or throwaway-compact). Empty means ComposeSeed Distills the path.
	Brief string `json:"brief,omitempty"`
	// BriefSource is self-brief | distill | throwaway-compact.
	BriefSource string `json:"brief_source,omitempty"`
	// CompactSessionID is the throwaway new-provider session that read
	// the predecessor when Distill was too thin. The work session id
	// on the registry row must differ (🎯T285.1).
	CompactSessionID string `json:"compact_session_id,omitempty"`
	// Delivered records that the successor received its seed, so a
	// migration resumed after a crash does not seed it twice.
	Delivered bool `json:"delivered,omitempty"`

	// Identity snapshot (🎯T474). Written at rotate time so a bare-thread
	// Launch can rebuild the registry row when a concurrent reap (or any
	// other Remove) deleted it between prepare and launch. Without these
	// fields the MINT branch invents purpose=aside / empty workdir / a
	// fresh uuid and discards the rotation's prepared successor.
	Purpose      string `json:"purpose,omitempty"`
	WorkDir      string `json:"workdir,omitempty"`
	Parent       string `json:"parent,omitempty"`
	Model        string `json:"model,omitempty"`
	TargetID     string `json:"target_id,omitempty"`
	Goal         string `json:"goal,omitempty"`
	NewSessionID string `json:"new_session_id,omitempty"`
}

// EffectiveKind is compact whenever the providers do not differ.
func (p Pending) EffectiveKind() string {
	if !ProviderSwitch(p.From, p.To) {
		if p.Kind == KindUpgrade {
			return KindUpgrade
		}
		return KindCompact
	}
	if p.Kind == KindCompact || p.Kind == KindUpgrade {
		return p.Kind
	}
	return KindMigrate
}

// Usable reports whether this record can still seed a successor.
// Empty TranscriptPath is COLD (🎯T542): there is no predecessor to
// hand over, so the sweep must reap rather than surface the record.
func (p Pending) Usable() bool {
	return !p.Delivered && strings.TrimSpace(p.TranscriptPath) != ""
}

// Created is when this record was written, and whether that is knowable.
// A record with no parseable stamp is not an error — CreatedAt has always
// been informational — but it is not a time either, and the sweep that
// decides whether a record is stale must not read a zero clock as "just
// now" (🎯T418 clause 5).
func (p Pending) Created() (time.Time, bool) {
	stamp := strings.TrimSpace(p.CreatedAt)
	if stamp == "" {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// Age is how long this record has been waiting, and whether that is
// knowable. The age is the number a pending handover is visible in: the
// oldest four records found on disk on 2026-08-10 were 22 hours old, and
// nothing anywhere said so.
func (p Pending) Age(now time.Time) (time.Duration, bool) {
	created, ok := p.Created()
	if !ok {
		return 0, false
	}
	return now.Sub(created), true
}

// DescribeAge renders the age for an operator, saying plainly when the
// record does not carry one rather than printing a plausible zero.
func (p Pending) DescribeAge(now time.Time) string {
	age, ok := p.Age(now)
	if !ok {
		return "age unknown (no readable created_at)"
	}
	return age.Round(time.Second).String()
}

// Seed renders the prompt for this record.
func (p Pending) Seed() string { return ComposeSeed(p) }

// OwnerVisible is the one cockpit line for an actual provider switch.
func (p Pending) OwnerVisible() string {
	if !ProviderSwitch(p.From, p.To) {
		return ""
	}
	return fmt.Sprintf("Continuing on %s (migrated from %s).",
		providerLabel(p.To), providerLabel(p.From))
}

// Describe is the owner-facing one-liner for logs and chat: which agent
// moved, between which backends, and what the successor was pointed at.
// A migration that carried nothing says so plainly.
func (p Pending) Describe() string {
	if strings.TrimSpace(p.TranscriptPath) == "" {
		return fmt.Sprintf("%s: %s → %s, COLD (no predecessor transcript found)",
			p.Agent, providerLabel(p.From), providerLabel(p.To))
	}
	return fmt.Sprintf("%s: %s → %s, successor pointed at %s",
		p.Agent, providerLabel(p.From), providerLabel(p.To), filepath.Base(p.TranscriptPath))
}

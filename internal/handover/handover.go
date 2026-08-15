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
// The successor is handed a bounded brief distilled from that file
// (🎯T392.1.1), not an instruction to walk it. The path is cited for
// lookup. Nothing is asked of the outgoing session — by migration time
// it is often already dead, and a handover that depends on interviewing
// a corpse is a handover that fails when it is needed most.
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

// Rotation kinds. Compact and upgrade are same-provider remints;
// migrate is the only provider switch.
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

// SeedMessage is the one-off prompt handed to a migrate successor. Same-
// provider arguments are forced onto the compact seed: the provider-switch
// story must not fire unless the backends actually differ (🎯T392.1.1).
//
// It returns "" when there is no transcript to point at, which the caller
// must treat as "refuse or deliberately cold-start" rather than sending a
// handover that names nothing.
func SeedMessage(fromProvider, toProvider, transcriptPath string) string {
	return ComposeSeed(Pending{
		From:           fromProvider,
		To:             toProvider,
		TranscriptPath: transcriptPath,
		Kind:           KindMigrate,
	})
}

// ComposeSeed is the shipped seed-construction path (🎯T392.1.1).
func ComposeSeed(p Pending) string {
	path := strings.TrimSpace(p.TranscriptPath)
	if path == "" {
		return ""
	}
	kind := p.EffectiveKind()
	brief := Distill(path)
	cite := fmt.Sprintf(
		"Predecessor transcript (lookup only if this brief is insufficient; do not read or walk the file):\n  %s\n  (%s)",
		path, TranscriptFormat(p.From))
	body := brief
	if body == "" {
		body = "(no distillable turns — honour any in-flight work named below, and do not reconstruct from the file.)"
	}
	switch kind {
	case KindCompact, KindUpgrade:
		return compactSeed(body, cite)
	default:
		return migrateSeed(p.From, p.To, body, cite)
	}
}

func compactSeed(brief, cite string) string {
	return "[Context compact — same backend, new session. You were rotated because the previous conversation crossed the context ceiling. This is not a provider switch.]\n\n" +
		"Predecessor brief:\n\n" + brief + "\n\n" + cite + "\n\n" +
		"Pick up exactly where that left off: finish what was in flight, honour what it promised, and do not redo completed work.\n\n" +
		"Acknowledge in ONE short sentence, or say nothing. Do not narrate reconstruction."
}

func migrateSeed(from, to, brief, cite string) string {
	return fmt.Sprintf(
		"[Session handover — you have been restarted on a different agent backend (%s → %s). "+
			"You have no memory of the previous session, and it cannot be resumed: the two "+
			"backends keep separate session stores.]\n\n"+
			"Predecessor brief:\n\n%s\n\n%s\n\n"+
			"Pick up exactly where that left off: finish what was in flight, honour what it promised, and do not redo completed work.\n\n"+
			"Acknowledge in ONE short sentence naming what you are continuing. Do not narrate reconstruction.",
		providerLabel(from), providerLabel(to), brief, cite)
}

// LooksLikeSeed reports a user turn that is a rotation seed, so a
// successor whose only user turns are seeds has not grown through the
// ceiling (🎯T392.1.1).
func LooksLikeSeed(text string) bool {
	low := strings.ToLower(text)
	return strings.Contains(low, "[session handover") ||
		strings.Contains(low, "[context compact") ||
		strings.Contains(low, "predecessor brief:") ||
		strings.Contains(low, "predecessor's transcript is on disk")
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
	// Delivered records that the successor received its seed, so a
	// migration resumed after a crash does not seed it twice.
	Delivered bool `json:"delivered,omitempty"`
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

// OwnerVisible is what the cockpit may paint for this rotation.
// Compact is silent: reconstruction is not an owner-facing turn.
func (p Pending) OwnerVisible() string {
	if p.EffectiveKind() != KindMigrate {
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

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
// The successor is told where to look. Both providers leave a JSONL
// transcript on disk (discovery.TranscriptPath resolves either), and
// every agent has file tools, so the handover is one prompt naming the
// predecessor's transcript and asking it to pick up where that left off.
// Nothing is asked of the outgoing session — by migration time it is
// often already dead, and a handover that depends on interviewing a
// corpse is a handover that fails when it is needed most.
package handover

import (
	"fmt"
	"path/filepath"
	"strings"
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

// SeedMessage is the one-off prompt handed to the successor as its first
// input. It returns "" when there is no transcript to point at, which the
// caller must treat as "refuse or deliberately cold-start" rather than
// sending a handover that names nothing.
//
// The instruction reads from the END backwards on purpose: a long-lived
// agent's transcript can be megabytes, and what bears on the next action
// is at the tail. Telling it to "read the transcript" invites a
// first-line-first crawl that burns the context the handover exists to
// preserve.
func SeedMessage(fromProvider, toProvider, transcriptPath string) string {
	path := strings.TrimSpace(transcriptPath)
	if path == "" {
		return ""
	}
	return fmt.Sprintf(
		"[Session handover — you have been restarted on a different agent backend (%s → %s). "+
			"You have no memory of the previous session, and it cannot be resumed: the two "+
			"backends keep separate session stores.]\n\n"+
			"Your predecessor's transcript is on disk:\n\n"+
			"  %s\n  (%s)\n\n"+
			"Read it before doing anything else. Start at the END and work backwards until you "+
			"have the current picture — it may be large, so do not read it whole. Then pick up "+
			"exactly where your predecessor left off: finish what was in flight, honour what it "+
			"promised, and do not redo work it already completed.\n\n"+
			"Acknowledge in ONE short sentence naming what you are continuing.",
		providerLabel(fromProvider), providerLabel(toProvider), path, TranscriptFormat(fromProvider))
}

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
	OldSessionID   string `json:"old_session_id"`  // predecessor's session
	TranscriptPath string `json:"transcript_path"` // resolved before rotation
	CreatedAt      string `json:"created_at"`      // RFC3339, informational
	// Delivered records that the successor received its seed, so a
	// migration resumed after a crash does not seed it twice.
	Delivered bool `json:"delivered,omitempty"`
}

// Usable reports whether this record can still seed a successor.
func (p Pending) Usable() bool {
	return !p.Delivered && strings.TrimSpace(p.TranscriptPath) != ""
}

// Seed renders the prompt for this record.
func (p Pending) Seed() string { return SeedMessage(p.From, p.To, p.TranscriptPath) }

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

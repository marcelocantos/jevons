// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/marcelocantos/jevons/internal/rsi"
)

// 🎯T328: post-restart resume of unfinished owner instructions.
//
// Durable source: state_dir/chatlog/<overseer>.jsonl (survives control-plane
// bounce). On restart, when a recoverable open owner instruction is found,
// the overseer gets an owner-intent-resume event that forces a real turn with
// that instruction — not silent-idle-only / status-dump.
//
// Residual classes (no resume payload; daemon-restarted status path only):
//   - no_chatlog / no_user_turns / only_harness / ack_only / no_recoverable_intent
//   - session fully wiped (chatlog missing or empty after materialize loss)

const (
	// eventOwnerIntentResume is the post-restart resume event for open owner work.
	eventOwnerIntentResume = "owner-intent-resume"

	// MaxOpenIntentRunes caps how much of the owner instruction is re-injected.
	MaxOpenIntentRunes = 2400
	// DefaultOpenIntentLookback is how many recent chat turns to sample.
	DefaultOpenIntentLookback = 40

	// Residual classes (named for acceptance / logs).
	ResidualNoChatlog           = "no_chatlog"
	ResidualNoUserTurns         = "no_user_turns"
	ResidualOnlyHarness         = "only_harness"
	ResidualAckOnly             = "ack_only"
	ResidualNoRecoverableIntent = "no_recoverable_intent"
)

// OpenOwnerIntent is a recovered unfinished owner instruction, or a residual.
type OpenOwnerIntent struct {
	// Text is the open instruction when Residual is empty.
	Text string
	// Residual names why nothing recoverable was found (empty = recoverable).
	Residual string
	// Source is usually "owner_chat".
	Source string
	// TS is the observation time when known.
	TS time.Time
}

// Recoverable reports whether Text is a usable open owner instruction.
func (o OpenOwnerIntent) Recoverable() bool {
	return strings.TrimSpace(o.Residual) == "" && strings.TrimSpace(o.Text) != ""
}

// OwnerIntentTurn is a pure user-chat turn input for extraction (test-friendly).
type OwnerIntentTurn struct {
	Text   string
	Source string
	TS     time.Time
}

// ExtractOpenOwnerIntent picks the most recent substantive owner instruction
// from turns ordered oldest→newest. Skips harness injects, pure restart
// re-nudges, and short acks. Hermetic oracle surface for 🎯T328.
func ExtractOpenOwnerIntent(turns []OwnerIntentTurn) OpenOwnerIntent {
	if len(turns) == 0 {
		return OpenOwnerIntent{Residual: ResidualNoUserTurns}
	}

	var substantive []OwnerIntentTurn
	var sawHarnessOnly bool
	var sawAckOnly bool
	for _, t := range turns {
		text := strings.TrimSpace(t.Text)
		if text == "" {
			continue
		}
		if isOpenIntentHarness(text) {
			sawHarnessOnly = true
			continue
		}
		if isOpenIntentRestartNudge(text) {
			// Owner re-prompt after bounce is not the open work itself.
			continue
		}
		if isOpenIntentAckOnly(text) {
			sawAckOnly = true
			continue
		}
		substantive = append(substantive, OwnerIntentTurn{
			Text:   text,
			Source: t.Source,
			TS:     t.TS,
		})
	}
	if len(substantive) == 0 {
		switch {
		case sawAckOnly:
			return OpenOwnerIntent{Residual: ResidualAckOnly}
		case sawHarnessOnly:
			return OpenOwnerIntent{Residual: ResidualOnlyHarness}
		default:
			return OpenOwnerIntent{Residual: ResidualNoRecoverableIntent}
		}
	}
	// Newest substantive instruction is the open work to resume.
	last := substantive[len(substantive)-1]
	src := strings.TrimSpace(last.Source)
	if src == "" {
		src = "owner_chat"
	}
	return OpenOwnerIntent{
		Text:   truncateRunes(last.Text, MaxOpenIntentRunes),
		Source: src,
		TS:     last.TS,
	}
}

// ChatTurnsToOwnerIntentTurns adapts rsi chat turns for extraction.
func ChatTurnsToOwnerIntentTurns(turns []rsi.ChatTurn) []OwnerIntentTurn {
	out := make([]OwnerIntentTurn, 0, len(turns))
	for _, t := range turns {
		role := strings.ToLower(strings.TrimSpace(t.Role))
		if role != "user" {
			continue
		}
		text := strings.TrimSpace(t.Text)
		if text == "" {
			continue
		}
		out = append(out, OwnerIntentTurn{
			Text:   text,
			Source: t.Source,
			TS:     t.TS,
		})
	}
	return out
}

// LoadOpenOwnerIntent reads state_dir/chatlog/<overseer>.jsonl and extracts
// recoverable open owner intent. Missing chatlog → residual no_chatlog.
func LoadOpenOwnerIntent(stateDir, overseer string) OpenOwnerIntent {
	stateDir = strings.TrimSpace(stateDir)
	overseer = strings.TrimSpace(overseer)
	if overseer == "" {
		overseer = "jevons"
	}
	if stateDir == "" {
		return OpenOwnerIntent{Residual: ResidualNoChatlog}
	}
	path := filepath.Join(stateDir, "chatlog", overseer+".jsonl")
	turns, err := rsi.LoadChatLogTurns(path, DefaultOpenIntentLookback)
	if err != nil || len(turns) == 0 {
		// Distinguish missing/empty file from parse noise: LoadChatLogTurns
		// returns empty,nil for missing — treat both as no recoverable intent
		// with no_chatlog when the path cannot yield user turns.
		if err != nil {
			return OpenOwnerIntent{Residual: ResidualNoChatlog}
		}
		// File may exist but only assistant/stream lines — still no user turns.
		return OpenOwnerIntent{Residual: ResidualNoUserTurns}
	}
	return ExtractOpenOwnerIntent(ChatTurnsToOwnerIntentTurns(turns))
}

// FormatOverseerOpenIntentResume builds the fire-and-forget body that forces
// continuation of open owner work after a control-plane bounce.
//
// Critical: this is NOT silent-idle-only. [silent] is forbidden for a pure
// status dump when an open instruction is still unfinished.
func FormatOverseerOpenIntentResume(intent OpenOwnerIntent, parent string, workers []WorkerIdleRef) string {
	var b strings.Builder
	b.WriteString("OPEN OWNER INSTRUCTION — resume after jevonsd restart (🎯T328).\n")
	b.WriteString("Control plane is back. You had unfinished owner work when the service bounced.\n\n")
	b.WriteString("MANDATORY: Continue the open instruction below NOW. Do not wait for the owner to re-nudge.\n")
	b.WriteString("Do NOT reply with only [silent] sleep / fleet status / \"everything is fine\".\n")
	b.WriteString("A pure status-dump without acting on the open instruction is a product failure.\n")
	b.WriteString("Act: file/spawn/edit/brief/document as the instruction requires. Local master only (T104).\n\n")
	if parent != "" {
		fmt.Fprintf(&b, "Addressed to: %s\n\n", parent)
	}
	b.WriteString("Open instruction (recovered from durable owner chatlog):\n")
	b.WriteString("----------\n")
	b.WriteString(strings.TrimSpace(intent.Text))
	b.WriteString("\n----------\n\n")
	if !intent.TS.IsZero() {
		fmt.Fprintf(&b, "Observed: %s\n", intent.TS.UTC().Format(time.RFC3339))
	}
	if intent.Source != "" {
		fmt.Fprintf(&b, "Source: %s\n", intent.Source)
	}
	b.WriteString("\nReattached fleet (context only — resume the open instruction first):\n")
	if len(workers) == 0 {
		b.WriteString("  (no running work children listed)\n")
	} else {
		for _, w := range workers {
			tid := strings.TrimSpace(strings.TrimPrefix(w.TargetID, "🎯"))
			st := strings.TrimSpace(w.Status)
			if st == "" {
				st = "running"
			}
			ph := strings.TrimSpace(w.Phase)
			if ph == "" {
				ph = "idle"
			}
			if tid != "" {
				fmt.Fprintf(&b, "  - %s | 🎯%s | status=%s phase=%s\n", w.Name, tid, st, ph)
			} else {
				fmt.Fprintf(&b, "  - %s | status=%s phase=%s\n", w.Name, st, ph)
			}
		}
	}
	b.WriteString("\nDual path (🎯T171): open-mission workers also get short resume separately.\n")
	b.WriteString("When the open instruction is fully done, report evidence; otherwise keep working.\n")
	return b.String()
}

// isOpenIntentHarness matches harness/system injects that are not owner work.
func isOpenIntentHarness(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return true
	}
	if strings.HasPrefix(t, "[Daemon restart") ||
		strings.HasPrefix(t, "[Jevons fleet standing brief") ||
		strings.HasPrefix(t, "[event:") {
		return true
	}
	if strings.Contains(t, "<system-reminder>") || strings.Contains(t, "system-reminder") {
		return true
	}
	if strings.HasPrefix(t, "[Agent ") && strings.Contains(t, "responded]") {
		return true
	}
	return false
}

// isOpenIntentRestartNudge skips owner messages that are pure bounce re-prompts
// or meta-complaints about agents sitting idle after restart — not the work itself.
func isOpenIntentRestartNudge(text string) bool {
	low := strings.ToLower(strings.TrimSpace(text))
	if low == "" {
		return false
	}
	// Exact-ish re-nudges.
	switch low {
	case "continue", "keep going", "resume", "go on", "carry on", "please continue":
		return true
	}
	// "service restarted. Continue" and close cousins.
	if strings.Contains(low, "service restarted") ||
		strings.Contains(low, "daemon restarted") ||
		strings.Contains(low, "after the restart") ||
		strings.Contains(low, "after restart") {
		return true
	}
	// Meta gap reports (the T328 incident itself).
	if strings.Contains(low, "gap in the restart") ||
		strings.Contains(low, "until being told to keep going") ||
		strings.Contains(low, "sit there until") ||
		(strings.Contains(low, "waited") && strings.Contains(low, "restart")) {
		return true
	}
	return false
}

// isOpenIntentAckOnly filters short acks / pure praise without a work order.
func isOpenIntentAckOnly(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return true
	}
	// Long enough to carry a real instruction — keep.
	if utf8.RuneCountInString(t) >= 48 {
		return false
	}
	low := strings.ToLower(t)
	// Imperative / work markers even in short messages.
	workHints := []string{
		"fix ", "file ", "spawn", "shove", "write ", "update ", "implement",
		"document", "add ", "remove ", "delete ", "restart", "deploy", "merge ",
		"commit", "test ", "please ", "need you", "do this", "make sure",
		"🎯", "target:",
	}
	for _, h := range workHints {
		if strings.Contains(low, h) {
			return false
		}
	}
	// Short praise / ack without work order.
	acks := []string{
		"ok", "okay", "thanks", "thank you", "ty", "cool", "great", "nice",
		"perfect", "good", "lgtm", "👍", "got it", "sounds good", "brilliant",
		"yes", "yep", "nope", "no", "k",
	}
	for _, a := range acks {
		if low == a || strings.HasPrefix(low, a+" ") || strings.HasPrefix(low, a+"!") || strings.HasPrefix(low, a+".") {
			return true
		}
	}
	// Very short without work markers.
	return utf8.RuneCountInString(t) < 20
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}

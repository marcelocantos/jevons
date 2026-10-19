// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/marcelocantos/jevons/internal/fleet"
	"github.com/marcelocantos/jevons/internal/ownerqa"
	"github.com/marcelocantos/jevons/internal/rsi"
	"github.com/marcelocantos/jevons/internal/statedb"
	"github.com/marcelocantos/jevons/internal/targetfile"
)

// 🎯T328: post-restart resume of unfinished owner instructions.
// 🎯T344: do not re-fire owner turns already answered with product evidence.
// 🎯T477: an owner question is not closed by an explanation sentence.
// A declared oracle or accepted-risk slot on the same topic is.
// 🎯T512: an owner fleet directive (kill/stop/park/reap a named seat) is closed
// by a structured fleet-op excerpt for that seat. A killed/deregistered
// sentence is not that excerpt (🎯T766.4).
// 🎯T528: a Goal / open-objective naming TargetIDs is closed when every named
// id is achieved (or set_aside) in the ledger — Session Goal Continue must
// not re-inject "Continue the open objective" for that text.
// 🎯T551: a declared send-probe fact (open-intent send-probe, or that
// fact plus one token) is never recoverable open owner work. A slug or
// a sentence that mentions a probe is owner text (🎯T766.4).
// 🎯T568: a later sentence is not a close (🎯T766.4). An overseer reply
// that names a TargetID achieved or set_aside in the ledger still closes,
// even when the owner text named none. A bare ack does not close. The
// live miss was the 2026-08-26 "Cursor monthly cycle?" question, landed
// as 🎯T550, then re-issued because the owner text named no id.
//
// Durable source: state_dir/chatlog/<overseer>.jsonl (survives control-plane
// bounce). On restart, when a recoverable open owner instruction is found,
// the overseer gets an owner-intent-resume event that forces a real turn with
// that instruction — not silent-idle-only / status-dump.
//
// Residual classes (no resume payload; daemon-restarted status path only):
//   - no_chatlog / no_user_turns / only_harness / ack_only / no_recoverable_intent
//   - answered_or_closed (T344/T477/T512/T528/T568: declared evidence,
//     fleet ops, or a ledger-closed target id)
//   - session fully wiped (chatlog missing or empty after materialize loss)

const (
	// eventOwnerIntentResume is the post-restart resume event for open owner work.
	eventOwnerIntentResume = "owner-intent-resume"

	// MaxOpenIntentRunes caps how much of the owner instruction is re-injected.
	MaxOpenIntentRunes = 2400
	// DefaultOpenIntentLookback is how many recent chat turns to sample.
	DefaultOpenIntentLookback = 40
	// DefaultOpenIntentAssistantCap bounds evidence-shaped assistant excerpts
	// kept after the oldest retained user turn (chatlogs are assistant-heavy).
	DefaultOpenIntentAssistantCap = 80

	// Residual classes (named for acceptance / logs).
	ResidualNoChatlog           = "no_chatlog"
	ResidualUnreadableChatlog   = "unreadable_chatlog"
	ResidualNoUserTurns         = "no_user_turns"
	ResidualOnlyHarness         = "only_harness"
	ResidualAckOnly             = "ack_only"
	ResidualNoRecoverableIntent = "no_recoverable_intent"
	// ResidualAnsweredOrClosed: newest substantive owner turn already has a
	// later overseer/assistant answer with product evidence (🎯T344).
	ResidualAnsweredOrClosed = "answered_or_closed"
	// ResidualNotSubstantive: newest owner-side turn is a marked send/delivery
	// probe, not real work (🎯T551).
	ResidualNotSubstantive = "not_substantive"
	// ResidualStaleChatlog: the chatlog is still being written — progress
	// frames keep arriving — but its newest turn is far older than its
	// newest frame, so the store has stopped recording turns and the
	// "newest owner instruction" it can offer is not the newest one
	// (🎯T592).
	ResidualStaleChatlog = "stale_chatlog"
	// Restart recovery needs evidence that this instruction predates boot.
	// These do not suppress ordinary owner delivery or non-restart recovery.
	ResidualPostBootIntent         = "post_boot_intent"
	ResidualUnknownRestartBoundary = "unknown_restart_boundary"
)

// OpenIntentStaleWindow is how far the newest journaled turn may lag the
// newest frame in the same chatlog before the store is degraded. A live
// conversation journals a turn far more often than hourly; an hour of
// frames with no turn behind them is the 🎯T592 defect, not a quiet day.
//
// The live shape: on 2026-08-31 state_dir/chatlog/jevons.jsonl held
// 5,343 progress frames written after its last type=user record of
// 2026-08-26T08:44. Sourcing a MANDATORY resume from that file re-issued
// a five-day-old Cursor-cycle instruction after every daemon bounce. A
// degraded store yields a residual instead: no resume is better than a
// confidently wrong one.
const OpenIntentStaleWindow = time.Hour

// ChatlogDegraded reports whether a chatlog whose newest turn is
// newestTurn and whose newest frame of any kind is newestFrame has
// stopped recording turns (🎯T592). Zero times are unknown, never
// degraded — an empty or turn-only store is handled by the other
// residual classes.
func ChatlogDegraded(newestTurn, newestFrame time.Time) bool {
	if newestTurn.IsZero() || newestFrame.IsZero() {
		return false
	}
	return newestFrame.Sub(newestTurn) > OpenIntentStaleWindow
}

// openIntentTargetIDRe matches 🎯T12 / T12 / T12.3 style target ids.
var openIntentTargetIDRe = regexp.MustCompile(`(?i)(?:🎯)?T\d+(?:\.\d+)*`)

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

// OwnerIntentTurn is a pure chat-turn input for extraction (test-friendly).
// Role is "user" (default when empty) or "assistant". Assistant turns are
// used only for 🎯T344 answered/closed disposition after a user instruction.
type OwnerIntentTurn struct {
	Text   string
	Source string
	TS     time.Time
	// Role is user or assistant; empty means user (T328 back-compat).
	Role string
}

// ExtractOpenOwnerIntent picks the most recent substantive owner instruction
// from turns ordered oldest→newest. Skips harness injects, pure restart
// re-nudges, and short acks. When a later assistant turn reports product
// evidence for the same complaint, yields residual answered_or_closed (🎯T344);
// explanation answers do not close questions (🎯T477); a structured fleet-op
// excerpt closes kill/stop/park/reap directives (🎯T512). Hermetic oracle surface for
// 🎯T328 + 🎯T344 + 🎯T477 + 🎯T512. Ledger Goal close is
// ExtractOpenOwnerIntentWithLedger (🎯T528).
func ExtractOpenOwnerIntent(turns []OwnerIntentTurn) OpenOwnerIntent {
	return ExtractOpenOwnerIntentWithLedger(turns, nil)
}

// ExtractOpenOwnerIntentWithLedger is ExtractOpenOwnerIntent plus 🎯T528
// (owner-named TargetIDs all achieved/set_aside) and 🎯T568 (later
// substantive overseer reply, or a later reply that names a ledger-closed
// TargetID — even when the owner text named none).
func ExtractOpenOwnerIntentWithLedger(turns []OwnerIntentTurn, statusByID map[string]string) OpenOwnerIntent {
	if len(turns) == 0 {
		return OpenOwnerIntent{Residual: ResidualNoUserTurns}
	}

	// Timeline of kept user + assistant turns (indices into this slice).
	type kept struct {
		turn OwnerIntentTurn
		user bool
	}
	var timeline []kept
	var sawHarnessOnly bool
	var sawProbeOnly bool
	var sawAckOnly bool
	var sawAnyUser bool

	for _, t := range turns {
		text := strings.TrimSpace(t.Text)
		if text == "" {
			continue
		}
		role := strings.ToLower(strings.TrimSpace(t.Role))
		if role == "" {
			role = "user"
		}
		if role == "assistant" {
			timeline = append(timeline, kept{
				turn: OwnerIntentTurn{Text: text, Source: t.Source, TS: t.TS, Role: "assistant"},
				user: false,
			})
			continue
		}
		// user / other treated as owner-side
		sawAnyUser = true
		if isOpenIntentHarness(text) {
			sawHarnessOnly = true
			continue
		}
		if isOpenIntentSendProbe(text) {
			sawProbeOnly = true
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
		timeline = append(timeline, kept{
			turn: OwnerIntentTurn{Text: text, Source: t.Source, TS: t.TS, Role: "user"},
			user: true,
		})
	}
	if !sawAnyUser && len(timeline) == 0 {
		return OpenOwnerIntent{Residual: ResidualNoUserTurns}
	}

	// Newest substantive user instruction.
	lastUserIdx := -1
	for i := len(timeline) - 1; i >= 0; i-- {
		if timeline[i].user {
			lastUserIdx = i
			break
		}
	}
	if lastUserIdx < 0 {
		switch {
		case sawAckOnly:
			return OpenOwnerIntent{Residual: ResidualAckOnly}
		case sawProbeOnly:
			return OpenOwnerIntent{Residual: ResidualNotSubstantive}
		case sawHarnessOnly:
			return OpenOwnerIntent{Residual: ResidualOnlyHarness}
		default:
			return OpenOwnerIntent{Residual: ResidualNoRecoverableIntent}
		}
	}
	last := timeline[lastUserIdx].turn

	// 🎯T528: Goal / open-objective naming TargetIDs closed when ledger
	// shows every named id achieved (or set_aside).
	if fleet.GoalMissionEvidencedComplete(last.Text, statusByID) {
		return OpenOwnerIntent{Residual: ResidualAnsweredOrClosed}
	}

	// 🎯T344 / T477 / T512 / T568: later assistant turns after this instruction.
	// When both timestamps are known, require the reply to be strictly after
	// the instruction's observed time (🎯T568).
	var laterAnswers []string
	for i := lastUserIdx + 1; i < len(timeline); i++ {
		if timeline[i].user {
			continue
		}
		a := timeline[i].turn
		if !last.TS.IsZero() && !a.TS.IsZero() && !a.TS.After(last.TS) {
			continue
		}
		laterAnswers = append(laterAnswers, a.Text)
	}
	// 🎯T528: GOAL_STATUS: complete with matching achieved/product evidence.
	if ownerGoalStatusCompleteWithEvidence(last.Text, laterAnswers) {
		return OpenOwnerIntent{Residual: ResidualAnsweredOrClosed}
	}
	if OwnerIntentAnsweredWithProductEvidence(last.Text, laterAnswers) {
		return OpenOwnerIntent{Residual: ResidualAnsweredOrClosed}
	}

	// 🎯T477: explanation prose is not an answer. The helper returns false
	// until a declared slot exists. Fleet kill/stop/park/reap keep T512.
	if OwnerQuestionAnsweredWithExplanation(last.Text, laterAnswers) {
		return OpenOwnerIntent{Residual: ResidualAnsweredOrClosed}
	}

	// 🎯T512: kill/stop/park/reap fleet directives close on a structured
	// fleet-op excerpt for the named seat. A sentence is not that excerpt.
	if OwnerFleetDirectiveCompleted(last.Text, laterAnswers) {
		return OpenOwnerIntent{Residual: ResidualAnsweredOrClosed}
	}

	// 🎯T568: a later sentence does not close. The callee returns false.
	// A reply that names a ledger-closed TargetID still closes when the
	// owner instruction named none (Cursor-monthly / 🎯T550).
	// Fleet kill/stop/park/reap keep the T512 structured same-seat contract.
	if len(ownerFleetDirectiveTargets(last.Text)) == 0 &&
		ownerIntentClosedByLaterSubstantiveReply(laterAnswers) {
		return OpenOwnerIntent{Residual: ResidualAnsweredOrClosed}
	}
	if ownerIntentReplyNamesClosedTarget(laterAnswers, statusByID) {
		return OpenOwnerIntent{Residual: ResidualAnsweredOrClosed}
	}

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

// ownerGoalStatusCompleteWithEvidence reports whether a later assistant
// excerpt closes a Goal/open-objective with an exact GOAL_STATUS: complete
// line plus matching product evidence for the same topic (🎯T528).
func ownerGoalStatusCompleteWithEvidence(ownerText string, laterAssistant []string) bool {
	ownerText = strings.TrimSpace(ownerText)
	if ownerText == "" {
		return false
	}
	for _, a := range laterAssistant {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if _, ok := parseAssistantGoalStatus(a); !ok {
			continue
		}
		if !hasOpenIntentProductEvidence(a) {
			continue
		}
		if openIntentTopicalLink(ownerText, a) {
			return true
		}
	}
	return false
}

// parseAssistantGoalStatus mirrors claudia.ParseGoalStatus without importing
// the live Agent package into open-intent hermetics for a line scan.
func parseAssistantGoalStatus(text string) (string, bool) {
	for _, line := range strings.Split(text, "\n") {
		switch strings.TrimSpace(line) {
		case "GOAL_STATUS: complete":
			return "GOAL_STATUS: complete", true
		case "GOAL_STATUS: blocked":
			return "GOAL_STATUS: blocked", true
		}
	}
	return "", false
}

// OwnerIntentAnsweredWithProductEvidence reports whether any later assistant
// excerpt answers the owner complaint with a declared oracle or accepted-risk
// slot for the same topic (🎯T344). A SHA sentence is not that slot (🎯T766.4).
func OwnerIntentAnsweredWithProductEvidence(ownerText string, laterAssistant []string) bool {
	ownerText = strings.TrimSpace(ownerText)
	if ownerText == "" {
		return false
	}
	for _, a := range laterAssistant {
		a = strings.TrimSpace(a)
		if a == "" {
			continue
		}
		if !hasOpenIntentProductEvidence(a) {
			continue
		}
		if openIntentTopicalLink(ownerText, a) {
			return true
		}
	}
	return false
}

// OwnerQuestionAnsweredWithExplanation reports whether the owner turn is a
// question that a later excerpt answers on the same topic (🎯T477).
// openIntentAnswerShaped returns false: an explanation sentence is not
// that answer (🎯T766.4). Questions only: ownerqa.IsQuestion is the gate.
func OwnerQuestionAnsweredWithExplanation(ownerText string, laterAssistant []string) bool {
	ownerText = strings.TrimSpace(ownerText)
	if ownerText == "" || !ownerqa.IsQuestion(ownerText) {
		return false
	}
	for _, a := range laterAssistant {
		a = strings.TrimSpace(a)
		if a == "" || !openIntentAnswerShaped(a) {
			continue
		}
		if openIntentTopicalLink(ownerText, a) {
			return true
		}
	}
	return false
}

// ownerIntentClosedByLaterSubstantiveReply reports whether a later excerpt
// closes the instruction (🎯T568). The callee returns false: a sentence
// is not that close (🎯T766.4).
func ownerIntentClosedByLaterSubstantiveReply(laterAssistant []string) bool {
	for _, a := range laterAssistant {
		if openIntentSubstantiveReply(a) {
			return true
		}
	}
	return false
}

// ownerIntentReplyNamesClosedTarget reports whether a later overseer excerpt
// names a TargetID that is achieved or set_aside in statusByID (🎯T568). The
// owner instruction need not name the id — that was the T550 miss versus T528.
func ownerIntentReplyNamesClosedTarget(laterAssistant []string, statusByID map[string]string) bool {
	if len(statusByID) == 0 {
		return false
	}
	for _, a := range laterAssistant {
		for id := range openIntentTargetIDs(a) {
			st, ok := statusByID[id]
			if !ok {
				st, ok = statusByID[strings.ToLower(id)]
			}
			if ok && targetfile.IsClosedStatus(st) {
				return true
			}
		}
	}
	return false
}

// OwnerFleetDirectiveCompleted reports whether the owner turn is a structured
// fleet-op directive that a later excerpt completes for the same seat
// (🎯T512). A kill, stop, park, or reap sentence is not that directive
// (🎯T766.4). Unrelated seats leave the directive open.
func OwnerFleetDirectiveCompleted(ownerText string, laterAssistant []string) bool {
	targets := ownerFleetDirectiveTargets(ownerText)
	if len(targets) == 0 {
		return false
	}
	want := make(map[string]bool, len(targets))
	for _, target := range targets {
		if target != "" {
			want[target] = true
		}
	}
	for _, answer := range laterAssistant {
		answer = strings.TrimSpace(answer)
		if answer == "" || openIntentFleetOpsEvidence(answer) {
			continue
		}
		_, seat, _, ok := parseFleetOpExcerpt(answer)
		if ok && want[seat] {
			return true
		}
	}
	return false
}

// ownerFleetDirectiveTargets returns the seat of a structured fleet-op
// directive. A kill, stop, park, or reap sentence names no seat (🎯T766.4).
func ownerFleetDirectiveTargets(ownerText string) []string {
	_, seat, _, ok := parseFleetOpExcerpt(ownerText)
	if !ok || seat == "" {
		return nil
	}
	return []string{seat}
}

// normalizeFleetAgentName maps owner spellings onto registry-style ids:
// "Jevons PO" / "jevons_po" / "`jevons-po`" → "jevons-po".
func normalizeFleetAgentName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Trim(s, "`\"'")
	s = strings.TrimRight(s, ".,!;:?")
	if s == "" {
		return ""
	}
	var b strings.Builder
	prevHyphen := false
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevHyphen = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if b.Len() > 0 && !prevHyphen {
				b.WriteByte('-')
				prevHyphen = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	return out
}

// openIntentFleetOpsEvidence is not a fleet-op close (🎯T766.4).
// A killed, deregistered, or parked sentence is not a structured excerpt.
func openIntentFleetOpsEvidence(string) bool { return false }

// parseFleetOpExcerpt reads "fleet-op <leaf> <seat>" and, for fleet_intent,
// a following reaped or parked state. The fields are identifiers. A sentence
// is not this excerpt.
func parseFleetOpExcerpt(text string) (leaf, seat, state string, ok bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) < 3 || fields[0] != "fleet-op" {
		return "", "", "", false
	}
	leaf, seat = fields[1], fields[2]
	switch leaf {
	case "agent_kill", "agent_stop":
		if seat == "" {
			return "", "", "", false
		}
		return leaf, seat, "", true
	case "fleet_intent":
		if len(fields) < 4 || seat == "" {
			return "", "", "", false
		}
		if fields[3] != "reaped" && fields[3] != "parked" {
			return "", "", "", false
		}
		return leaf, seat, fields[3], true
	default:
		return "", "", "", false
	}
}

// fleetOpsToolIs reports whether a tool identifier is leaf or ends with a
// separator plus leaf. The comparison is the identifier, not a sentence.
func fleetOpsToolIs(name, leaf string) bool {
	if name == leaf {
		return true
	}
	for _, sep := range []string{"__", "/", "_"} {
		suffix := sep + leaf
		if len(name) >= len(suffix) && name[len(name)-len(suffix):] == suffix {
			return true
		}
	}
	return false
}

func isOpenIntentFleetOpsTool(name string, input map[string]any) bool {
	n := openIntentFleetOpsToolName(name, input)
	if fleetOpsToolIs(n, "agent_kill") || fleetOpsToolIs(n, "agent_stop") {
		return true
	}
	if fleetOpsToolIs(n, "fleet_intent") {
		state := openIntentFleetOpsToolState(input)
		return state == "reaped" || state == "parked"
	}
	return false
}

func openIntentFleetOpsToolName(name string, input map[string]any) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if input != nil {
		if tn, ok := input["tool_name"].(string); ok && strings.TrimSpace(tn) != "" {
			n = strings.ToLower(strings.TrimSpace(tn))
		}
	}
	return n
}

func openIntentFleetOpsToolState(input map[string]any) string {
	if input == nil {
		return ""
	}
	state, _ := input["state"].(string)
	if state == "" {
		if nested, ok := input["tool_input"].(map[string]any); ok {
			state, _ = nested["state"].(string)
		}
	}
	return strings.ToLower(strings.TrimSpace(state))
}

// synthesizeOpenIntentFleetOpsTool builds the structured fleet-op excerpt a
// close can recognize (🎯T512). flat is the rendered tool input and is not
// part of the excerpt: a sentence in that input is not the close (🎯T766.4).
func synthesizeOpenIntentFleetOpsTool(name string, input map[string]any, flat string) string {
	_ = flat
	tool := openIntentFleetOpsToolName(name, input)
	state := openIntentFleetOpsToolState(input)
	var leaf string
	switch {
	case fleetOpsToolIs(tool, "agent_kill"):
		leaf = "agent_kill"
	case fleetOpsToolIs(tool, "agent_stop"):
		leaf = "agent_stop"
	case fleetOpsToolIs(tool, "fleet_intent"):
		if state != "reaped" && state != "parked" {
			return ""
		}
		leaf = "fleet_intent"
	default:
		return ""
	}
	target := openIntentFleetOpsToolTarget(input)
	if target == "" {
		return ""
	}
	if leaf == "fleet_intent" {
		return "fleet-op " + leaf + " " + target + " " + state
	}
	return "fleet-op " + leaf + " " + target
}

func openIntentFleetOpsToolTarget(input map[string]any) string {
	if input == nil {
		return ""
	}
	for _, key := range []string{"name", "target", "agent"} {
		if v, ok := input[key].(string); ok {
			if n := normalizeFleetAgentName(v); n != "" {
				return n
			}
		}
	}
	if nested, ok := input["tool_input"].(map[string]any); ok {
		return openIntentFleetOpsToolTarget(nested)
	}
	return ""
}

// openIntentAnswerShaped is not an explanation close (🎯T766.4).
// A connective or a file name is not an answer.
func openIntentAnswerShaped(string) bool { return false }

// openIntentSubstantiveReply is not a close (🎯T766.4).
// A long sentence is not a reply that finishes owner work.
func openIntentSubstantiveReply(string) bool { return false }

// hasOpenIntentProductEvidence is true when the excerpt declares an oracle
// or accepted-risk slot. A SHA sentence, a hermetic PASS, or the word
// achieved is not product evidence (🎯T766.4).
func hasOpenIntentProductEvidence(text string) bool {
	return HasOracleEvidence(text) || HasAcceptedRisk(text)
}

// openIntentTopicalLink is true when the owner text and the answer share a
// target id. Token overlap is not that link (🎯T766.4).
func openIntentTopicalLink(owner, answer string) bool {
	oIDs := openIntentTargetIDs(owner)
	aIDs := openIntentTargetIDs(answer)
	for id := range oIDs {
		if aIDs[id] {
			return true
		}
	}
	return false
}

func openIntentTargetIDs(s string) map[string]bool {
	out := make(map[string]bool)
	for _, m := range openIntentTargetIDRe.FindAllString(s, -1) {
		id := strings.ToUpper(strings.TrimPrefix(strings.TrimSpace(m), "🎯"))
		if id != "" {
			out[id] = true
		}
	}
	return out
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
			Role:   "user",
		})
	}
	return out
}

// LoadOpenOwnerIntent extracts recoverable open owner intent from the
// overseer transcript — the product statedb whenever it exists, otherwise
// state_dir/chatlog/<overseer>.jsonl. Empty or unreadable canonical history
// must never resurrect removed instructions from the legacy file.
// Loads user turns plus assistant excerpts for 🎯T344 / 🎯T477 / 🎯T512 / 🎯T568
// disposition (product evidence, explanation answers, fleet ops, later
// substantive replies, TargetID-naming turns).
func LoadOpenOwnerIntent(stateDir, overseer string) OpenOwnerIntent {
	return LoadOpenOwnerIntentWithLedger(stateDir, overseer, "")
}

// LoadOpenOwnerIntentWithLedger is LoadOpenOwnerIntent plus 🎯T528 ledger
// Goal close: when ledgerCwd resolves a bullseye.yaml, TargetIDs named in
// the newest owner turn that are all achieved/set_aside yield
// answered_or_closed (no owner-intent-resume / Continue inject).
func LoadOpenOwnerIntentWithLedger(stateDir, overseer, ledgerCwd string) OpenOwnerIntent {
	stateDir = strings.TrimSpace(stateDir)
	overseer = strings.TrimSpace(overseer)
	if overseer == "" {
		overseer = "jevons"
	}
	if stateDir == "" {
		return OpenOwnerIntent{Residual: ResidualNoChatlog}
	}
	// 🎯T592 / 🎯T548.2: statedb is where turns live —
	// the chatlog JSONL froze at its last pre-SQLite record, and sourcing
	// a MANDATORY resume from it re-issued a five-day-old instruction
	// after every daemon bounce. JSONL is fallback only for pre-statedb
	// state dirs; empty canonical history is still authoritative.
	path := filepath.Join(stateDir, "chatlog", overseer+".jsonl")
	turns, newestFrame, fromDB, err := loadOpenIntentDialogueStateDB(stateDir, overseer, DefaultOpenIntentLookback, DefaultOpenIntentAssistantCap)
	if err != nil {
		slog.Warn("owner intent recovery could not read canonical history", "overseer", overseer, "err", err)
		return OpenOwnerIntent{Residual: ResidualUnreadableChatlog}
	}
	if !fromDB {
		var err error
		turns, newestFrame, err = loadOpenIntentDialogue(path, DefaultOpenIntentLookback, DefaultOpenIntentAssistantCap)
		if err != nil {
			return OpenOwnerIntent{Residual: ResidualNoChatlog}
		}
	}
	// 🎯T592: a store still taking frames but no longer recording turns
	// cannot say what the newest owner instruction is, so it does not
	// source a MANDATORY resume.
	if ChatlogDegraded(newestOpenIntentTurnTS(turns), newestFrame) {
		return OpenOwnerIntent{Residual: ResidualStaleChatlog}
	}
	if len(turns) == 0 {
		if fromDB {
			// An empty store also establishes that there is no owner intent.
			return OpenOwnerIntent{Residual: ResidualNoUserTurns}
		}
		// Distinguish missing file vs empty / assistant-only.
		if _, statErr := os.Stat(path); statErr != nil {
			return OpenOwnerIntent{Residual: ResidualNoChatlog}
		}
		return OpenOwnerIntent{Residual: ResidualNoUserTurns}
	}
	var status map[string]string
	if cwd := strings.TrimSpace(ledgerCwd); cwd != "" {
		// Scan user *and* assistant turns: 🎯T568 closes when a later reply
		// names a ledger-closed TargetID the owner text never mentioned.
		var goalParts []string
		for _, t := range turns {
			if ids := fleet.GoalTargetIDs(t.Text); len(ids) > 0 {
				goalParts = append(goalParts, t.Text)
			}
		}
		if len(goalParts) > 0 {
			status = fleet.LoadGoalTargetStatuses(cwd, strings.Join(goalParts, "\n"))
		}
	}
	return ExtractOpenOwnerIntentWithLedger(turns, status)
}

// loadOpenIntentDialogue streams chatlog JSONL into user turns + assistant
// excerpts kept for close disposition (text and tool_use attestation/send
// bodies). Caps user lookback and assistant count so assistant-heavy logs
// stay bounded.
func loadOpenIntentDialogue(path string, maxUser, maxAssistant int) ([]OwnerIntentTurn, time.Time, error) {
	var newestFrame time.Time
	if strings.TrimSpace(path) == "" {
		return nil, newestFrame, nil
	}
	if maxUser <= 0 {
		maxUser = DefaultOpenIntentLookback
	}
	if maxAssistant <= 0 {
		maxAssistant = DefaultOpenIntentAssistantCap
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, newestFrame, nil
		}
		return nil, newestFrame, err
	}
	defer f.Close()

	var scanErr error
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	turns, folded := foldOpenIntentDialogue(func(yield func(string) bool) {
		for sc.Scan() {
			if !yield(sc.Text()) {
				return
			}
		}
		scanErr = sc.Err()
	}, maxUser, maxAssistant)
	if scanErr != nil {
		return nil, folded, scanErr
	}
	return turns, folded, nil
}

// loadOpenIntentDialogueStateDB reads the overseer dialogue from the
// product statedb (🎯T592). Only an absent database permits JSONL fallback.
// Opening read-only prevents a stat/open race from creating a replacement
// store and prevents recovery from applying schema repairs to its evidence.
func loadOpenIntentDialogueStateDB(stateDir, overseer string, maxUser, maxAssistant int) ([]OwnerIntentTurn, time.Time, bool, error) {
	dbPath := statedb.DefaultPath(stateDir)
	if _, err := os.Lstat(dbPath); err != nil {
		if os.IsNotExist(err) {
			return nil, time.Time{}, false, nil
		}
		return nil, time.Time{}, true, err
	}
	db, err := statedb.OpenReadOnly(dbPath)
	if err != nil {
		return nil, time.Time{}, true, err
	}
	defer db.Close()
	if maxUser <= 0 {
		maxUser = DefaultOpenIntentLookback
	}
	snapshot, err := db.TailSnapshot(overseer, maxUser)
	if err != nil {
		return nil, time.Time{}, true, err
	}
	rows := snapshot.Events
	turns, newestFrame := foldOpenIntentDialogue(func(yield func(string) bool) {
		for _, r := range rows {
			if !yield(overlayOpenIntentRowTS(r.Body, r.TS)) {
				return
			}
		}
	}, maxUser, maxAssistant)
	// The ts column is authoritative for staleness even when a body
	// carries no timestamp field of its own.
	for _, r := range rows {
		if ts := parseOpenIntentTS(r.TS); ts.After(newestFrame) {
			newestFrame = ts
		}
	}
	return turns, newestFrame, true, nil
}

// foldOpenIntentDialogue folds a stream of chat event lines — chatlog
// JSONL lines or statedb body columns, which are the same shape — into
// user turns + kept assistant excerpts, plus the newest frame timestamp
// of any type for 🎯T592 staleness.
func foldOpenIntentDialogue(lines func(yield func(string) bool), maxUser, maxAssistant int) ([]OwnerIntentTurn, time.Time) {
	var newestFrame time.Time
	if maxUser <= 0 {
		maxUser = DefaultOpenIntentLookback
	}
	if maxAssistant <= 0 {
		maxAssistant = DefaultOpenIntentAssistantCap
	}

	var users []OwnerIntentTurn
	// Assistants kept after the oldest retained user (ring by user count).
	type asst struct {
		turn      OwnerIntentTurn
		afterUser int // index in users at append time (user count)
	}
	var assts []asst

	lines(func(raw string) bool {
		line := strings.TrimSpace(raw)
		if line == "" {
			return true
		}
		// 🎯T592: every frame counts for staleness, including the
		// progress/tool chrome extraction skips. A store that is still
		// being appended to but has stopped recording turns is the
		// defect; a store nobody writes at all is merely old.
		if ts := openIntentFrameTS(line); ts.After(newestFrame) {
			newestFrame = ts
		}
		role, text, ts, ok := parseOpenIntentChatlogLine(line)
		if !ok || text == "" {
			return true
		}
		switch role {
		case "user":
			users = append(users, OwnerIntentTurn{
				Text: text, Source: "owner_chat", TS: ts, Role: "user",
			})
			if len(users) > maxUser {
				// Drop oldest user; prune assistants that were only after dropped users.
				drop := len(users) - maxUser
				users = users[drop:]
				kept := assts[:0]
				for _, a := range assts {
					nu := a.afterUser - drop
					if nu < 0 {
						continue
					}
					a.afterUser = nu
					kept = append(kept, a)
				}
				assts = kept
			}
		case "assistant":
			// Keep evidence-shaped excerpts (T344), explanation answers (T477),
			// fleet-ops completion (T512), later substantive replies (T568),
			// and TargetID-naming turns so a ledger-closed id in a short
			// "Filed 🎯T550" reply still reaches extraction. Dropping any of
			// those re-fires an already-closed owner turn on every bounce.
			if !keepOpenIntentAssistant(text) {
				return true
			}
			assts = append(assts, asst{
				turn: OwnerIntentTurn{
					Text:   truncateRunes(text, MaxOpenIntentRunes),
					Source: "owner_chat", TS: ts, Role: "assistant",
				},
				afterUser: len(users),
			})
			if len(assts) > maxAssistant {
				assts = assts[len(assts)-maxAssistant:]
			}
		}
		return true
	})
	if len(users) == 0 {
		return nil, newestFrame
	}

	// Merge users + assistants into oldest→newest timeline by user index.
	out := make([]OwnerIntentTurn, 0, len(users)+len(assts))
	ai := 0
	for ui, u := range users {
		out = append(out, u)
		for ai < len(assts) && assts[ai].afterUser == ui+1 {
			// afterUser is count of users when assistant was appended (= index+1
			// after that user). Assistants before any user: afterUser==0 — skip
			// for disposition (nothing to close).
			out = append(out, assts[ai].turn)
			ai++
		}
		// Also drain assistants tagged afterUser == ui+1 only above; assistants
		// between user ui and ui+1 have afterUser == ui+1 when appended right
		// after user ui was added... When user is appended, len(users)=ui+1,
		// so assistants after user ui have afterUser == ui+1. Good.
	}
	// Trailing assistants after the last user (afterUser == len(users)).
	for ai < len(assts) {
		if assts[ai].afterUser == len(users) {
			out = append(out, assts[ai].turn)
		}
		ai++
	}
	return out, newestFrame
}

// overlayOpenIntentRowTS copies the statedb ts column onto a body that
// has no timestamp field. Restart recovery (🎯T627.3) must see the
// authoritative observation time even when the JSON body omitted it.
func overlayOpenIntentRowTS(body, rowTS string) string {
	if strings.TrimSpace(body) == "" {
		return body
	}
	if !openIntentFrameTS(body).IsZero() {
		return body
	}
	rowTS = strings.TrimSpace(rowTS)
	if rowTS == "" || parseOpenIntentTS(rowTS).IsZero() {
		return body
	}
	var m map[string]any
	if json.Unmarshal([]byte(body), &m) != nil {
		return body
	}
	m["timestamp"] = rowTS
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return string(out)
}

// openIntentFrameTS reads the timestamp off any chatlog line, whatever
// its type (🎯T592 staleness). Extraction proper skips progress frames;
// staleness is exactly the question of how far they run past the last
// turn.
func openIntentFrameTS(line string) time.Time {
	var d struct {
		Timestamp string `json:"timestamp"`
	}
	if json.Unmarshal([]byte(line), &d) != nil || d.Timestamp == "" {
		return time.Time{}
	}
	return parseOpenIntentTS(d.Timestamp)
}

// parseOpenIntentTS parses an RFC3339(Nano) timestamp string; zero when
// empty or malformed.
func parseOpenIntentTS(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

// keepOpenIntentAssistant reports whether an assistant chatlog excerpt must
// be retained for close disposition (🎯T344 / T477 / T512 / T568).
func keepOpenIntentAssistant(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" {
		return false
	}
	if hasOpenIntentProductEvidence(text) || openIntentEvidenceCandidate(text) {
		return true
	}
	if openIntentAnswerShaped(text) {
		return true
	}
	if _, _, _, ok := parseFleetOpExcerpt(text); ok {
		return true
	}
	if openIntentSubstantiveReply(text) {
		return true
	}
	return openIntentTargetIDRe.MatchString(text)
}

// openIntentEvidenceCandidate is not product evidence (🎯T766.4).
// A SHA or the word achieved is not a slot.
func openIntentEvidenceCandidate(string) bool { return false }

// parseOpenIntentChatlogLine extracts role + plain text from a chatlog JSONL line.
// Assistant tool_use bodies are flattened when they carry evidence-shaped fields.
func parseOpenIntentChatlogLine(line string) (role, text string, ts time.Time, ok bool) {
	var d struct {
		Type      string `json:"type"`
		Timestamp string `json:"timestamp"`
		Text      string `json:"text"`
		Message   struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal([]byte(line), &d) != nil {
		return "", "", time.Time{}, false
	}
	typ := strings.ToLower(strings.TrimSpace(d.Type))
	switch typ {
	case "user", "error":
		role = "user"
		if typ == "error" {
			role = "user" // treat as owner-side friction/instruction surface
		}
		text = extractOpenIntentContentText(d.Message.Content)
		if text == "" {
			text = strings.TrimSpace(d.Text)
		}
	case "assistant":
		role = "assistant"
		text = extractOpenIntentAssistantContent(d.Message.Content)
	default:
		// agent_note / status / etc. are not owner instructions; skip.
		return "", "", time.Time{}, false
	}
	if strings.TrimSpace(text) == "" {
		return "", "", time.Time{}, false
	}
	if d.Timestamp != "" {
		if t, err := time.Parse(time.RFC3339Nano, d.Timestamp); err == nil {
			ts = t
		} else if t, err := time.Parse(time.RFC3339, d.Timestamp); err == nil {
			ts = t
		}
	}
	return role, strings.TrimSpace(text), ts, true
}

func extractOpenIntentContentText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		var b strings.Builder
		for _, p := range parts {
			if p.Text != "" {
				b.WriteString(p.Text)
			}
		}
		return strings.TrimSpace(b.String())
	}
	return ""
}

// extractOpenIntentAssistantContent joins text blocks and evidence-shaped
// tool_use inputs (attestation, agent_send text, achieve payloads).
func extractOpenIntentAssistantContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Plain string content.
	if s := extractOpenIntentContentText(raw); s != "" {
		// May still be only text; also try tool array below if raw is array.
		var probe []json.RawMessage
		if json.Unmarshal(raw, &probe) != nil {
			return s
		}
	}
	var blocks []map[string]json.RawMessage
	if json.Unmarshal(raw, &blocks) != nil {
		return extractOpenIntentContentText(raw)
	}
	var chunks []string
	for _, b := range blocks {
		var typ string
		_ = json.Unmarshal(b["type"], &typ)
		typ = strings.ToLower(strings.TrimSpace(typ))
		switch typ {
		case "text":
			var t string
			_ = json.Unmarshal(b["text"], &t)
			t = strings.TrimSpace(t)
			if t != "" {
				chunks = append(chunks, t)
			}
		case "tool_use":
			// Flatten input object (and nested tool_input for use_tool MCP).
			var toolName string
			_ = json.Unmarshal(b["name"], &toolName)
			var input map[string]any
			if json.Unmarshal(b["input"], &input) != nil {
				continue
			}
			flat := flattenOpenIntentToolInput(input)
			if flat == "" && !isOpenIntentFleetOpsTool(toolName, input) {
				continue
			}
			if hasOpenIntentProductEvidence(flat) || openIntentEvidenceCandidate(flat) {
				chunks = append(chunks, flat)
				continue
			}
			// 🎯T512: agent_kill / agent_stop / fleet_intent reaped|parked are
			// themselves the completion act. The excerpt is the structured
			// fleet-op line, not the English words that used to stand in for it.
			if isOpenIntentFleetOpsTool(toolName, input) {
				if syn := synthesizeOpenIntentFleetOpsTool(toolName, input, flat); syn != "" {
					chunks = append(chunks, syn)
				}
			}
		}
	}
	return strings.TrimSpace(strings.Join(chunks, "\n"))
}

func flattenOpenIntentToolInput(input map[string]any) string {
	if input == nil {
		return ""
	}
	var parts []string
	// Prefer high-signal fields first.
	for _, key := range []string{"attestation", "text", "command", "prompt", "name"} {
		if v, ok := input[key]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				parts = append(parts, s)
			}
		}
	}
	// Nested MCP tool_input (use_tool wrapper).
	if nested, ok := input["tool_input"].(map[string]any); ok {
		if s := flattenOpenIntentToolInput(nested); s != "" {
			parts = append(parts, s)
		}
	}
	// op=achieve is strong signal even without attestation field alone.
	if op, ok := input["op"].(string); ok && strings.ToLower(op) == "achieve" {
		parts = append(parts, "op=achieve")
		if id, ok := input["id"].(string); ok {
			parts = append(parts, "id="+id)
		}
	}
	if len(parts) == 0 {
		// Last resort: whole JSON (bounded) for SHA/target scanning.
		if raw, err := json.Marshal(input); err == nil {
			s := string(raw)
			if len(s) > 4000 {
				s = s[:4000]
			}
			return s
		}
		return ""
	}
	return strings.Join(parts, "\n")
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

// isOpenIntentSendProbe reports a declared send-probe fact (🎯T551, 🎯T766.4).
// The line is "open-intent send-probe" or that fact plus one token.
// A slug or a sentence that mentions send-probe is owner text.
func isOpenIntentSendProbe(text string) bool {
	fields := strings.Fields(text)
	switch len(fields) {
	case 2, 3:
	default:
		return false
	}
	return fields[0] == "open-intent" && fields[1] == "send-probe"
}

// isOpenIntentHarness reports machine turns that are not owner work.
// Empty text is not an instruction. A protocol frame is a JSON object
// with a type field (🎯T362). A declared fact is "open-intent harness"
// plus one kind: daemon-restart, standing-brief, event, system-reminder,
// or agent-responded. A rendered banner is owner text (🎯T766.4).
func isOpenIntentHarness(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return true
	}
	if isOpenIntentProtocolJSON(t) {
		return true
	}
	fields := strings.Fields(t)
	if len(fields) != 3 || fields[0] != "open-intent" || fields[1] != "harness" {
		return false
	}
	switch fields[2] {
	case "daemon-restart", "standing-brief", "event", "system-reminder", "agent-responded":
		return true
	default:
		return false
	}
}

// isOpenIntentProtocolJSON reports whether a chatlog user turn is really a
// protocol control frame rather than owner prose (🎯T362): a bare JSON object
// carrying a non-empty string "type" (ux_state, ping, rewind, inspect_*, …).
// Deliberately generic — the next control frame the client learns to send must
// not be able to reopen this hole before the server-side filter catches up.
func isOpenIntentProtocolJSON(text string) bool {
	t := strings.TrimSpace(text)
	if len(t) < 2 || t[0] != '{' || t[len(t)-1] != '}' {
		return false
	}
	var probe map[string]json.RawMessage
	if json.Unmarshal([]byte(t), &probe) != nil {
		return false
	}
	raw, ok := probe["type"]
	if !ok {
		return false
	}
	var typ string
	if json.Unmarshal(raw, &typ) != nil {
		return false
	}
	return strings.TrimSpace(typ) != ""
}

// isOpenIntentRestartNudge skips an exact bounce word. A sentence about a
// restart is the owner's text, not a nudge (🎯T766.4).
func isOpenIntentRestartNudge(text string) bool {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "continue", "keep going", "resume", "go on", "carry on", "please continue":
		return true
	default:
		return false
	}
}

// isOpenIntentAckOnly is an exact ack word. A short sentence is not an ack,
// and a prefix of an ack is not an ack (🎯T766.4). A fleet directive is a
// work order.
func isOpenIntentAckOnly(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return true
	}
	if len(ownerFleetDirectiveTargets(t)) > 0 {
		return false
	}
	switch strings.ToLower(t) {
	case "ok", "okay", "thanks", "thank you", "ty", "cool", "great", "nice",
		"perfect", "good", "lgtm", "got it", "sounds good", "brilliant",
		"yes", "yep", "nope", "no", "k", "👍":
		return true
	default:
		return false
	}
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

// newestOpenIntentTurnTS is the newest timestamp across recovered turns
// of either role (🎯T592).
func newestOpenIntentTurnTS(turns []OwnerIntentTurn) time.Time {
	var newest time.Time
	for _, t := range turns {
		if t.TS.After(newest) {
			newest = t.TS
		}
	}
	return newest
}

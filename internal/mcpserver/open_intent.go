// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/marcelocantos/jevons/internal/ownerqa"
	"github.com/marcelocantos/jevons/internal/rsi"
)

// 🎯T328: post-restart resume of unfinished owner instructions.
// 🎯T344: do not re-fire owner turns already answered with product evidence.
// 🎯T477: an owner *question* is also closed by a topically-linked explanation
// answer — "why did X" answered with "because Y" carries no SHA/PASS/achieve
// marker, and before this every bounce re-fired the answered question as
// owner-intent-resume.
// 🎯T512: an owner fleet directive (kill/stop/park/reap a named seat) is closed
// by later ops evidence (killed/deregistered, gone from agent_list, fleet
// intent reaped/parked) for that seat — ops directives have no code SHA, and
// before this every bounce re-fired "Kill jevons-po" after the seat was gone.
//
// Durable source: state_dir/chatlog/<overseer>.jsonl (survives control-plane
// bounce). On restart, when a recoverable open owner instruction is found,
// the overseer gets an owner-intent-resume event that forces a real turn with
// that instruction — not silent-idle-only / status-dump.
//
// Residual classes (no resume payload; daemon-restarted status path only):
//   - no_chatlog / no_user_turns / only_harness / ack_only / no_recoverable_intent
//   - answered_or_closed (T344/T477/T512: later assistant evidence closed the newest work)
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
	ResidualNoUserTurns         = "no_user_turns"
	ResidualOnlyHarness         = "only_harness"
	ResidualAckOnly             = "ack_only"
	ResidualNoRecoverableIntent = "no_recoverable_intent"
	// ResidualAnsweredOrClosed: newest substantive owner turn already has a
	// later overseer/assistant answer with product evidence (🎯T344).
	ResidualAnsweredOrClosed = "answered_or_closed"
)

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
// explanation answers close questions (🎯T477); fleet ops evidence closes
// kill/stop/park/reap directives (🎯T512). Hermetic oracle surface for
// 🎯T328 + 🎯T344 + 🎯T477 + 🎯T512.
func ExtractOpenOwnerIntent(turns []OwnerIntentTurn) OpenOwnerIntent {
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
		case sawHarnessOnly:
			return OpenOwnerIntent{Residual: ResidualOnlyHarness}
		default:
			return OpenOwnerIntent{Residual: ResidualNoRecoverableIntent}
		}
	}
	last := timeline[lastUserIdx].turn

	// 🎯T344: later assistant product evidence for the same complaint → closed.
	var laterAnswers []string
	for i := lastUserIdx + 1; i < len(timeline); i++ {
		if !timeline[i].user {
			laterAnswers = append(laterAnswers, timeline[i].turn.Text)
		}
	}
	if OwnerIntentAnsweredWithProductEvidence(last.Text, laterAnswers) {
		return OpenOwnerIntent{Residual: ResidualAnsweredOrClosed}
	}

	// 🎯T477: a question is answered by explanation prose, not only by product
	// evidence. Directives keep the stricter T344 contract — an explanation of
	// why work is needed does not close a work order.
	if OwnerQuestionAnsweredWithExplanation(last.Text, laterAnswers) {
		return OpenOwnerIntent{Residual: ResidualAnsweredOrClosed}
	}

	// 🎯T512: kill/stop/park/reap fleet directives close on ops evidence for the
	// named seat (no SHA/PASS/achieve required).
	if OwnerFleetDirectiveCompleted(last.Text, laterAnswers) {
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

// OwnerIntentAnsweredWithProductEvidence reports whether any later assistant
// excerpt answers the owner complaint with product evidence (SHA and/or
// hermetic PASS and/or achieved-target attestation) for the same topic (🎯T344).
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
// question that a later assistant excerpt answers with explanation prose on
// the same topic (🎯T477). Questions only: ownerqa.IsQuestion is the gate, so
// directives keep the T344 product-evidence contract. The answer must be
// answer-shaped (explanatory substance, not a progress ack) and topically
// linked, so "looking into it" and an unrelated explanation both leave the
// question open — the cost of a false close is a lost resume, which is the
// T328 regression class.
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

// openIntentFleetDirectiveRe matches an owner kill/stop/park/reap directive
// aimed at a named fleet seat (🎯T512). Captures the verb and the remainder
// (agent name / "Jevons PO" / backtick name).
var openIntentFleetDirectiveRe = regexp.MustCompile(`(?i)^\s*(?:please\s+)?(kill|stop|park|reap)\b(?:\s+(?:the|a|an))?\s+(.+?)\s*$`)

// OwnerFleetDirectiveCompleted reports whether the owner turn is a fleet
// kill/stop/park/reap directive that a later assistant excerpt completes with
// ops evidence for the same named seat — killed/deregistered, gone from the
// agent list, stopped+parked, or fleet intent reaped/parked — without needing
// SHA/PASS/achieve markers (🎯T512). Unrelated seats and progress chatter leave
// the directive open.
func OwnerFleetDirectiveCompleted(ownerText string, laterAssistant []string) bool {
	targets := ownerFleetDirectiveTargets(ownerText)
	if len(targets) == 0 {
		return false
	}
	for _, a := range laterAssistant {
		a = strings.TrimSpace(a)
		if a == "" || !openIntentFleetOpsEvidence(a) {
			continue
		}
		if openIntentFleetOpsNamesTarget(a, targets) {
			return true
		}
	}
	return false
}

// ownerFleetDirectiveTargets returns normalised agent names the owner asked
// to kill/stop/park/reap. Empty when the turn is not a fleet directive or
// names no seat (e.g. "kill all workers" without a concrete agent id).
func ownerFleetDirectiveTargets(ownerText string) []string {
	ownerText = strings.TrimSpace(ownerText)
	if ownerText == "" {
		return nil
	}
	m := openIntentFleetDirectiveRe.FindStringSubmatch(ownerText)
	if m == nil {
		return nil
	}
	rest := stripFleetDirectiveNoise(strings.TrimSpace(m[2]))
	if rest == "" {
		return nil
	}
	// Prefer backtick-quoted names when present.
	var names []string
	for _, q := range extractBacktickTokens(rest) {
		if n := normalizeFleetAgentName(q); n != "" && looksLikeFleetAgentName(n) {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		if n := normalizeFleetAgentName(rest); n != "" && looksLikeFleetAgentName(n) {
			names = append(names, n)
		}
	}
	return uniqueStrings(names)
}

// stripFleetDirectiveNoise drops trailing filler so "Kill jevons-po please"
// and "Kill the jevons-po agent now" still resolve to the seat name.
func stripFleetDirectiveNoise(rest string) string {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return ""
	}
	noise := []string{
		"from the fleet", "from fleet", "please", "now", "immediately",
		"thanks", "thank you", "agent", "seat", "process",
	}
	for {
		low := strings.ToLower(rest)
		trimmed := false
		for _, n := range noise {
			if strings.HasSuffix(low, n) {
				rest = strings.TrimSpace(rest[:len(rest)-len(n)])
				rest = strings.TrimRight(rest, ".,!;:?")
				trimmed = true
				break
			}
		}
		if !trimmed {
			break
		}
	}
	return strings.TrimSpace(rest)
}

func extractBacktickTokens(s string) []string {
	var out []string
	for {
		i := strings.IndexByte(s, '`')
		if i < 0 {
			break
		}
		s = s[i+1:]
		j := strings.IndexByte(s, '`')
		if j < 0 {
			break
		}
		tok := strings.TrimSpace(s[:j])
		if tok != "" {
			out = append(out, tok)
		}
		s = s[j+1:]
	}
	return out
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

// looksLikeFleetAgentName rejects bare plurals ("workers") and one-word
// noise so "Kill all workers" does not close on coincidental chatter.
func looksLikeFleetAgentName(n string) bool {
	if n == "" || !strings.Contains(n, "-") {
		return false
	}
	// Reject all-digit segments-only noise; require a letter somewhere.
	hasLetter := false
	for _, r := range n {
		if unicode.IsLetter(r) {
			hasLetter = true
			break
		}
	}
	return hasLetter
}

func uniqueStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// openIntentFleetOpsEvidence detects kill/stop/park/reap completion prose
// or tool payloads (🎯T512) — stricter than bare "working on the kill".
func openIntentFleetOpsEvidence(text string) bool {
	low := strings.ToLower(strings.TrimSpace(text))
	if low == "" {
		return false
	}
	// Progress chatter is not completion.
	for _, m := range openIntentProgressMarkers {
		if strings.Contains(low, m) {
			return false
		}
	}
	strong := []string{
		"killed", "deregistered", "deregister",
		"stopped and parked",
		"already not registered",
		"no longer in the registry",
		"gone from the registry",
		"removed from the fleet",
		"removed from the registry",
		"not on agent list",
		"no longer appears in agent_list",
		"no longer on agent_list",
		"state=reaped", "state=parked",
		`"state":"reaped"`, `"state":"parked"`,
		`"state": "reaped"`, `"state": "parked"`,
	}
	for _, m := range strong {
		if strings.Contains(low, m) {
			return true
		}
	}
	if strings.Contains(low, "fleet intent") &&
		(strings.Contains(low, "reaped") || strings.Contains(low, "parked")) {
		return true
	}
	if strings.Contains(low, "parked") &&
		(strings.Contains(low, "still registered") || strings.Contains(low, "nothing revives")) {
		return true
	}
	// Tool names that are themselves the completion act.
	if strings.Contains(low, "agent_kill") || strings.Contains(low, "agent_stop") ||
		(strings.Contains(low, "fleet_intent") &&
			(strings.Contains(low, "reaped") || strings.Contains(low, "parked"))) {
		return true
	}
	return false
}

func openIntentFleetOpsNamesTarget(answer string, targets []string) bool {
	if len(targets) == 0 {
		return false
	}
	low := strings.ToLower(answer)
	for _, t := range targets {
		if t == "" {
			continue
		}
		if strings.Contains(low, t) {
			return true
		}
		spaced := strings.ReplaceAll(t, "-", " ")
		if spaced != t && strings.Contains(low, spaced) {
			return true
		}
	}
	return false
}

func isOpenIntentFleetOpsTool(name string, input map[string]any) bool {
	n := openIntentFleetOpsToolName(name, input)
	if strings.Contains(n, "agent_kill") || strings.Contains(n, "agent_stop") {
		return true
	}
	if strings.Contains(n, "fleet_intent") {
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

// synthesizeOpenIntentFleetOpsTool builds a chatlog excerpt from a fleet-ops
// tool_use so extraction can match both the completion act and the seat name
// (🎯T512). Flat input alone often carries only `name=jevons-po`.
func synthesizeOpenIntentFleetOpsTool(name string, input map[string]any, flat string) string {
	tool := openIntentFleetOpsToolName(name, input)
	state := openIntentFleetOpsToolState(input)
	var parts []string
	switch {
	case strings.Contains(tool, "agent_kill"):
		parts = append(parts, "agent_kill", "killed", "deregistered")
	case strings.Contains(tool, "agent_stop"):
		parts = append(parts, "agent_stop", "stopped and parked")
	case strings.Contains(tool, "fleet_intent"):
		parts = append(parts, "fleet_intent")
		if state != "" {
			parts = append(parts, "state="+state)
		}
	default:
		if tool != "" {
			parts = append(parts, tool)
		}
	}
	if target := openIntentFleetOpsToolTarget(input); target != "" {
		parts = append(parts, target)
	}
	flat = strings.TrimSpace(flat)
	if flat != "" {
		parts = append(parts, flat)
	}
	return strings.TrimSpace(strings.Join(parts, " "))
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

// openIntentExplanationConnectives mark prose as explaining a cause rather
// than acknowledging work. Deliberately concrete: generic verbs would let
// status chatter close a question.
var openIntentExplanationConnectives = []string{
	"because", "due to", "caused by", "the cause", "root cause", "the reason",
	"reason is", "turns out", "loses to", "wins over", "takes precedence",
	"overrides", "overridden", "shadowed by", "falls back", "comes from",
	"leftover",
}

// openIntentProgressMarkers veto answer shape: a turn announcing that the
// answer is being sought is not the answer.
var openIntentProgressMarkers = []string{
	"looking into", "investigating", "digging into", "working on",
	"will report", "stand by", "one moment", "checking ",
}

// openIntentFileTokenRe matches a concrete file/knob citation — the
// "citing knobs/files" half of an explanation answer.
var openIntentFileTokenRe = regexp.MustCompile(`(?i)[\w./~-]+\.(go|yaml|yml|json|md|js|ts|html|css|sh|sql|txt|plist)\b`)

// openIntentAnswerShaped reports whether assistant prose has the substance of
// an explanation answer (🎯T477): an explanatory connective, or a concrete
// file/knob citation without progress-marker framing. Short turns never
// qualify — an answer that fits in an ack is an ack.
func openIntentAnswerShaped(text string) bool {
	t := strings.TrimSpace(text)
	if utf8.RuneCountInString(t) < 40 {
		return false
	}
	low := strings.ToLower(t)
	for _, m := range openIntentProgressMarkers {
		if strings.Contains(low, m) {
			return false
		}
	}
	for _, c := range openIntentExplanationConnectives {
		if strings.Contains(low, c) {
			return true
		}
	}
	return openIntentFileTokenRe.MatchString(t)
}

// hasOpenIntentProductEvidence detects SHA / hermetic PASS / achieved-target
// product evidence in an assistant excerpt (stricter than bare chatter).
func hasOpenIntentProductEvidence(text string) bool {
	s := strings.TrimSpace(text)
	if s == "" {
		return false
	}
	low := strings.ToLower(s)
	hasSHA := commitSHARe.MatchString(s)
	hasTarget := openIntentTargetIDRe.MatchString(s)
	hasAchieved := strings.Contains(low, "achieved") || strings.Contains(low, "attestation")
	hasFixCommit := strings.Contains(low, "fix(") || strings.Contains(low, "feat(")
	hasPass := strings.Contains(low, "pass") &&
		(strings.Contains(low, "hermetic") ||
			strings.Contains(low, "test") ||
			strings.Contains(low, "oracle") ||
			strings.Contains(low, "playwright") ||
			strings.Contains(low, "go test") ||
			strings.Contains(low, "make test") ||
			strings.Contains(low, "node "))

	// SHA + closing context (fix/feat/achieved/pass/sha/commit/landed).
	if hasSHA && (hasAchieved || hasFixCommit || hasPass ||
		strings.Contains(low, "sha") || strings.Contains(low, "commit") ||
		strings.Contains(low, "landed") || strings.Contains(low, "merged")) {
		return true
	}
	// Achieved target attestation (with or without SHA).
	if hasAchieved && (hasTarget || hasSHA) {
		return true
	}
	// Hermetic PASS with target or SHA ties the oracle to work product.
	if hasPass && (hasTarget || hasSHA) {
		return true
	}
	// Reuse T31 oracle classifier when it already sees SHA/test evidence
	// alongside a completion claim (achieved/done/finished).
	if HasOracleEvidence(s) && hasCompletionClaim(low) {
		return true
	}
	return false
}

// openIntentTopicalLink is true when owner complaint and evidence answer
// share a target id or significant content tokens (same complaint).
func openIntentTopicalLink(owner, answer string) bool {
	oIDs := openIntentTargetIDs(owner)
	aIDs := openIntentTargetIDs(answer)
	for id := range oIDs {
		if aIDs[id] {
			return true
		}
	}
	// Answer names a target while also carrying complaint tokens.
	oTokens := openIntentContentTokens(owner)
	aLow := strings.ToLower(answer)
	hits := 0
	for _, tok := range oTokens {
		if openIntentTokenIn(tok, aLow) {
			hits++
		}
	}
	// One strong content hit is enough when answer has product evidence
	// (caller already checked evidence); two hits for weaker short tokens.
	if hits >= 2 {
		return true
	}
	if hits >= 1 {
		// Prefer longer stems (jiggl*, thrash, layout, re-render…).
		for _, tok := range oTokens {
			if len(tok) >= 5 && openIntentTokenIn(tok, aLow) {
				return true
			}
		}
	}
	// Owner question without shared tokens but answer explicitly quotes the
	// complaint class with evidence (e.g. "text jiggle" for pixel/jiggle Q)
	// — covered by stem overlap above. If owner mentioned no long tokens
	// and no target, require ≥1 token hit of len≥4.
	if hits >= 1 {
		return true
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

var openIntentStop = map[string]bool{
	"that": true, "this": true, "with": true, "from": true, "have": true,
	"what": true, "when": true, "where": true, "which": true, "while": true,
	"about": true, "after": true, "before": true, "being": true, "there": true,
	"their": true, "would": true, "could": true, "should": true, "doing": true,
	"does": true, "didn": true, "isn": true, "aren": true, "wasn": true,
	"were": true, "been": true, "will": true, "just": true, "like": true,
	"some": true, "them": true, "then": true, "than": true, "also": true,
	"into": true, "over": true, "under": true, "your": true, "youre": true,
	"page": true, "text": true, "time": true, "make": true, "need": true,
	"want": true, "please": true, "still": true, "really": true, "constantly": true,
	"affecting": true, "subtle": true, "all": true, "the": true, "and": true,
	"for": true, "not": true, "but": true, "are": true, "why": true, "how": true,
	"its": true, "it": true, "or": true, "so": true, "to": true, "of": true,
	"in": true, "on": true, "a": true, "an": true, "is": true, "by": true,
	"up": true, "down": true, "way": true, "whats": true,
}

func openIntentContentTokens(s string) []string {
	low := strings.ToLower(s)
	var b strings.Builder
	var toks []string
	flush := func() {
		tok := b.String()
		b.Reset()
		if len(tok) < 4 || openIntentStop[tok] {
			return
		}
		// Digits-only noise.
		allDigit := true
		for _, r := range tok {
			if !unicode.IsDigit(r) {
				allDigit = false
				break
			}
		}
		if allDigit {
			return
		}
		toks = append(toks, tok)
	}
	for _, r := range low {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else if b.Len() > 0 {
			flush()
		}
	}
	if b.Len() > 0 {
		flush()
	}
	return toks
}

func openIntentTokenIn(tok, hayLower string) bool {
	if tok == "" || hayLower == "" {
		return false
	}
	if strings.Contains(hayLower, tok) {
		return true
	}
	// Crude stem: jiggling ↔ jiggle / thrashing ↔ thrash.
	if len(tok) >= 6 {
		stem := tok
		for _, suf := range []string{"ing", "ed", "es", "ly", "tion"} {
			if strings.HasSuffix(stem, suf) && len(stem)-len(suf) >= 4 {
				stem = stem[:len(stem)-len(suf)]
				break
			}
		}
		if len(stem) >= 4 && stem != tok && strings.Contains(hayLower, stem) {
			return true
		}
	}
	return false
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

// LoadOpenOwnerIntent reads state_dir/chatlog/<overseer>.jsonl and extracts
// recoverable open owner intent. Missing chatlog → residual no_chatlog.
// Loads user turns plus evidence-shaped assistant excerpts for 🎯T344 / 🎯T477 /
// 🎯T512 disposition (product evidence, explanation answers, fleet ops).
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
	turns, err := loadOpenIntentDialogue(path, DefaultOpenIntentLookback, DefaultOpenIntentAssistantCap)
	if err != nil {
		return OpenOwnerIntent{Residual: ResidualNoChatlog}
	}
	if len(turns) == 0 {
		// Distinguish missing file vs empty / assistant-only.
		if _, statErr := os.Stat(path); statErr != nil {
			return OpenOwnerIntent{Residual: ResidualNoChatlog}
		}
		return OpenOwnerIntent{Residual: ResidualNoUserTurns}
	}
	return ExtractOpenOwnerIntent(turns)
}

// loadOpenIntentDialogue streams chatlog JSONL into user turns + evidence-shaped
// assistant excerpts (text and tool_use attestation/send bodies). Caps user
// lookback and assistant evidence count so assistant-heavy logs stay bounded.
func loadOpenIntentDialogue(path string, maxUser, maxAssistant int) ([]OwnerIntentTurn, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
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
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var users []OwnerIntentTurn
	// Assistants kept after the oldest retained user (ring by user count).
	type asst struct {
		turn      OwnerIntentTurn
		afterUser int // index in users at append time (user count)
	}
	var assts []asst

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		role, text, ts, ok := parseOpenIntentChatlogLine(line)
		if !ok || text == "" {
			continue
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
			// and fleet-ops completion prose/tool payloads (T512). Dropping any
			// of those re-fires an already-closed owner turn on every bounce.
			if !hasOpenIntentProductEvidence(text) && !openIntentEvidenceCandidate(text) &&
				!openIntentAnswerShaped(text) && !openIntentFleetOpsEvidence(text) {
				continue
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
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, nil
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
	return out, nil
}

// openIntentEvidenceCandidate is a cheaper pre-filter before full product
// evidence check — catches achieve/SHA-ish tool payloads worth retaining.
func openIntentEvidenceCandidate(text string) bool {
	low := strings.ToLower(text)
	if commitSHARe.MatchString(text) {
		return true
	}
	if openIntentTargetIDRe.MatchString(text) &&
		(strings.Contains(low, "achiev") || strings.Contains(low, "attestation") ||
			strings.Contains(low, "pass") || strings.Contains(low, "fix(") ||
			strings.Contains(low, "oracle") || strings.Contains(low, "hermetic")) {
		return true
	}
	if strings.Contains(low, "attestation") {
		return true
	}
	return false
}

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
			// themselves the completion act — keep a synthetic excerpt that
			// carries the tool marker plus the target name (flat alone is just
			// "jevons-po" and would not pass openIntentFleetOpsEvidence).
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
	if op, ok := input["op"].(string); ok && strings.EqualFold(op, "achieve") {
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

// isOpenIntentHarness matches harness/system injects that are not owner work.
func isOpenIntentHarness(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return true
	}
	// 🎯T362: client protocol control frames ({"type":"ux_state",…}) that
	// leaked into the owner chatlog are machine wire, never an instruction.
	// Resuming one re-fires composer telemetry as the open mission after
	// every restart — the exact spam this filter stops.
	if isOpenIntentProtocolJSON(t) {
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
	// 🎯T512: short fleet directives ("Kill jevons-po") are work orders, not acks.
	if len(ownerFleetDirectiveTargets(t)) > 0 {
		return false
	}
	low := strings.ToLower(t)
	// Imperative / work markers even in short messages.
	workHints := []string{
		"fix ", "file ", "spawn", "shove", "write ", "update ", "implement",
		"document", "add ", "remove ", "delete ", "restart", "deploy", "merge ",
		"commit", "test ", "please ", "need you", "do this", "make sure",
		"kill ", "stop ", "park ", "reap ",
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

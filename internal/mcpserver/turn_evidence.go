// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T387 — a spawned worker's opening brief is confirmed from evidence
// about the AGENT, never from evidence about the send call.
//
// 🎯T305 does cover this path: agents.go calls deliverStartPrompt, and a
// failure there stops the process and errors the tool. The predicate was
// the lie. ConfirmSendBeganTurn is handed a status string, and
// deliverToSender sets that string to "sent" exactly when proc.Send()
// returned nil. "Sent" is a statement about the send CALL — the keystrokes
// left, nothing refused them — and the daemon reported prompt_delivered=true
// on that basis alone, having never once looked at the agent.
//
// Four fleet spawns on 2026-08-09 returned prompt_delivered=true and ran
// nothing whatsoever: jv-t375, jv-t387, and claudia-po's two. Their session
// transcripts were not late, they did not exist; one was finally born nine
// minutes later when a human repressured the worker by hand.
//
// The damage is worse than a slow start, because every mechanism that would
// re-cover the work reads the phantom seat as success: the row stays bound
// to its target so 🎯T222 refuses a second implementer, the frontier shows
// the leaf consumed so 🎯T155 will not re-spawn it, and the sentinel filters
// the row as ordinary idle residue. Work is silently dropped, not delayed,
// and the only reason it surfaced at all was a PO reading session files by
// hand.
//
// WHAT COUNTS AS EVIDENCE. Not "did the send call succeed" — necessary,
// never sufficient — but "did this agent subsequently do anything". Which
// observation answers that depends on what the backend actually offers, so
// the rule is written against the surface rather than against a provider
// name (acceptance 3 — the fleet is mixed):
//
//   - Durable-transcript backends (Claude-shaped: claudia sets JSONLPath and
//     tails it) must show the transcript GROW past its pre-send size. The
//     agent's own submitted message is what appends. Published events are
//     deliberately NOT accepted here: claudia's tailJSONL reads the file from
//     byte zero on attach, so a resumed agent republishes its entire history
//     as events, and accepting those would confirm a turn from a conversation
//     that ended yesterday.
//   - Live-stream backends (Grok over ACP, which claudia leaves with an empty
//     JSONLPath and TailJSONL false) must publish a session event. There is
//     no replay on these; the stream carries live activity only.
//
// DIRECTION OF FAILURE, deliberately chosen. The old rule inferred SUCCESS
// from the absence of an error, and its failure mode is work that vanishes
// with a green tool result. This rule infers FAILURE from the absence of
// evidence, and its failure mode is a loud error on a seat that is then torn
// down — leaving the leaf unconsumed for 🎯T155 and the target unengaged for
// 🎯T222. Over-strictness costs a re-spawn; over-permissiveness costs the
// work. That asymmetry is the whole argument for the direction, and it is
// also why the over-broad mutation still has to be proved against: a
// confirmation nothing can satisfy strands the entire fleet.

// 🎯T416 — on the SEND path, evidence about the agent is not enough either.
// It has to be evidence about THIS MESSAGE.
//
// Two live findings force the distinction, both from 2026-08-10 (target
// context has the full specimen):
//
//   - DELIVERY IS OFF BY ONE. A send's Enter submits whatever the composer
//     already held, and the payload just pasted stays behind. claudia-po's
//     session gained one 8965-byte user message that was [fleet brief] + [a
//     message sent at 07:34] + [a message sent at 07:42], concatenated with no
//     separator — S3's send is what delivered S2. So the transcript grows
//     promptly and healthily while the payload the caller just handed us is
//     still sitting unsubmitted. Growth-based confirmation does not merely
//     miss this: it actively confirms the wrong message, and it would have
//     reported the send at 07:42:46 begun on the strength of the arrival of
//     two earlier ones.
//
//   - RAW FILE TEXT IS NOT THE TRANSCRIPT. Grepping the session file for the
//     payload finds it whether it was delivered or merely quoted — an agent
//     investigating this bug captured its own composer and the capture landed
//     in its transcript as a tool_result inside a user-role record. That is
//     how a real loss was retracted as a phantom. The match therefore runs at
//     user-MESSAGE level, over authored content only: a plain string content,
//     or text blocks. tool_result blocks are pane captures, command output and
//     file reads — never the agent being spoken to — and are excluded.
//
// PayloadSeen is the only positive that means "this payload became a turn".
// The other two mean "the agent did something", which is the right question
// for a fresh spawn (🎯T387, empty composer, nothing to confuse it with) and
// the wrong question for a send into a session that already has a backlog.
//
// AND ONE POSITIVE TEST FOR THE OTHER DIRECTION. Every instrument above is a
// failure to observe: the payload did not appear, which is consistent with
// born-stuck, with a mid-turn read (🎯T417), and with a slow disk. There is
// exactly one cheap observation that asserts born-stuck rather than failing to
// deny it, and we already had it and were not using it — a session's JSONL is
// created by its FIRST SUBMIT, so a registry-named session with no transcript
// file anywhere has never begun a turn at all. It requires no tmux, no pane,
// and no provider.
//
// It is recorded as TranscriptAbsent and used WITH PayloadSeen, never instead
// of it: file-exists says nothing whatever about WHICH message landed, which is
// the mistake that retracted a real loss as a phantom. The pair is what
// separates the two failure shapes a single instrument cannot:
//
//	file absent            ⇒ born-stuck, the whole backlog unsubmitted
//	file present, no match ⇒ 🎯T417 mid-turn false negative, or genuinely lost
//
// Named first by this target's worker at 08:56Z for the overseer's own rotated
// session 795525fb — registry row, live pane, no file on disk — and confirmed
// independently by the overseer on claudia-po's ab2326d3 in another repo, whose
// JSONL appeared at the instant a hand-pressed Enter created it.

// TurnEvidence is what the daemon observed OF THE AGENT after handing it
// text. Every field records something the agent itself did; none of them
// can be satisfied by a send call that merely returned nil.
type TurnEvidence struct {
	// ConversationGrew: the durable transcript gained bytes after the send.
	ConversationGrew bool
	// SessionEvent: the agent published a live session event after the send.
	SessionEvent bool
	// PayloadSeen: the durable transcript gained a USER MESSAGE carrying this
	// very payload (🎯T416). The only evidence that identifies the message
	// rather than the agent.
	PayloadSeen bool
	// Observed: an instrument existed at all — there was a live process to
	// watch. False means nothing was measured, which is NOT the same as
	// measuring nothing, and must never be read as a stuck paste (🎯T416).
	Observed bool
	// Durable: the agent keeps a transcript this daemon can read, which decides
	// what counts as proof. Recorded by the instrument that looked rather than
	// asked separately, because a second lookup is a second answer waiting to
	// disagree with the first — the shape of the whole defect.
	Durable bool
	// TranscriptAbsent: the agent keeps a durable transcript and there is no
	// such file at all. A POSITIVE born-stuck finding rather than a failure to
	// observe (🎯T416 clause 9 instrument B): the file is created by the first
	// submit, so its absence proves no turn has ever begun in this session —
	// and therefore that no turn can be in flight for the message to be
	// waiting behind.
	TranscriptAbsent bool
	// Detail names what was, or was not, seen — it becomes the operator's
	// account of the failure, so it carries the path and the window.
	Detail string
}

// Positive reports whether anything at all was observed of the agent.
func (e TurnEvidence) Positive() bool { return e.ConversationGrew || e.SessionEvent || e.PayloadSeen }

// ConfirmTurnBegan decides whether an opening brief actually began a turn.
//
// The send outcome is checked first and is NECESSARY — a refused or merely
// queued send cannot have begun a turn — but passing it only earns the right
// to look at the agent. Evidence about the agent decides.
func ConfirmTurnBegan(status string, sendErr error, ev TurnEvidence) error {
	if err := ConfirmSendBeganTurn(status, sendErr); err != nil {
		return err
	}
	if ev.Positive() {
		return nil
	}
	detail := strings.TrimSpace(ev.Detail)
	if detail == "" {
		detail = "no transcript growth and no session event"
	}
	return fmt.Errorf(
		"turn not begun: send reported %s but the agent did nothing — %s "+
			"(a send that returns without error is not evidence the agent began a turn)",
		status, detail)
}

// defaultTurnConfirmWindow bounds how long a spawn waits for the agent to
// show a sign of life before declaring the turn unbegun.
//
// This is not a race-tuning knob, and lengthening it fixes nothing. Healthy
// first evidence is prompt: a Claude-shaped agent's own submitted message is
// appended to its transcript as the turn starts, and a Grok ACP agent streams
// within a second or two. The failure this must catch is not a slow start but
// a total absence — nine minutes to the first byte, and only then because a
// human intervened. The window sits an order of magnitude above healthy
// latency and two below the failure, and a healthy spawn never waits it out:
// the watch returns the instant the first evidence lands.
const defaultTurnConfirmWindow = 45 * time.Second

// turnEvidencePoll is how often the transcript is re-stat'd while waiting.
// Session events do not wait on it; they wake the watch directly.
const turnEvidencePoll = 50 * time.Millisecond

// TurnConfirmWindowEnv overrides defaultTurnConfirmWindow with a Go duration.
// Present for operators and for oracles that need the failure path to resolve
// in test time; an unparseable or non-positive value keeps the default rather
// than disabling the confirmation.
const TurnConfirmWindowEnv = "JEVONS_TURN_CONFIRM_WINDOW"

func turnConfirmWindow() time.Duration {
	raw := strings.TrimSpace(os.Getenv(TurnConfirmWindowEnv))
	if raw == "" {
		return defaultTurnConfirmWindow
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		slog.Warn("ignoring unusable turn-confirm window",
			"component", "turn_evidence", "env", TurnConfirmWindowEnv,
			"value", raw, "using", defaultTurnConfirmWindow)
		return defaultTurnConfirmWindow
	}
	return d
}

// turnObserver is the part of a live agent process that reports what the
// AGENT did — as distinct from agentSender, which reports what the send call
// did. *claudia.Agent satisfies it; the split is the point of the target.
type turnObserver interface {
	SubscribeEvents(fn claudia.EventFunc) int64
	UnsubscribeEvents(token int64)
	JSONLPath() string
	Alive() bool
}

// turnWatch is an observation opened BEFORE the send and awaited after it,
// so "the transcript grew" is measured against a pre-send baseline instead of
// against whatever an earlier session happened to leave on disk.
type turnWatch func() TurnEvidence

// turnWitness opens a watch on one agent for one payload. Nil — the product
// path — observes the live claudia process from the registry. Test seam
// (SetTurnWitness), matching the senderResolver pattern. The payload is part
// of the seam because 🎯T416's evidence is about the message, not the agent.
type turnWitness func(name, payload string) turnWatch

// SetTurnWitness overrides turn-evidence observation. Test seam.
func (s *Server) SetTurnWitness(fn turnWitness) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observeTurnWitness = fn
}

// watchAgentTurn opens the observation used to confirm the next start prompt
// to name. Call it before the send; call the returned watch after. The spawn
// path identifies its prompt by agent activity alone (🎯T387): the composer is
// empty at that point, so there is no earlier backlog to confuse growth with.
func (s *Server) watchAgentTurn(name string) turnWatch {
	return s.watchAgentTurnFor(name, "")
}

// watchAgentTurnFor is the same observation told what to look for. A non-empty
// payload asks the watch to recognise THIS message in the transcript rather
// than settling for evidence that the agent stirred (🎯T416).
func (s *Server) watchAgentTurnFor(name, payload string) turnWatch {
	s.mu.Lock()
	witness := s.observeTurnWitness
	s.mu.Unlock()
	if witness != nil {
		return witness(name, payload)
	}
	var obs turnObserver
	if s.registry != nil {
		if proc := s.registry.Get(name); proc != nil {
			obs = proc
		}
	}
	return observeTurnFor(obs, payload, turnConfirmWindow())
}

// observeTurn is the product watch for the spawn path: agent activity only.
func observeTurn(obs turnObserver, window time.Duration) turnWatch {
	return observeTurnFor(obs, "", window)
}

// observeTurnFor is the product watch. It snapshots the transcript now and,
// when awaited, blocks until the payload appears (or, with no payload to look
// for, until the agent shows any sign of life), the agent dies, or the window
// closes.
func observeTurnFor(obs turnObserver, payload string, window time.Duration) turnWatch {
	if obs == nil {
		return func() TurnEvidence {
			return TurnEvidence{Detail: "no live agent process to observe"}
		}
	}

	path := strings.TrimSpace(obs.JSONLPath())
	baseline, hadTranscript := transcriptSize(path)
	needle := ""
	if path != "" {
		needle = payloadNeedle(payload)
	}

	// Live-stream backends only: see the file header on why a durable
	// transcript disqualifies published events as evidence.
	var seen chan struct{}
	var token int64
	if path == "" {
		seen = make(chan struct{}, 1)
		token = obs.SubscribeEvents(func(claudia.Event) {
			select {
			case seen <- struct{}{}:
			default:
			}
		})
	}

	// Everything this watch returns was measured — obs is non-nil past the
	// guard above — and every answer carries the same account of what kind of
	// backend was watched. Stamping both here rather than at each return is
	// deliberate: an answer that forgot to say it was observed reads as "no
	// instrument ran", which is the one thing that must never be guessed.
	measured := func(e TurnEvidence) TurnEvidence {
		e.Observed = true
		e.Durable = path != ""
		return e
	}

	return func() TurnEvidence {
		if seen != nil {
			defer obs.UnsubscribeEvents(token)
		}
		deadline := time.Now().Add(window)
		grew := false
		for {
			if path != "" {
				if size, ok := transcriptSize(path); ok && (!hadTranscript || size > baseline) {
					if needle == "" {
						return measured(TurnEvidence{
							ConversationGrew: true,
							Detail: fmt.Sprintf("transcript %s grew %d→%d bytes",
								path, baseline, size),
						})
					}
					// Growth is the cue to look, never the answer: the bytes
					// that just arrived are as likely to be an earlier stuck
					// payload finally submitted by this send's Enter.
					grew = true
					if transcriptCarriesPayload(path, baseline, hadTranscript, needle) {
						return measured(TurnEvidence{
							PayloadSeen: true,
							Detail:      fmt.Sprintf("transcript %s gained a user message carrying this payload", path),
						})
					}
				}
			}
			if !obs.Alive() {
				return measured(TurnEvidence{
					TranscriptAbsent: transcriptMissing(path),
					Detail:           "the agent process exited before it did anything",
				})
			}
			if time.Now().After(deadline) {
				return measured(TurnEvidence{
					TranscriptAbsent: transcriptMissing(path),
					Detail:           describePayloadAbsent(path, hadTranscript, baseline, grew, needle != "", window),
				})
			}
			if seen != nil {
				select {
				case <-seen:
					return measured(TurnEvidence{SessionEvent: true, Detail: "the agent published a session event"})
				case <-time.After(turnEvidencePoll):
				}
				continue
			}
			time.Sleep(turnEvidencePoll)
		}
	}
}

// payloadNeedleLen bounds the needle in runes. The daemon prepends the fleet
// standing brief to an agent's first send, so a multi-KB payload is mostly
// boilerplate that every other agent also received; the distinguishing part is
// the caller's own text at the END. Taking the tail therefore identifies the
// message rather than the brief, and keeps the scan cheap.
const payloadNeedleLen = 512

// payloadNeedle reduces a payload to what will be looked for in the
// transcript. Empty when there is nothing distinctive enough to recognise —
// which disables payload matching rather than asserting a match on noise.
func payloadNeedle(payload string) string {
	norm := normalizeForMatch(payload)
	if len([]rune(norm)) < 16 {
		// Too short to identify: "ok", "continue", a bare ack. Matching on
		// these would confirm from an unrelated message that happens to
		// contain the word.
		return ""
	}
	r := []rune(norm)
	if len(r) > payloadNeedleLen {
		r = r[len(r)-payloadNeedleLen:]
	}
	return string(r)
}

// normalizeForMatch collapses whitespace so a payload still matches after the
// CLI has re-wrapped or re-indented it. Both sides of the comparison run
// through this; nothing else is altered, because anything more aggressive
// starts matching messages that merely resemble the payload.
func normalizeForMatch(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// transcriptCarriesPayload reports whether the region appended since the send
// contains a user message carrying the payload.
//
// It reads from the pre-send baseline so an earlier occurrence of the same
// text — a resend of a message that DID land before — cannot confirm this one.
func transcriptCarriesPayload(path string, baseline int64, hadTranscript bool, needle string) bool {
	from := baseline
	if !hadTranscript {
		from = 0
	}
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	if from > 0 {
		if _, err := f.Seek(from, io.SeekStart); err != nil {
			return false
		}
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), transcriptScanMax)
	first := true
	for sc.Scan() {
		line := sc.Bytes()
		if first && from > 0 {
			// The baseline may have landed mid-record if a write was in
			// flight. That partial line is an earlier record, never ours.
			first = false
			if !json.Valid(line) {
				continue
			}
		}
		first = false
		if text, ok := authoredUserText(line); ok {
			if strings.Contains(normalizeForMatch(text), needle) {
				return true
			}
		}
	}
	return false
}

// transcriptScanMax bounds a single transcript record. Records carrying large
// tool results run big; a record longer than this is not a user message we
// need to read, and refusing to buffer it keeps a runaway line from being an
// allocation hazard.
const transcriptScanMax = 8 * 1024 * 1024

// authoredUserText extracts what was actually SAID to the agent in one
// transcript record, and reports whether the record is such a message at all.
//
// This is the distinction that clause 1 turns on. A user-role record can carry
// two very different things: text the sender authored, and tool_result blocks
// holding whatever the agent's own tools returned — pane captures, file reads,
// command output. An agent investigating a stuck send captures its own
// composer, and the unsubmitted payload lands in its transcript inside a
// tool_result. A raw file grep, or any check that reads a record's bytes
// without asking which part of it was authored, calls that delivery. It is not
// delivery; it is the agent looking at the message it never received.
func authoredUserText(line []byte) (string, bool) {
	var rec struct {
		IsSidechain bool `json:"isSidechain"`
		Message     *struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(line, &rec); err != nil || rec.Message == nil {
		return "", false
	}
	if rec.Message.Role != "user" {
		return "", false
	}
	if rec.IsSidechain {
		// A subagent's own conversation, not the agent we addressed.
		return "", false
	}
	var asString string
	if err := json.Unmarshal(rec.Message.Content, &asString); err == nil {
		return asString, true
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(rec.Message.Content, &blocks); err != nil {
		return "", false
	}
	var b strings.Builder
	for _, blk := range blocks {
		if blk.Type == "text" {
			b.WriteString(blk.Text)
			b.WriteString("\n")
		}
	}
	if b.Len() == 0 {
		return "", false
	}
	return b.String(), true
}

// releaseUnbriefedSeat tears down a seat whose opening brief could not be
// confirmed, and reports whether the registry row was retired.
//
// Stopping the process is not enough on its own. Engagement is decided from
// registry rows rather than from liveness (workAgentsEngagedOnTarget reads
// registry.List), so a stopped-but-registered seat keeps 🎯T222 refusing a
// second implementer on the target and keeps the leaf looking consumed to
// 🎯T155 — which is precisely how a dead spawn swallowed the work instead of
// merely delaying it.
//
// existed says whether the caller found the seat already registered. A seat
// that predates this call is stopped but kept: failing to re-brief an
// established agent must not delete it.
func (s *Server) releaseUnbriefedSeat(name string, existed bool) bool {
	if s == nil || s.registry == nil || strings.TrimSpace(name) == "" {
		return false
	}
	s.registry.Stop(name)
	if existed {
		return false
	}
	if err := s.registry.Remove(name); err != nil {
		slog.Warn("unbriefed seat left registered after failed opening brief",
			"component", compAgentLifecycle, "name", name, "err", err)
		return false
	}
	s.clearAgentTurnBegan(name)
	return true
}

// transcriptSize reports the size of a durable transcript, and whether one
// exists. An empty path (live-stream backend) is reported as absent.
func transcriptSize(path string) (int64, bool) {
	if strings.TrimSpace(path) == "" {
		return 0, false
	}
	fi, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	return fi.Size(), true
}

// transcriptMissing reports the born-stuck positive: this agent keeps a durable
// transcript, and no such file exists.
//
// Read the two guards together. An empty path is a live-stream backend, which
// has no transcript to be missing — reporting absence there would turn every
// healthy Grok send into a born-stuck finding. A non-empty path with no file is
// the real thing: the session was named, the pane is up, and nothing has ever
// been submitted into it.
func transcriptMissing(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, exists := transcriptSize(path)
	return !exists
}

// describePayloadAbsent renders the operator-facing account of a window that
// closed without the payload appearing. When the transcript moved but the
// payload never showed, saying so is the whole diagnosis: the session is
// healthy and busy, and this message is the one that did not land — which is
// exactly the case a growth-based check reported as success.
func describePayloadAbsent(path string, hadTranscript bool, baseline int64, grew, wanted bool, window time.Duration) string {
	if !wanted {
		return describeNoEvidence(path, hadTranscript, baseline, window)
	}
	if grew {
		return fmt.Sprintf(
			"transcript %s grew within %s but never gained a user message carrying this payload "+
				"(the growth was other traffic — an earlier stuck send, or the turn already running)",
			path, window)
	}
	return describeNoEvidence(path, hadTranscript, baseline, window)
}

// describeNoEvidence renders the operator-facing account of a window that
// closed with nothing observed. It names the transcript because "the file was
// never created" and "the file exists and did not move" are different
// diagnoses and the reader has to be able to tell them apart.
func describeNoEvidence(path string, hadTranscript bool, baseline int64, window time.Duration) string {
	switch {
	case path == "":
		return fmt.Sprintf("no session event within %s", window)
	case !hadTranscript:
		return fmt.Sprintf("no transcript was ever created at %s within %s", path, window)
	default:
		return fmt.Sprintf("transcript %s still %d bytes after %s", path, baseline, window)
	}
}

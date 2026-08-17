// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/marcelocantos/claudia"
	"github.com/marcelocantos/jevons/internal/fleetlog"
	"github.com/marcelocantos/jevons/internal/turnev"
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
	// very payload (🎯T416). The strongest evidence there is — it identifies
	// the message rather than the agent, and says a turn began on it.
	PayloadSeen bool
	// PayloadEnteredTurn: the receiver's own queue records show this payload
	// leaving the queue into a turn — a dequeue/remove record, or the
	// queued_command attachment the CLI writes when it drains one.
	//
	// It exists because PayloadSeen has a false negative and it is the ORDINARY
	// case, not an edge: a message accepted behind a live turn is replayed into
	// that turn as an attachment and NEVER becomes a user message, so reading
	// user messages only reports absent for a message the receiver already has.
	// The overseer hit this against this target's own worker at 09:31:24Z and
	// the daemon repeated it twice, both times 21 seconds after the queue had
	// already drained the payload into the running turn.
	PayloadEnteredTurn bool
	// PayloadQueued: an enqueue record carries the payload. The receiver has
	// accepted it behind a live turn and not yet drained it — the legitimate
	// third outcome, and now a POSITIVE finding rather than an inference from
	// this daemon's own memory of what the agent was doing.
	PayloadQueued bool
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

// Positive reports whether the observation settled anything.
//
// The two queue findings count. A brief the receiver has enqueued or drained
// into its turn is not a dropped brief, and 🎯T387's whole purpose is to catch
// dropped briefs — refusing a seat whose message the receiver demonstrably
// holds is the over-strictness clause 9 pins as its own mutant. What must not
// count is the absence of any of them.
func (e TurnEvidence) Positive() bool {
	return e.ConversationGrew || e.SessionEvent || e.PayloadSeen ||
		e.PayloadEnteredTurn || e.PayloadQueued
}

// ConfirmTurnBegan decides whether an opening brief actually began a turn.
//
// The send outcome is checked first and is NECESSARY — a refused or merely
// queued send cannot have begun a turn — but passing it only earns the right
// to look at the agent. Evidence about the agent decides.
//
// 🎯T429 — WITH ONE EXCEPTION, AND IT IS THE ONE THIS TARGET IS ABOUT. A send
// error that merely failed to VERIFY a submission is not a refusal: claudia's
// submit loop returns `turn not submitted: composer state=…` from its reading of
// a captured frame, and the live specimens are payloads the receiver was already
// working from. Letting that short-circuit the evidence here would tear down a
// seat whose brief demonstrably landed — releaseUnbriefedSeat stops the process
// AND removes the registry row — so an unverified submit gets the same treatment
// as a clean one: the agent's own records decide, and their absence is still a
// failure. A send that positively disproves delivery still short-circuits.
func ConfirmTurnBegan(status string, sendErr error, ev TurnEvidence) error {
	if sendErr != nil && !ClassifySendError(sendErr).DisprovesDelivery() && ev.Positive() {
		return nil
	}
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

// watchAgentTurn opens an observation that will settle for ANY sign of life
// from the agent. Call it before the send; call the returned watch after.
//
// NO PRODUCT PATH USES THIS ANY MORE (🎯T416 finding 3). It is the shape of
// clause 9's excluded mutant (i), and it is kept only so the suite can hold a
// growth-only watch next to a payload watch and prove they answer differently
// on a resumed session — the case in which the start path used it and got a
// confirmed delivery that had not happened. Reaching for it in product code is
// reaching for the mutant.
//
// The suite that keeps this sentence honest is
// TestGrowthWatchAndPayloadWatchDisagreeOnAResumedSeat in
// send_turn_begin_test.go. Named here because for one commit it was NOT true:
// the retention rationale was written while nothing called this at all, which
// is dead code wearing a justification. If that test goes, so does this.
func (s *Server) watchAgentTurn(name string) turnWatch {
	return s.watchAgentTurnFor(name, "")
}

// watchAgentTurnFor is the same observation told what to look for. A non-empty
// payload asks the watch to recognise THIS message in the transcript rather
// than settling for evidence that the agent stirred (🎯T416).
func (s *Server) watchAgentTurnFor(name, payload string) turnWatch {
	return s.watchAgentTurnForWindow(name, payload, turnConfirmWindow())
}

// watchAgentTurnForWindow is watchAgentTurnFor with the wait named by the
// caller. A caller that has ALREADY waited — jevons_event_push, which blocks on
// the target's reply — wants the records read now rather than waited for again
// (🎯T429 clause 5), and a window at or below zero makes the await a single
// immediate scan. This is not a knob on the confirmation window that 🎯T416
// clause 10 forbids widening: it only ever shortens, and the send path still
// passes turnConfirmWindow().
func (s *Server) watchAgentTurnForWindow(name, payload string, window time.Duration) turnWatch {
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
			if def := s.registry.Def(name); def != nil && !providerKeepsClaudeTranscript(def.Provider) {
				obs = liveStreamObserver{proc}
			}
		}
	}
	return observeTurnFor(obs, payload, window)
}

// providerKeepsClaudeTranscript reports whether this provider's backend
// actually maintains the durable ~/.claude/projects JSONL the file header
// calls a "durable-transcript backend". Only Claude-shaped agents do.
//
// 🎯T501 — claudia v0.23.0 nevertheless ADVERTISES a Claude-shaped
// JSONLPath for every provider: Start precomputes SessionJSONLPath for
// the session id before asking the backend, the codex and grok backends
// leave agentStart.JSONLPath empty (codex has no file transcript at all;
// grok's comment reads "Leave JSONLPath empty: Grok Session is not a
// Claude JSONL transcript"), and the caller's `if start.JSONLPath != ""`
// guard reads that emptiness as "no override" rather than as the claim it
// was. So a codex-app-server agent reports a transcript path nothing will
// ever write, this watch took the durable branch on it, and every codex
// work-agent mint died at the 45s window as "no transcript was ever
// created at ~/.claude/projects/….jsonl" → unbriefed_seat (T433) — while
// the agent itself was live and its event stream was carrying the turn.
//
// The surface rule in the header stands: durable backends prove a turn
// from transcript records, live-stream backends from session events. This
// helper corrects which SURFACE the agent actually has, using the one
// fact the registry holds and claudia's Agent does not expose. When
// claudia stops advertising the phantom path, this collapses to a no-op:
// JSONLPath()=="" already takes the live-stream branch.
func providerKeepsClaudeTranscript(p claudia.Provider) bool {
	return p == "" || p == claudia.ProviderClaude
}

// liveStreamObserver hides the vestigial Claude-shaped JSONLPath so the
// watch treats the agent as the live-stream backend it is (🎯T501).
type liveStreamObserver struct{ turnObserver }

func (liveStreamObserver) JSONLPath() string { return "" }

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
	baseline, hadTranscript := turnev.Size(path)
	needle := ""
	if path != "" {
		needle = turnev.Needle(payload)
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
				if size, ok := turnev.Size(path); ok && (!hadTranscript || size > baseline) {
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
					if ev, ok := fateEvidence(path, baseline, hadTranscript, needle); ok {
						return measured(ev)
					}
				}
			}
			if !obs.Alive() {
				return measured(TurnEvidence{
					TranscriptAbsent: turnev.Missing(path),
					Detail:           "the agent process exited before it did anything",
				})
			}
			if time.Now().After(deadline) {
				// One last look before condemning it, because the queue
				// records are written by the receiver at its own pace and a
				// message can be accepted without the file growing in a way
				// this loop noticed. The whole region since the baseline is
				// re-read, so this is one extra scan and NOT a longer clock
				// (🎯T416 clause 10 forbids widening either window).
				if ev, ok := fateEvidence(path, baseline, hadTranscript, needle); ok {
					return measured(ev)
				}
				return measured(TurnEvidence{
					TranscriptAbsent: turnev.Missing(path),
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

// fateEvidence turns one scan of the receiver's transcript into the answer
// about THIS payload, and reports whether the scan settled anything.
//
// The three positives are separate fields rather than one enum because they
// are separate claims and the operator's account differs: a turn began on it,
// the receiver's queue handed it into a turn, or the receiver is holding it
// behind a live turn. A queued payload is deliberately settling — it stops the
// watch — because there is nothing further to wait for: the receiver has it,
// and waiting past that point only invents a failure out of a healthy send.
func fateEvidence(path string, baseline int64, hadTranscript bool, needle string) (TurnEvidence, bool) {
	switch turnev.Scan(path, baseline, hadTranscript, needle) {
	case turnev.FateUserMessage:
		return TurnEvidence{
			PayloadSeen: true,
			Detail:      fmt.Sprintf("transcript %s gained a user message carrying this payload", path),
		}, true
	case turnev.FateEnteredTurn:
		return TurnEvidence{
			PayloadEnteredTurn: true,
			Detail: fmt.Sprintf("transcript %s shows this payload leaving the receiver's queue into a turn "+
				"(a queued message is replayed as an attachment and never becomes a user message)", path),
		}, true
	case turnev.FateQueued:
		return TurnEvidence{
			PayloadQueued: true,
			Detail: fmt.Sprintf("transcript %s shows this payload enqueued behind a live turn "+
				"(accepted by the receiver, not yet begun)", path),
		}, true
	}
	return TurnEvidence{}, false
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
	// 🎯T435: the seat leaving the registry is accounted for. A seat retired
	// here never began a turn, so its row vanishing is exactly the kind of
	// diff a watcher would otherwise read as an agent lost mid-flight.
	if _, err := s.RemovalAccount().Remove(s.registry, name, fleetlog.Removal{
		Reason: fleetlog.ReasonUnbriefedSeat,
		Detail: "retired a seat whose opening brief never landed (🎯T433)",
	}); err != nil {
		slog.Warn("unbriefed seat left registered after failed opening brief",
			"component", compAgentLifecycle, "name", name, "err", err)
		return false
	}
	s.clearAgentTurnBegan(name)
	return true
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

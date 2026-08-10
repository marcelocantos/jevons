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

// TurnEvidence is what the daemon observed OF THE AGENT after handing it
// text. Every field records something the agent itself did; none of them
// can be satisfied by a send call that merely returned nil.
type TurnEvidence struct {
	// ConversationGrew: the durable transcript gained bytes after the send.
	ConversationGrew bool
	// SessionEvent: the agent published a live session event after the send.
	SessionEvent bool
	// Detail names what was, or was not, seen — it becomes the operator's
	// account of the failure, so it carries the path and the window.
	Detail string
}

// Positive reports whether anything at all was observed of the agent.
func (e TurnEvidence) Positive() bool { return e.ConversationGrew || e.SessionEvent }

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

// turnWitness opens a watch on one agent. Nil — the product path — observes
// the live claudia process from the registry. Test seam (SetTurnWitness),
// matching the senderResolver pattern.
type turnWitness func(name string) turnWatch

// SetTurnWitness overrides turn-evidence observation. Test seam.
func (s *Server) SetTurnWitness(fn turnWitness) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observeTurnWitness = fn
}

// watchAgentTurn opens the observation used to confirm the next send to name.
// Call it before the send; call the returned watch after.
func (s *Server) watchAgentTurn(name string) turnWatch {
	s.mu.Lock()
	witness := s.observeTurnWitness
	s.mu.Unlock()
	if witness != nil {
		return witness(name)
	}
	var obs turnObserver
	if s.registry != nil {
		if proc := s.registry.Get(name); proc != nil {
			obs = proc
		}
	}
	return observeTurn(obs, turnConfirmWindow())
}

// observeTurn is the product watch. It snapshots the transcript now and,
// when awaited, blocks until the agent shows a sign of life, dies, or the
// window closes.
func observeTurn(obs turnObserver, window time.Duration) turnWatch {
	if obs == nil {
		return func() TurnEvidence {
			return TurnEvidence{Detail: "no live agent process to observe"}
		}
	}

	path := strings.TrimSpace(obs.JSONLPath())
	baseline, hadTranscript := transcriptSize(path)

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

	return func() TurnEvidence {
		if seen != nil {
			defer obs.UnsubscribeEvents(token)
		}
		deadline := time.Now().Add(window)
		for {
			if path != "" {
				if size, ok := transcriptSize(path); ok && (!hadTranscript || size > baseline) {
					return TurnEvidence{
						ConversationGrew: true,
						Detail: fmt.Sprintf("transcript %s grew %d→%d bytes",
							path, baseline, size),
					}
				}
			}
			if !obs.Alive() {
				return TurnEvidence{Detail: "the agent process exited before it did anything"}
			}
			if time.Now().After(deadline) {
				return TurnEvidence{Detail: describeNoEvidence(path, hadTranscript, baseline, window)}
			}
			if seen != nil {
				select {
				case <-seen:
					return TurnEvidence{SessionEvent: true, Detail: "the agent published a session event"}
				case <-time.After(turnEvidencePoll):
				}
				continue
			}
			time.Sleep(turnEvidencePoll)
		}
	}
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

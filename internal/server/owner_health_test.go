// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/chatlog"
	"github.com/marcelocantos/jevons/internal/converge"
)

// ownerHealthServer builds a hermetic server with a durable chatlog, one
// connected chat client, and a stubbed overseer delivery seam. No process, no
// provider, no daemon (🎯T355 hermetics).
func ownerHealthServer(t *testing.T, send func(string) error) (*Server, chan string) {
	t.Helper()
	dir := t.TempDir()
	s := New("test", dir)

	log, err := chatlog.Open(filepath.Join(dir, "chatlog", "jevons.jsonl"))
	if err != nil {
		t.Fatalf("open chatlog: %v", err)
	}
	t.Cleanup(func() { log.Close() })
	s.SetChatLog(log)

	frames := make(chan string, 64)
	s.mu.Lock()
	s.chatListeners = append(s.chatListeners, frames)
	s.notifySender = send
	s.mu.Unlock()

	// Tight bounds: the contract is the same, the patience is not.
	s.SetOwnerHealthBounds(converge.OwnerBounds{
		SendLand:    10 * time.Millisecond,
		ChromeTruth: 10 * time.Millisecond,
		Reply:       10 * time.Millisecond,
		ACPStall:    10 * time.Millisecond,
		UIHeartbeat: time.Hour,
	})
	return s, frames
}

// ownerSend replays what handleChat does with an accepted owner turn.
func ownerSend(t *testing.T, s *Server, text string) {
	t.Helper()
	echo := chatUserEcho(text)
	s.NoteOwnerSend(text, echo)
	s.BroadcastChat(echo)
	if err := s.SendToOverseer(userTurnPrefix + text); err != nil {
		t.Fatalf("SendToOverseer: %v", err)
	}
}

func drainFrames(frames chan string) []string {
	var out []string
	for {
		select {
		case f := <-frames:
			out = append(out, f)
		default:
			return out
		}
	}
}

func frameMatching(frames []string, pred func(map[string]any) bool) (map[string]any, bool) {
	for _, f := range frames {
		var m map[string]any
		if json.Unmarshal([]byte(f), &m) != nil {
			continue
		}
		if pred(m) {
			return m, true
		}
	}
	return nil, false
}

// A healthy owner turn — journaled, delivered, sealed — leaves no standing
// gap. The reconciler must not manufacture work on a working chat.
func TestOwnerHealthHealthyTurnLeavesNoGaps(t *testing.T) {
	s, _ := ownerHealthServer(t, func(string) error { return nil })
	ownerSend(t, s, "how is the fleet?")

	// Seal the turn the way a provider does.
	s.HandleAgentEvent(claudia.Event{Type: "system"})

	if got := s.ReconcileOwnerHealth(time.Now().Add(time.Minute)); len(got) == 0 {
		t.Fatal("no verdicts produced")
	}
	if gaps := s.OwnerHealthGaps(); len(gaps) != 0 {
		t.Fatalf("healthy turn left gaps: %+v", gaps)
	}
}

// The lying-spinner class: chrome says working, the server has no turn in
// flight. The recovery step is a level-truth sample, and the *next
// observation* — not the step — is what satisfies the gap.
func TestOwnerHealthClearsFalseWorkingChrome(t *testing.T) {
	s, frames := ownerHealthServer(t, func(string) error { return nil })

	// An owner send lights client chrome; the turn then vanishes without a
	// seal (the daemon-bounce / lost-session shape) so level truth is idle.
	ownerSend(t, s, "status please")
	s.mu.Lock()
	s.waiting = false
	s.overseerOwnerTurn = false
	s.mu.Unlock()
	drainFrames(frames)

	now := time.Now()
	// First pass stamps the mismatch edge; the grace has not elapsed.
	s.ReconcileOwnerHealth(now)
	if _, open := gapFor(s, converge.OwnerDimChromeTruthful); open {
		t.Fatal("chrome gap opened inside its grace window")
	}

	// Past the grace: the gap opens and the actuator publishes level truth.
	s.ReconcileOwnerHealth(now.Add(time.Second))
	frame, ok := frameMatching(drainFrames(frames), func(m map[string]any) bool {
		return m["type"] == "status" && m["state"] == "idle"
	})
	if !ok {
		t.Fatal("no idle level-truth frame published to clear false working chrome")
	}
	// 🎯T260: the client only lets an idle level clear an open owner turn
	// when the text reads as a settle/recovery.
	if text, _ := frame["text"].(string); !strings.Contains(text, "settled") {
		t.Errorf("idle frame text = %q, want settle wording the client accepts", text)
	}

	// The step alone did not satisfy anything — the next observation does.
	s.ReconcileOwnerHealth(now.Add(2 * time.Second))
	if g, open := gapFor(s, converge.OwnerDimChromeTruthful); open {
		t.Fatalf("chrome gap still standing after truth was published: %+v", g)
	}
	// The unanswered turn behind it is a separate dimension and stays open —
	// clearing the spinner is not answering the owner.
	if _, open := gapFor(s, converge.OwnerDimReplyOrResidual); !open {
		t.Error("clearing chrome silently closed the unanswered-turn gap")
	}
}

func gapFor(s *Server, dim converge.OwnerDimension) (converge.OwnerGap, bool) {
	for _, g := range s.OwnerHealthGaps() {
		if g.Dimension == dim {
			return g, true
		}
	}
	return converge.OwnerGap{}, false
}

// Delivery deferred: the owner's words are durable and still queued because
// the overseer is busy. That is a standing gap — the owner has no answer —
// but re-injecting would ask the question twice, so the step is skipped and
// the recovery is the drain itself.
func TestOwnerHealthQueuedTurnIsNotAskedTwice(t *testing.T) {
	var sent []string
	down := true
	s, _ := ownerHealthServer(t, func(text string) error {
		if down {
			return errors.New("prompt already in flight")
		}
		sent = append(sent, text)
		return nil
	})

	ownerSend(t, s, "ship the coach tab")
	if _, open := gapFor(s, converge.OwnerDimSendLanded); open {
		t.Fatal("gap opened before any reconcile")
	}

	now := time.Now()
	s.ReconcileOwnerHealth(now.Add(time.Second))
	g, open := gapFor(s, converge.OwnerDimSendLanded)
	if !open {
		t.Fatal("undelivered owner turn did not open a send-landed gap")
	}
	if g.Kind != converge.OwnerGapSendNotDelivered {
		t.Errorf("kind = %q, want %q", g.Kind, converge.OwnerGapSendNotDelivered)
	}
	if n := g.StepsByKind[converge.StepRequeueOwnerSend]; n != 0 {
		t.Errorf("re-injected a turn that was still queued (%d times)", n)
	}

	// The overseer comes back; the queued owner text lands exactly once.
	down = false
	s.drainOverseerNotes()
	if len(sent) != 1 {
		t.Fatalf("delivered %d prompts after recovery, want exactly 1: %v", len(sent), sent)
	}
	if !strings.Contains(sent[0], "ship the coach tab") {
		t.Errorf("delivered text = %q, want the owner's words", sent[0])
	}

	s.ReconcileOwnerHealth(now.Add(2 * time.Second))
	if _, open := gapFor(s, converge.OwnerDimSendLanded); open {
		t.Fatal("send-landed gap still standing after the turn was acked")
	}
}

// Delivery failure: the owner's words are durable but no longer anywhere in
// flight — dropped queue, lost session, bounce. The actuator re-injects them
// (🎯T328 owner-intent-resume generalized), and the observation that the
// prompt finally landed — not the step — closes the gap.
func TestOwnerHealthRequeuesLostOwnerTurn(t *testing.T) {
	var sent []string
	s, _ := ownerHealthServer(t, func(text string) error {
		sent = append(sent, text)
		return nil
	})

	// Accepted and journaled, then lost before delivery.
	echo := chatUserEcho("what is the frontier?")
	s.NoteOwnerSend("what is the frontier?", echo)
	s.BroadcastChat(echo)
	s.mu.Lock()
	s.notifyQueue = nil
	s.mu.Unlock()

	now := time.Now()
	s.ReconcileOwnerHealth(now.Add(time.Second))
	if len(sent) != 1 || !strings.Contains(sent[0], "what is the frontier?") {
		t.Fatalf("lost owner turn was not re-injected: %v", sent)
	}

	s.ReconcileOwnerHealth(now.Add(2 * time.Second))
	if _, open := gapFor(s, converge.OwnerDimSendLanded); open {
		t.Fatal("send-landed gap still standing after re-injection was acked")
	}
}

// One lost turn opens two dimensions — nothing acked it and nothing answered
// it — but the owner is asked their own question at most once per round.
func TestOwnerHealthDoesNotReinjectOncePerOpenDimension(t *testing.T) {
	var sent []string
	s, _ := ownerHealthServer(t, func(text string) error {
		sent = append(sent, text)
		return nil
	})

	echo := chatUserEcho("did that land?")
	s.NoteOwnerSend("did that land?", echo)
	s.BroadcastChat(echo)
	s.mu.Lock()
	s.notifyQueue = nil
	s.mu.Unlock()

	now := time.Now()
	s.ReconcileOwnerHealth(now.Add(time.Second))
	open := 0
	for _, dim := range []converge.OwnerDimension{converge.OwnerDimSendLanded, converge.OwnerDimReplyOrResidual} {
		if _, ok := gapFor(s, dim); ok {
			open++
		}
	}
	if open < 1 {
		t.Fatal("a lost owner turn opened no gap at all")
	}
	if len(sent) != 1 {
		t.Fatalf("re-injected the owner turn %d times in one round: %v", len(sent), sent)
	}
}

// An owner turn that produced neither a reply nor a residual is a gap even
// though nothing is in flight — the stall class a live-but-useless daemon
// hides. A named residual satisfies it; silence never does.
func TestOwnerHealthTurnStallSatisfiedOnlyByReplyOrResidual(t *testing.T) {
	s, _ := ownerHealthServer(t, func(string) error { return nil })
	ownerSend(t, s, "what happened to the fleet?")
	// Delivered, then the session evaporates without sealing.
	s.mu.Lock()
	s.waiting = false
	s.overseerOwnerTurn = false
	s.mu.Unlock()

	now := time.Now()
	s.ReconcileOwnerHealth(now.Add(time.Second))
	g, open := gapFor(s, converge.OwnerDimReplyOrResidual)
	if !open {
		t.Fatal("evaporated owner turn did not open a reply-or-residual gap")
	}
	if g.Kind != converge.OwnerGapTurnStall {
		t.Errorf("kind = %q, want %q", g.Kind, converge.OwnerGapTurnStall)
	}

	// Steps keep it standing; only an observation closes it.
	s.ReconcileOwnerHealth(now.Add(2 * time.Second))
	if _, open := gapFor(s, converge.OwnerDimReplyOrResidual); !open {
		t.Fatal("reply gap closed without a reply or a residual")
	}

	s.NoteOwnerResidual("overseer_unreachable")
	s.ReconcileOwnerHealth(now.Add(3 * time.Second))
	if _, open := gapFor(s, converge.OwnerDimReplyOrResidual); open {
		t.Fatal("named residual did not satisfy reply-or-residual")
	}
}

// When recovery does not take, the gap escalates: the owner's own chat is
// told that interaction is degraded rather than being left to guess. The
// stimulus is a client that stopped ticking — the coordinate step runs, the
// plant stays broken, and the ladder walks to the owner-visible rung.
func TestOwnerHealthEscalatesUnrecoveredGapToOwner(t *testing.T) {
	s, frames := ownerHealthServer(t, func(string) error { return nil })
	s.SetOwnerHealthBounds(converge.OwnerBounds{
		SendLand:    10 * time.Millisecond,
		ChromeTruth: 10 * time.Millisecond,
		Reply:       10 * time.Millisecond,
		ACPStall:    10 * time.Millisecond,
		UIHeartbeat: 10 * time.Millisecond,
	})
	s.NoteOwnerUIHeartbeat()
	s.ownerMu.Lock()
	s.ownerHealth.uiHeartbeatAt = time.Now().Add(-time.Hour) // main thread stopped
	s.ownerMu.Unlock()
	drainFrames(frames)

	now := time.Now()
	for i := 1; i <= 8; i++ {
		s.ReconcileOwnerHealth(now.Add(time.Duration(i) * time.Second))
		s.ownerMu.Lock()
		s.ownerHealth.uiHeartbeatAt = now.Add(-time.Hour) // still frozen
		s.ownerMu.Unlock()
	}

	alert, ok := frameMatching(drainFrames(frames), func(m map[string]any) bool {
		if m["type"] != "error" {
			return false
		}
		text, _ := m["error"].(string)
		return strings.Contains(text, "owner interaction degraded")
	})
	if !ok {
		t.Fatal("an unrecovered owner-interaction gap never reached the owner")
	}
	if text, _ := alert["error"].(string); !strings.Contains(text, "recovery steps") {
		t.Errorf("alert = %q, want it to name the recovery attempts", text)
	}

	// Escalating is noise, not satisfaction: the gap is still standing.
	if len(s.OwnerHealthGaps()) == 0 {
		t.Fatal("escalation emptied the gap set — a rung is not satisfaction")
	}
}

// A stalled prompt is unstuck only when one is genuinely in flight: with no
// process there is nothing to interrupt, and the step must not be spent.
func TestOwnerHealthACPUnstickRequiresPromptInFlight(t *testing.T) {
	s, _ := ownerHealthServer(t, func(string) error { return nil })
	err := ownerActuator{s}.Step(
		converge.OwnerGap{Dimension: converge.OwnerDimReplyOrResidual, Kind: converge.OwnerGapACPStall},
		converge.StepACPUnstick, time.Now())
	if !errors.Is(err, converge.ErrOwnerStepNotApplicable) {
		t.Fatalf("unstick with no in-flight prompt = %v, want not-applicable", err)
	}
}

// With nobody watching, chrome and interaction are out of scope — the
// reconciler must not broadcast recovery frames into an empty room.
func TestOwnerHealthNoClientScopesOutChrome(t *testing.T) {
	s, frames := ownerHealthServer(t, func(string) error { return nil })
	ownerSend(t, s, "hello")
	s.mu.Lock()
	s.chatListeners = nil
	s.waiting = false
	s.overseerOwnerTurn = false
	s.mu.Unlock()
	drainFrames(frames)

	now := time.Now()
	s.ReconcileOwnerHealth(now)
	s.ReconcileOwnerHealth(now.Add(time.Second))
	if _, open := gapFor(s, converge.OwnerDimChromeTruthful); open {
		t.Error("chrome gap opened with no connected client")
	}
	if _, open := gapFor(s, converge.OwnerDimInteractive); open {
		t.Error("interaction gap opened with no connected client")
	}
}

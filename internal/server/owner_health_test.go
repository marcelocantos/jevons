// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package server

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"

	"github.com/marcelocantos/jevons/internal/chatlog"
	"github.com/marcelocantos/jevons/internal/converge"
)

// ownerClock is the clock these oracles run on. Time moves when the test says
// so and at no other moment, which is the whole of 🎯T437: the fixture used to
// stamp its edges from the wall clock and then classify from a logical `now`,
// so on a loaded machine the real gap between two statements could exceed the
// bound under test and the verdict stopped being a function of the tree.
type ownerClock struct {
	mu sync.Mutex
	t  time.Time
}

// newOwnerClock starts at the wall clock so that server state stamped outside
// the owner-health subsystem (overseer progress, chatlog timestamps) stays in
// the same era; nothing here reads it as a duration.
func newOwnerClock() *ownerClock { return &ownerClock{t: time.Now()} }

func (c *ownerClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// Advance moves the clock and returns the new reading, so a reconcile pass
// reads `clk.Advance(d)` — one statement that says both when it happens and
// that time passed to get there.
func (c *ownerClock) Advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
	return c.t
}

// ownerHealthServer builds a hermetic server with a durable chatlog, one
// connected chat client, and a stubbed overseer delivery seam. No process, no
// provider, no daemon (🎯T355 hermetics).
//
// The bounds are the shipped ones. They used to be flattened to 10ms each so
// the suite would not sit through a 90-second reply grace, which cost twice:
// real elapsed time could cross them (🎯T437), and flattening also destroyed
// the proportions the contract is written in — a chrome grace covers a
// send→drain race, a reply bound covers a whole turn, and collapsing both to
// one value let dimensions open in an order the daemon would never produce.
// With a controlled clock the grace costs nothing to wait out, so there is no
// reason to test anything but the contract as shipped.
func ownerHealthServer(t *testing.T, send func(string) error) (*Server, chan string, *ownerClock) {
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

	clk := newOwnerClock()
	s.SetOwnerHealthClock(clk.Now)
	return s, frames, clk
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
	s, _, clk := ownerHealthServer(t, func(string) error { return nil })
	ownerSend(t, s, "how is the fleet?")

	// Seal the turn the way a provider does.
	s.HandleAgentEvent(claudia.Event{Type: "system"})

	if got := s.ReconcileOwnerHealth(clk.Advance(time.Minute)); len(got) == 0 {
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
	s, frames, clk := ownerHealthServer(t, func(string) error { return nil })

	// An owner send lights client chrome; the turn then vanishes without a
	// seal (the daemon-bounce / lost-session shape) so level truth is idle.
	ownerSend(t, s, "status please")
	s.mu.Lock()
	s.waiting = false
	s.overseerOwnerTurn = false
	s.mu.Unlock()
	drainFrames(frames)

	// First pass stamps the mismatch edge; the grace has not elapsed.
	s.ReconcileOwnerHealth(clk.Now())
	if _, open := gapFor(s, converge.OwnerDimChromeTruthful); open {
		t.Fatal("chrome gap opened inside its grace window")
	}

	// Past the chrome grace and still inside the reply bound, which is the
	// window this dimension owns: the gap opens and the actuator publishes
	// level truth. Both offsets are the shipped contract's, so the dimensions
	// open in the order the daemon would open them in.
	s.ReconcileOwnerHealth(clk.Advance(20 * time.Second))
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
	// Far enough on that the unanswered turn behind the spinner is past its
	// own bound, which is what the last assertion is about.
	s.ReconcileOwnerHealth(clk.Advance(80 * time.Second))
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
	s, _, clk := ownerHealthServer(t, func(text string) error {
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

	s.ReconcileOwnerHealth(clk.Advance(10 * time.Second))
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

	s.ReconcileOwnerHealth(clk.Advance(10 * time.Second))
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
	s, _, clk := ownerHealthServer(t, func(text string) error {
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

	s.ReconcileOwnerHealth(clk.Advance(10 * time.Second))
	if len(sent) != 1 || !strings.Contains(sent[0], "what is the frontier?") {
		t.Fatalf("lost owner turn was not re-injected: %v", sent)
	}

	s.ReconcileOwnerHealth(clk.Advance(10 * time.Second))
	if _, open := gapFor(s, converge.OwnerDimSendLanded); open {
		t.Fatal("send-landed gap still standing after re-injection was acked")
	}
}

// One lost turn opens two dimensions — nothing acked it and nothing answered
// it — but the owner is asked their own question at most once per round.
func TestOwnerHealthDoesNotReinjectOncePerOpenDimension(t *testing.T) {
	var sent []string
	s, _, clk := ownerHealthServer(t, func(text string) error {
		sent = append(sent, text)
		return nil
	})

	echo := chatUserEcho("did that land?")
	s.NoteOwnerSend("did that land?", echo)
	s.BroadcastChat(echo)
	s.mu.Lock()
	s.notifyQueue = nil
	s.mu.Unlock()

	// Past the reply bound as well as the send bound, so both dimensions are
	// genuinely standing when the dedupe is measured — that is the case the
	// dedupe exists for.
	s.ReconcileOwnerHealth(clk.Advance(100 * time.Second))
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
	s, _, clk := ownerHealthServer(t, func(string) error { return nil })
	ownerSend(t, s, "what happened to the fleet?")
	// Delivered, then the session evaporates without sealing.
	s.mu.Lock()
	s.waiting = false
	s.overseerOwnerTurn = false
	s.mu.Unlock()

	s.ReconcileOwnerHealth(clk.Advance(100 * time.Second))
	g, open := gapFor(s, converge.OwnerDimReplyOrResidual)
	if !open {
		t.Fatal("evaporated owner turn did not open a reply-or-residual gap")
	}
	if g.Kind != converge.OwnerGapTurnStall {
		t.Errorf("kind = %q, want %q", g.Kind, converge.OwnerGapTurnStall)
	}

	// Steps keep it standing; only an observation closes it.
	s.ReconcileOwnerHealth(clk.Advance(100 * time.Second))
	if _, open := gapFor(s, converge.OwnerDimReplyOrResidual); !open {
		t.Fatal("reply gap closed without a reply or a residual")
	}

	s.NoteOwnerResidual("overseer_unreachable")
	s.ReconcileOwnerHealth(clk.Advance(100 * time.Second))
	if _, open := gapFor(s, converge.OwnerDimReplyOrResidual); open {
		t.Fatal("named residual did not satisfy reply-or-residual")
	}
}

// When recovery does not take, the gap escalates: the owner's own chat is
// told that interaction is degraded rather than being left to guess. The
// stimulus is a client that stopped ticking — the coordinate step runs, the
// plant stays broken, and the ladder walks to the owner-visible rung.
func TestOwnerHealthEscalatesUnrecoveredGapToOwner(t *testing.T) {
	s, frames, clk := ownerHealthServer(t, func(string) error { return nil })
	s.NoteOwnerUIHeartbeat()
	s.ownerMu.Lock()
	s.ownerHealth.uiHeartbeatAt = clk.Now().Add(-time.Hour) // main thread stopped
	s.ownerMu.Unlock()
	drainFrames(frames)

	// Each pass is spaced by more than the step spacing this kind carries
	// (the heartbeat bound), so all eight are due and the ladder actually
	// walks: three coordinate steps, one overseer report, then the owner.
	for range 8 {
		now := clk.Advance(2 * time.Minute)
		s.ReconcileOwnerHealth(now)
		s.ownerMu.Lock()
		s.ownerHealth.uiHeartbeatAt = now.Add(-time.Hour) // still frozen
		s.ownerMu.Unlock()
	}

	alert, ok := frameMatching(drainFrames(frames), func(m map[string]any) bool {
		if m["type"] != "status" || m["owner_ux"] != "degraded" {
			return false
		}
		text, _ := m["text"].(string)
		return strings.Contains(text, "owner interaction degraded")
	})
	if !ok {
		t.Fatal("an unrecovered owner-interaction gap never reached the owner")
	}
	if text, _ := alert["text"].(string); !strings.Contains(text, "recovery steps") {
		t.Errorf("alert = %q, want it to name the recovery attempts", text)
	}

	// Escalating is noise, not satisfaction: the gap is still standing.
	if len(s.OwnerHealthGaps()) == 0 {
		t.Fatal("escalation emptied the gap set — a rung is not satisfaction")
	}
}

// 🎯T361: the client's own report of a blocked composer opens the UX-degrade
// dimension on a page that is still ticking. Without the reporter this level
// was always false and only a frozen main thread could ever be seen.
func TestOwnerHealthObservesClientReportedBlockedComposer(t *testing.T) {
	s, _, clk := ownerHealthServer(t, func(string) error { return nil })
	s.NoteOwnerComposerBlocked(true, "overseer_down")

	now := clk.Now()
	obs := s.ObserveOwnerInteraction(now)
	if !obs.ComposerBlocked {
		t.Fatal("client-reported blocked composer was not observed")
	}
	// A reporting client is a ticking client: the heartbeat must not be the
	// thing that opens the gap here.
	if obs.SinceUIHeartbeat > time.Minute {
		t.Fatalf("SinceUIHeartbeat = %v, want fresh from the report", obs.SinceUIHeartbeat)
	}

	s.ReconcileOwnerHealth(now)
	g, open := gapFor(s, converge.OwnerDimInteractive)
	if !open {
		t.Fatal("blocked composer did not open the interaction gap")
	}
	if g.Kind != converge.OwnerGapUXDegraded || g.Reason != "composer_blocked" {
		t.Fatalf("gap = %s/%s, want ux_degraded/composer_blocked", g.Kind, g.Reason)
	}

	// The unblock report is what satisfies it — the step never does.
	s.NoteOwnerComposerBlocked(false, "")
	s.ReconcileOwnerHealth(clk.Advance(time.Second))
	if _, open := gapFor(s, converge.OwnerDimInteractive); open {
		t.Fatal("unblocked composer left the interaction gap standing")
	}
}

// 🎯T361: the UX-degrade step must carry the yield/hydrate hint on a status
// frame — that hint is the whole client-side recovery, not decoration.
func TestOwnerHealthUXStepHintsYieldHydrate(t *testing.T) {
	s, frames, clk := ownerHealthServer(t, func(string) error { return nil })
	drainFrames(frames)

	err := ownerActuator{s}.Step(
		converge.OwnerGap{Dimension: converge.OwnerDimInteractive, Kind: converge.OwnerGapUXDegraded},
		converge.StepUXCoordinate, clk.Now())
	if err != nil {
		t.Fatalf("ux coordinate step: %v", err)
	}
	if _, ok := frameMatching(drainFrames(frames), func(m map[string]any) bool {
		return m["type"] == "status" && m["ux"] == "yield_hydrate"
	}); !ok {
		t.Fatal(`no status frame carried ux:"yield_hydrate"`)
	}
}

// 🎯T361: an alert the owner can see needs a recovery the owner can see. The
// client's banner for this class is sticky, so satisfying an escalated gap
// must publish, and a gap that never reached the owner must stay quiet.
func TestOwnerHealthPublishesRecoveryOnlyAfterAnAlert(t *testing.T) {
	s, frames, clk := ownerHealthServer(t, func(string) error { return nil })

	// Never alerted: satisfying the dimension says nothing to the owner.
	s.publishOwnerRecovered(converge.OwnerDimInteractive, "interaction_usable")
	if _, ok := frameMatching(drainFrames(frames), func(m map[string]any) bool {
		return m["ux"] == "recovered"
	}); ok {
		t.Fatal("recovery announced for a gap the owner was never told about")
	}

	if err := (ownerActuator{s}).Step(
		converge.OwnerGap{Dimension: converge.OwnerDimInteractive, Kind: converge.OwnerGapUXDegraded},
		converge.StepHumanAlert, clk.Now()); err != nil {
		t.Fatalf("human alert step: %v", err)
	}
	drainFrames(frames)

	s.publishOwnerRecovered(converge.OwnerDimInteractive, "interaction_usable")
	rec, ok := frameMatching(drainFrames(frames), func(m map[string]any) bool {
		return m["type"] == "status" && m["ux"] == "recovered"
	})
	if !ok {
		t.Fatal("an alerted gap recovered without telling the owner — sticky banner would outlive the gap")
	}
	if _, hasState := rec["state"]; hasState {
		t.Error("recovery frame carries a state — it could settle working chrome mid-turn (🎯T260)")
	}
	if text, _ := rec["text"].(string); !strings.Contains(text, "owner interaction recovered") {
		t.Errorf("recovery text = %q, want the class the banner matches", text)
	}

	// One alert, one recovery: a second call must not re-announce.
	s.publishOwnerRecovered(converge.OwnerDimInteractive, "interaction_usable")
	if _, ok := frameMatching(drainFrames(frames), func(m map[string]any) bool {
		return m["ux"] == "recovered"
	}); ok {
		t.Fatal("recovery re-announced without a new alert")
	}
}

// A stalled prompt is unstuck only when one is genuinely in flight: with no
// process there is nothing to interrupt, and the step must not be spent.
func TestOwnerHealthACPUnstickRequiresPromptInFlight(t *testing.T) {
	s, _, clk := ownerHealthServer(t, func(string) error { return nil })
	err := ownerActuator{s}.Step(
		converge.OwnerGap{Dimension: converge.OwnerDimReplyOrResidual, Kind: converge.OwnerGapACPStall},
		converge.StepACPUnstick, clk.Now())
	if !errors.Is(err, converge.ErrOwnerStepNotApplicable) {
		t.Fatalf("unstick with no in-flight prompt = %v, want not-applicable", err)
	}
}

// With nobody watching, chrome and interaction are out of scope — the
// reconciler must not broadcast recovery frames into an empty room.
func TestOwnerHealthNoClientScopesOutChrome(t *testing.T) {
	s, frames, clk := ownerHealthServer(t, func(string) error { return nil })
	ownerSend(t, s, "hello")
	s.mu.Lock()
	s.chatListeners = nil
	s.waiting = false
	s.overseerOwnerTurn = false
	s.mu.Unlock()
	drainFrames(frames)

	s.ReconcileOwnerHealth(clk.Now())
	s.ReconcileOwnerHealth(clk.Advance(100 * time.Second))
	if _, open := gapFor(s, converge.OwnerDimChromeTruthful); open {
		t.Error("chrome gap opened with no connected client")
	}
	if _, open := gapFor(s, converge.OwnerDimInteractive); open {
		t.Error("interaction gap opened with no connected client")
	}
}

// 🎯T437, stated directly: every owner-health edge is stamped from the clock
// the test also classifies from, so wall time cannot get between an edge and
// the verdict about it. Without this the subsystem reads two clocks — edges
// from time.Now, classification from the caller's `now` — and the distance
// between them is whatever the machine was busy doing, which is what made
// TestOwnerHealthClearsFalseWorkingChrome report on the fleet's load.
func TestOwnerHealthEdgesAreStampedFromTheClassifyingClock(t *testing.T) {
	s, _, clk := ownerHealthServer(t, func(string) error { return nil })
	ownerSend(t, s, "status please")
	s.NoteOwnerUIHeartbeat()
	s.NoteOwnerResidual("checked")

	// The wall clock moves; the one this fixture reasons about does not.
	time.Sleep(25 * time.Millisecond)

	now := clk.Now()
	obs := s.ObserveOwnerInteraction(now)
	for _, e := range []struct {
		name string
		at   time.Time
	}{
		{"SendAt", obs.SendAt},
		{"OwnerTurnAt", obs.OwnerTurnAt},
		{"ResidualAt", obs.ResidualAt},
	} {
		if !e.at.Equal(now) {
			t.Errorf("%s = %v, want the clock's own reading %v (off by %v of real time)",
				e.name, e.at, now, now.Sub(e.at))
		}
	}
	if obs.SinceUIHeartbeat != 0 {
		t.Errorf("SinceUIHeartbeat = %v, want 0 — the heartbeat was stamped at this very reading", obs.SinceUIHeartbeat)
	}
}

// 🎯T437's reproducer, made hermetic. The target's version needed 2×CPU busy
// loops and eight batches to catch this a quarter of the time; all that load
// ever did was put real time between the owner's send and the reconcile pass,
// and a sleep is that on demand. 25ms is far past the 10ms bounds the fixture
// used to run, which is where the red came from: pass 1 opened the *reply*
// dimension instead, its requeue step put a turn back in flight, and by pass 2
// the chrome was truthfully working — so the level-truth step never ran and
// the idle frame the test waits for was never published.
func TestOwnerHealthVerdictSurvivesRealElapsedTime(t *testing.T) {
	s, frames, clk := ownerHealthServer(t, func(string) error { return nil })

	ownerSend(t, s, "status please")
	s.mu.Lock()
	s.waiting = false
	s.overseerOwnerTurn = false
	s.mu.Unlock()
	drainFrames(frames)

	time.Sleep(25 * time.Millisecond)
	s.ReconcileOwnerHealth(clk.Now())
	if g, open := gapFor(s, converge.OwnerDimReplyOrResidual); open {
		t.Fatalf("real elapsed time opened %s/%s — the verdict is reading the machine, not the tree", g.Dimension, g.Kind)
	}
	if _, open := gapFor(s, converge.OwnerDimChromeTruthful); open {
		t.Fatal("chrome gap opened inside its grace window after a real-time delay")
	}

	time.Sleep(25 * time.Millisecond)
	s.ReconcileOwnerHealth(clk.Advance(20 * time.Second))
	if _, ok := frameMatching(drainFrames(frames), func(m map[string]any) bool {
		return m["type"] == "status" && m["state"] == "idle"
	}); !ok {
		t.Fatal("no idle level-truth frame published to clear false working chrome")
	}
}

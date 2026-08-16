// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/marcelocantos/jevons/internal/butler"
)

// 🎯T429 — A SEND REPORTS ONLY WHAT IT OBSERVED.
//
// THE SPECIMEN, and this whole file is built on it. jv-t405's session
// dd25d9f6-8bd8-4091-b3bb-19fc452ac8f2.jsonl records a queue-operation enqueue
// at 10:35:37.813Z, a queue-operation remove at 10:36:07.475Z, and an attachment
// at line 125 carrying the payload — while the send that delivered it returned
//
//	turn not submitted: composer state=paste_chip after 8 Enter presses
//
// One file, thirty seconds, the daemon's own records contradicting its own
// return value. Five more instances the same day, all in the same direction, and
// nine composer copies stacked by re-sends on top of them.
//
// WHAT WAS ACTUALLY WRONG. Not the uncertainty — the over-claim. claudia's
// submit loop cannot see a submission it did not recognise in a captured frame,
// and saying so is fine. What is not fine is the daemon translating "I could not
// verify this" into "this did not happen" on the way out, because those are the
// two answers a caller acts on most differently: one says look, the other says
// send again, and sending again is what stacks the composer.
//
// 🎯T416 built the instrument that can decide it — the RECEIVER's own records —
// and wired it into five callers. It did not reach the arm where the send CALL
// returns an error, because that arm returned before the instrument ran. This
// file is the ledger for closing that arm, and it has two halves for the same
// reason T416's does: the fix must not be achieved by answering "unknown"
// everywhere. Every honest-uncertainty test here is paired with one that proves
// a genuinely unsubmitted payload STILL gets accused.
//
//	MUST NOT REGRESS — a negative verdict from a non-observation:
//	  (i)   a paste-chip error over a payload the receiver drained into its turn
//	  (ii)  a paste-chip error over a payload the receiver has enqueued
//	  (iii) a reply timeout over a payload sitting in the receiver's transcript
//	  (iv)  re-send advice on any of the above
//
//	MUST STILL FIRE — a negative verdict from a positive observation:
//	  (A) an idle agent whose transcript never took the payload → not_submitted
//	  (B) a send error that really does disprove delivery → the old failure path
//	  (C) an unknown target → still an error, not a cheerful unknown

// chipSender is a live agent whose Send RETURNS AN ERROR while the payload
// reaches the receiver anyway. That combination is the entire target: every
// existing fixture in this package has Send returning nil, so the arm that
// throws away the observation was never exercised.
type chipSender struct {
	alive     bool
	sendErr   error
	sent      []string
	onPaste   func(text string)
	interrupt error
}

func (a *chipSender) Alive() bool { return a.alive }

func (a *chipSender) Send(text string) error {
	a.sent = append(a.sent, text)
	if a.onPaste != nil {
		a.onPaste(text)
	}
	return a.sendErr
}

func (a *chipSender) Interrupt() error { return a.interrupt }

// pasteChipErr is claudia's own text, verbatim in shape (internal/tmuxagent/
// send.go:422) including the frame tail — and the tail matters. On every live
// instance it read `Press up to edit queued messages`, which is the CLI saying
// it ACCEPTED the message. The submit loop was staring at a successful delivery
// and calling it a stuck composer.
func pasteChipErr() error {
	return fmt.Errorf("turn not submitted: composer state=paste_chip after 8 Enter presses; last frame tail:\n" +
		"  Press up to edit queued messages\n  paste again to expand\n❯ ")
}

// chipFixture is sendFixture with an erroring sender. The instrument is the
// REAL one — observeTurnFor over a real file on disk, real payload matching —
// so what is under test is the daemon's verdict and nothing else.
func chipFixture(t *testing.T, agent *chipSender, tr *sessionLog) (*Server, *overseerInbox) {
	t.Helper()
	inbox := &overseerInbox{}
	s := &Server{}
	s.SetOverseerDeliver(inbox.deliver)
	s.SetSenderResolver(func(string) (agentSender, bool, error) { return agent, false, nil })
	obs := newFakeObserver(tr.path)
	s.SetTurnWitness(func(_, payload string) turnWatch {
		return observeTurnFor(obs, payload, shortWindow)
	})
	return s, inbox
}

// ── (i) the specimen itself ────────────────────────────────────────────

// dd25d9f6 replayed. The receiver enqueued the payload and drained it into its
// turn; the send call reported `turn not submitted: composer state=paste_chip`.
//
// RED AGAINST THE PRE-FIX TREE: deliverToSenderWith took any non-busy send
// error straight to agenterr.ClassifyAndFormat and returned it, so the watch it
// had already opened — over the very file that contains the answer — was never
// awaited. The verdict was the transport's, about itself.
func TestPasteChipErrorOverADrainedPayloadIsNotReportedNotSubmitted(t *testing.T) {
	tr := newSessionLog(t)
	tr.submitted("the turn that was running when the brief arrived")
	payload := strings.Repeat(brief, 20)
	agent := &chipSender{alive: true, sendErr: pasteChipErr(), onPaste: func(text string) {
		// 10:35:37.813Z enqueue, 10:36:07.475Z remove, attachment at line 125.
		tr.enqueued(text)
		tr.drained(text)
	}}
	s, _ := chipFixture(t, agent, tr)

	res, err := s.deliverByName("jv-t405-daemon-supervised", payload, OriginAgent, false)
	if err != nil {
		t.Fatalf("a payload the receiver drained into its turn was reported as a failure: %v", err)
	}
	if res.Status != "sent" {
		t.Fatalf("status=%q want sent — the receiver's own queue records show the payload entered the turn", res.Status)
	}
	if !s.agentHasTurnBegan("jv-t405-daemon-supervised") {
		t.Error("turn-began not marked for a payload the receiver demonstrably took up")
	}
	// The transport's account is not suppressed — it is reported BESIDE the
	// observation. A reader who cannot see both cannot tell which one they are
	// being told, and this disagreement is the ordinary case now.
	for _, want := range []string{"paste_chip", "contradicted by"} {
		if !strings.Contains(res.Message, want) {
			t.Errorf("the operator is not shown both accounts: missing %q in %q", want, res.Message)
		}
	}
}

// ── (ii) enqueued, not yet drained ─────────────────────────────────────

// The same error over a payload the receiver has accepted behind a live turn.
// Reporting this as not-submitted accuses a healthy send; reporting it as sent
// would be the same lie one step later. It is queued, and that is a POSITIVE
// reading of the receiver's records rather than an inference from this daemon's
// memory of what the agent was doing.
func TestPasteChipErrorOverAnEnqueuedPayloadIsQueuedNotFailed(t *testing.T) {
	tr := newSessionLog(t)
	tr.submitted("the turn that is currently running")
	agent := &chipSender{alive: true, sendErr: pasteChipErr(), onPaste: func(text string) { tr.enqueued(text) }}
	s, _ := chipFixture(t, agent, tr)

	res, err := s.deliverByName("jevons-po", strings.Repeat(brief, 20), OriginAgent, false)
	if err != nil {
		t.Fatalf("an enqueued payload was reported as a failure: %v", err)
	}
	if res.Status != "queued" {
		t.Fatalf("status=%q want queued", res.Status)
	}
}

// ── (iv) and the honest unknown ────────────────────────────────────────

// The middle answer, which already existed and was not being used. The receiver
// is alive, its transcript is moving with other traffic, this payload is in
// none of its records, and this daemon has no in-flight record to interpret the
// silence with — the post-restart state, and the most common one there is.
//
// The verdict must be delivered_unconfirmed, and the ADVICE must change with
// it: an unknown is resolved by looking, not by sending again. Nine stacked
// composer copies came from advice this path used to give.
func TestPasteChipErrorWithNoReceiverRecordsIsUnknownAndAdvisesLookingNotResending(t *testing.T) {
	tr := newSessionLog(t)
	tr.submitted("a turn that began before this daemon started")
	agent := &chipSender{alive: true, sendErr: pasteChipErr(), onPaste: func(string) {
		// Other traffic moves the file; nothing carries this payload.
		tr.replied("still working on the previous instruction")
	}}
	s, _ := chipFixture(t, agent, tr)
	// Deliberately no flight record: this daemon has just restarted.

	res, err := s.deliverByName("jv-t434-watchdog-toolchain", strings.Repeat(brief, 20), OriginAgent, false)
	if err != nil {
		t.Fatalf("a non-observation was returned as a failure: %v", err)
	}
	if res.Status != "delivered_unconfirmed" {
		t.Fatalf("status=%q want delivered_unconfirmed", res.Status)
	}
	// Clause 4: name the instrument that can decide it, and forbid the retry.
	for _, want := range []string{"DO NOT re-send", "interrupt=true", "jevons_transcript_read", "queue-operation"} {
		if !strings.Contains(res.Message, want) {
			t.Errorf("the unknown does not name its instrument: missing %q in %q", want, res.Message)
		}
	}
	if strings.Contains(res.Message, "Treat as undelivered") {
		t.Errorf("the caller is still told to treat an unknown as a failure: %q", res.Message)
	}
}

// ── (A) the anti-downgrade half ────────────────────────────────────────

// THE FIX MUST NOT BE ACHIEVED BY ANSWERING UNKNOWN EVERYWHERE. Same error,
// same absent payload — and this time the daemon WATCHED the previous turn end,
// so the agent is known idle and there is no live turn for the message to be
// waiting behind. That is a positive observation, and it still convicts.
//
// Without this test, deleting the error arm entirely would pass every other
// test in this file, and the daemon would have traded a false negative for a
// false positive: work silently lost with a green tool result, which is the
// failure 🎯T387 and 🎯T416 exist to prevent.
func TestGenuinelyUnsubmittedPayloadStillReportsNotSubmitted(t *testing.T) {
	tr := newSessionLog(t)
	tr.submitted("earlier work that DID land, so the session is not born-stuck")
	agent := &chipSender{alive: true, sendErr: pasteChipErr()} // pastes, never submits, nothing enqueued
	s, _ := chipFixture(t, agent, tr)
	s.noteTurnEnded("jv-t429-send-verdict") // the daemon watched the last turn end

	res, err := s.deliverByName("jv-t429-send-verdict", strings.Repeat(brief, 20), OriginAgent, false)
	if err == nil {
		t.Fatalf("an idle agent whose transcript never took the payload was reported as %q", res.Status)
	}
	msg := err.Error()
	for _, want := range []string{"not submitted", "composer"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the accusation does not name itself: missing %q in %q", want, msg)
		}
	}
	// And it says where the accusation came from: the observation, with the
	// transport's claim recorded as agreeing rather than as the reason.
	if !strings.Contains(msg, "agrees with what was observed") {
		t.Errorf("the verdict does not distinguish observation from claim: %q", msg)
	}
	if s.agentHasTurnBegan("jv-t429-send-verdict") {
		t.Error("turn-began marked from a send that never became a turn")
	}
}

// ── (B) errors that really do disprove delivery ────────────────────────

// The pure classifier, over the strings that actually occur. A send error is
// asked one narrow question — does it DISPROVE delivery — and only errors that
// report on the HANDOVER answer yes. Errors that report on a captured frame, or
// on a wait for a reply, have not looked at the receiver at all.
func TestClassifySendErrorSeparatesNonObservationFromDisproof(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want SendClaim
	}{
		{"nil", nil, ClaimNotSent},
		{"paste chip", pasteChipErr(), ClaimSubmitUnverified},
		{"composer empty after paste",
			errors.New("turn not submitted: composer empty after paste (brief never reached pane)"),
			ClaimSubmitUnverified},
		{"paste never appeared",
			errors.New("turn not submitted: paste never appeared in pane within 8s; composer state=empty; last frame tail:\n❯ "),
			ClaimSubmitUnverified},
		{"reply deadline", context.DeadlineExceeded, ClaimReplyUnverified},
		{"wrapped reply deadline",
			fmt.Errorf("deliver turn to agent %q: %w", "jevons-po", context.DeadlineExceeded),
			ClaimReplyUnverified},
		{"operation timed out",
			errors.New("push \"jevons-po\" (event \"worker-finished\"): The operation timed out"),
			ClaimReplyUnverified},
		// Disproof: each of these is a statement about the handover.
		{"no such agent", errors.New(`no agent "ghost"`), ClaimNotSent},
		{"registry refused", errors.New(`could not rehydrate agent "w": pane exited`), ClaimNotSent},
		{"provider outage", errors.New("session/prompt failed: 503 Service Unavailable"), ClaimNotSent},
		{"no participant", errors.New(`deliver: no participant "ghost"`), ClaimNotSent},
	}
	for _, c := range cases {
		if got := ClassifySendError(c.err); got != c.want {
			t.Errorf("%s: claim=%s want %s", c.name, got, c.want)
		}
	}
	// The rule the rest of the daemon reads off this type: exactly one of the
	// three answers is evidence of anything.
	if !ClaimNotSent.DisprovesDelivery() {
		t.Error("a handover that never happened must disprove delivery")
	}
	if ClaimSubmitUnverified.DisprovesDelivery() || ClaimReplyUnverified.DisprovesDelivery() {
		t.Error("a failure to observe was treated as evidence")
	}
}

// A frame tail can quote anything at all — including the word timeout, off
// whatever the agent had on screen. The submit markers are therefore checked
// first, and this pins that ordering: misclassifying a stuck composer as a
// reply timeout would send it down a path that reads the wrong instrument.
func TestFrameTailQuotingATimeoutIsStillASubmitClaim(t *testing.T) {
	t.Parallel()
	err := errors.New("turn not submitted: composer state=paste_chip after 8 Enter presses; last frame tail:\n" +
		"  ⎿  Error: context deadline exceeded while waiting for the operation timed out\n❯ ")
	if got := ClassifySendError(err); got != ClaimSubmitUnverified {
		t.Fatalf("claim=%s want %s — the frame tail was read as the daemon's own failure", got, ClaimSubmitUnverified)
	}
}

// ── the drain, where believing the transport is most expensive ─────────

// On the drain the message has ALREADY left the daemon's queue. Believing an
// unverified submit there loses it twice: the queue no longer holds it, and the
// fleet is told a delivery that happened did not — a fleet-health note naming
// an "undelivered backlog" that the receiver is at that moment working through.
func TestDrainWithAnUnverifiedSubmitReadsTheReceiverInstead(t *testing.T) {
	tr := newSessionLog(t)
	tr.submitted("the turn that just ended")
	agent := &chipSender{alive: true, sendErr: pasteChipErr(), onPaste: func(text string) {
		tr.enqueued(text)
		tr.drained(text)
	}}
	s, inbox := chipFixture(t, agent, tr)

	s.enqueueAgentSend("w", strings.Repeat(brief, 20))
	s.drainAgentSendQueue("w")

	if len(agent.sent) != 1 {
		t.Fatalf("drain sent %d times want 1", len(agent.sent))
	}
	if len(inbox.texts) != 0 {
		t.Fatalf("a delivered payload was reported to the overseer as an undelivered backlog: %v", inbox.texts)
	}
	if n := s.pendingAgentSends("w"); n != 0 {
		t.Fatalf("pending=%d want 0 — the message was delivered", n)
	}
	// The discriminating assertion, and the reason this test is not satisfied
	// by the pre-fix tree: believing the transport made the drain RETURN, which
	// left an empty queue, a silent log line and no record that the receiver had
	// taken the message up. Everything above is true of that silence too. Only
	// the turn-began mark says the daemon actually read the receiver.
	if !s.agentHasTurnBegan("w") {
		t.Fatal("the drain believed the transport and never read the receiver's records")
	}
	if got := s.flightState("w"); got != FlightInFlight {
		t.Fatalf("flight=%s want in_flight — the drained payload is in a running turn", got)
	}
}

// And the drain's other half: an unverified submit with nothing in the
// receiver's records is still surfaced. The drain must not become a place where
// messages quietly evaporate.
func TestDrainWithAnUnverifiedSubmitAndNoRecordsStillSurfaces(t *testing.T) {
	tr := newSessionLog(t)
	tr.submitted("the turn that just ended")
	agent := &chipSender{alive: true, sendErr: pasteChipErr()} // nothing reaches the receiver
	s, inbox := chipFixture(t, agent, tr)

	s.enqueueAgentSend("w", strings.Repeat(brief, 20))
	s.drainAgentSendQueue("w")

	if len(inbox.texts) != 1 {
		t.Fatalf("an undelivered backlog was not surfaced to the overseer: %v", inbox.texts)
	}
	if !strings.Contains(inbox.texts[0], "Undelivered backlog") {
		t.Errorf("fleet-health note does not name the failure: %q", inbox.texts[0])
	}
}

// ── the spawn path: a seat is not torn down on a non-observation ───────

// releaseUnbriefedSeat stops the process AND removes the registry row, so a
// spawn that mis-reads its brief as undelivered destroys a worker that is at
// that moment reading it — and leaves the target unengaged, which is how the
// work gets spawned twice. The spawn path owns its own verdict (confirmByCaller),
// so the rule has to hold in ConfirmTurnBegan as well as in the send path.
func TestStartPromptWithAnUnverifiedSubmitIsConfirmedFromTheReceiver(t *testing.T) {
	t.Parallel()
	delivered := TurnEvidence{Observed: true, Durable: true, PayloadEnteredTurn: true,
		Detail: "transcript shows this payload leaving the receiver's queue into a turn"}
	if err := ConfirmTurnBegan("", pasteChipErr(), delivered); err != nil {
		t.Fatalf("a brief the receiver drained into its turn was refused: %v", err)
	}
	// The other direction, unchanged: nothing observed is still a failure,
	// whatever the send call said.
	nothing := TurnEvidence{Observed: true, Durable: true, Detail: "transcript still 412 bytes after 45s"}
	if err := ConfirmTurnBegan("", pasteChipErr(), nothing); err == nil {
		t.Fatal("a brief with no receiver-side evidence was confirmed")
	}
	// And an error that DISPROVES delivery still short-circuits: there is
	// nothing to look for, so evidence cannot rescue it.
	if err := ConfirmTurnBegan("", errors.New(`no agent "ghost"`), delivered); err == nil {
		t.Fatal("an error that disproves delivery was overridden by evidence")
	}
}

// ── (iii) clause 5: the transport that cannot see completion ───────────

// blockingParticipants is a fleet whose Deliver takes longer than any client
// will wait — which is not a pathology, it is a turn. The fleet allows a reply
// ten minutes (fleet.defaultReplyTimeout); MCP clients do not. The payload is
// recorded into the receiver's transcript on the way in, exactly as the live
// specimen did: 3873 bytes at 10:22:51Z, line 115 of session 76cef0a9, while
// the caller was told `The operation timed out`.
type blockingParticipants struct {
	names     map[string]bool
	onDeliver func(text string)
	release   chan struct{}
}

func (p *blockingParticipants) Exists(id string) bool { return p.names[id] }

func (p *blockingParticipants) Deliver(id, text string) (string, error) {
	if p.onDeliver != nil {
		p.onDeliver(text)
	}
	<-p.release
	return "late reply from " + id, nil
}

func pushFixture(t *testing.T, p butler.Participants, tr *sessionLog) *Server {
	t.Helper()
	t.Setenv(EventPushReplyWindowEnv, "80ms")
	s := &Server{butler: newPushButler(t, t.TempDir(), &pushFakeFleet{}, p)}
	obs := newFakeObserver(tr.path)
	s.SetTurnWitness(func(_, payload string) turnWatch {
		return observeTurnFor(obs, payload, 0)
	})
	return s
}

func callPush(t *testing.T, s *Server, target, event, text string) *mcp.CallToolResult {
	t.Helper()
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"target": target, "event": event, "text": text}
	res, err := s.handleEventPush(context.Background(), req)
	if err != nil {
		t.Fatalf("handleEventPush: %v", err)
	}
	return res
}

// TIMEOUT IS NOT ABSENCE. The reply never comes back inside the window, and the
// receiver's records show the payload in its turn. The tool must report
// delivered — from those records — and must not hand the caller a failure.
func TestEventPushWhoseReplyOutlivesTheWindowReportsDeliveryFromTheReceiver(t *testing.T) {
	tr := newSessionLog(t)
	tr.submitted("the turn that was running when the push arrived")
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	p := &blockingParticipants{
		names:   map[string]bool{"jevons-po": true},
		release: release,
		onDeliver: func(text string) {
			tr.enqueued(text)
			tr.drained(text)
		},
	}
	s := pushFixture(t, p, tr)

	res := callPush(t, s, "jevons-po", "worker-finished", "🎯T429 clause 5 landed; frontier is clear")
	if res.IsError {
		t.Fatalf("a delivered push was returned as an error: %q", toolText(res))
	}
	text := toolText(res)
	for _, want := range []string{"DELIVERED", "delivered_confirmed", "DO NOT re-push"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
}

// The same wait with nothing in the receiver's records: an unknown, reported as
// an unknown. Not an error result — an error result is read by every caller as
// "this did not happen", which is the over-claim wearing a different hat.
func TestEventPushWithNoEvidenceIsUnknownRatherThanFailure(t *testing.T) {
	tr := newSessionLog(t)
	tr.submitted("the receiver's earlier traffic")
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	p := &blockingParticipants{names: map[string]bool{"jevons-po": true}, release: release}
	s := pushFixture(t, p, tr)

	res := callPush(t, s, "jevons-po", "timer", "tick")
	if res.IsError {
		t.Fatalf("an unknown was returned as an error: %q", toolText(res))
	}
	text := toolText(res)
	for _, want := range []string{"NOT CONFIRMED", "delivered_unconfirmed", "DO NOT re-push", "jevons_transcript_read"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
}

// (C) The anti-downgrade half for the push path. A target that does not exist
// is not an uncertainty — nothing was handed to anybody — and it must still
// come back as an error. A tool that answers "unknown" to a typo is useless.
func TestEventPushToAnUnknownTargetIsStillAnError(t *testing.T) {
	tr := newSessionLog(t)
	s := pushFixture(t, &pushFakeParticipants{names: map[string]bool{}}, tr)

	res := callPush(t, s, "ghost", "ci", "green")
	if !res.IsError {
		t.Fatalf("an undeliverable push was reported as an unknown: %q", toolText(res))
	}
}

// And the ordinary case, which must stay ordinary: a reply that arrives inside
// the window is returned verbatim, with none of the above machinery in it.
func TestEventPushWithAPromptReplyIsUnchanged(t *testing.T) {
	tr := newSessionLog(t)
	p := &pushFakeParticipants{names: map[string]bool{"jevons-po": true}}
	s := pushFixture(t, p, tr)

	res := callPush(t, s, "jevons-po", "timer", "tick")
	if res.IsError {
		t.Fatalf("a healthy push failed: %q", toolText(res))
	}
	text := toolText(res)
	if !strings.Contains(text, "agent-ack:") {
		t.Fatalf("the reply was not returned to the caller: %q", text)
	}
	if strings.Contains(text, "NOT CONFIRMED") {
		t.Fatalf("a prompt reply was hedged: %q", text)
	}
}

// The window is a real bound and it is the daemon's own, not the client's. If
// this constant ever drifts above a plausible MCP client timeout the fix stops
// working silently — the client answers first again, and nothing in the daemon
// notices.
func TestEventPushReplyWindowStaysBelowAPlausibleClientBound(t *testing.T) {
	if defaultEventPushReplyWindow >= 60*time.Second {
		t.Fatalf("window=%s — at or above the 60s bound clients answer on, which is the defect",
			defaultEventPushReplyWindow)
	}
	t.Setenv(EventPushReplyWindowEnv, "not a duration")
	if got := eventPushReplyWindow(); got != defaultEventPushReplyWindow {
		t.Fatalf("an unusable override disabled the bound: %s", got)
	}
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/marcelocantos/jevons/internal/turnev"
)

// 🎯T416 — jevons_agent_send must not say "Message sent" for a payload that was
// pasted and never submitted.
//
// RED AGAINST THE PRE-FIX TREE. deliverToSender reported status=sent whenever
// proc.Send() returned nil, so every "never submitted" case below passed. On
// 2026-08-10 five overseer→PO sends logged status=sent queued=0 and three of
// them are in no transcript on disk; two more accumulated in a composer and
// arrived, minutes later, concatenated into one message by a LATER send's Enter.
//
// THIS FILE IS THE INSTRUMENT LEDGER (clause 9), and it has two halves on
// purpose. Killing bad oracles is not enough: on the day this was written,
// three different instruments were consulted and all three passed while wrong,
// and two instruments we already had were truthful and never consulted. A suite
// that only records the failures leaves the next reader reaching for whatever
// is closest to hand, which is exactly how three false instruments got trusted
// inside one day.
//
//	EXCLUDED — each must break this suite:
//	  (i)   transcript GROWTH as confirmation
//	  (ii)  RAW FILE TEXT match, accepting a payload quoted in a pane capture
//	  (iii) BEHAVIOURAL COMPLIANCE — the receiver acks, or does as it was told
//
//	EXERCISED — asserted positively, not only as mutants:
//	  (A) the handover dispatcher's fail-closed daemon log (internal/fleet,
//	      TestHandoverHandOffFailsClosedAndSaysSo — it lives there because that
//	      is where the honest line is emitted)
//	  (B) TRANSCRIPT-FILE ABSENCE for a registry-named session, used WITH
//	      payload-match and never instead of it
//
// And one mutant in the other direction, which is the standing hazard of every
// fix of this shape (🎯T387's residual, clause 9): a confirmation so strict that
// ordinary sends fail would strand the fleet. The realistic instance is a
// message legitimately waiting behind a live turn.

// ── fixtures: a real transcript, read by the real instrument ─────────────

// sessionLog is a session JSONL written by hand in the shapes real ones come
// in. The file does NOT exist until something is appended, which is not an
// accident of the fixture — it is instrument (B): a session's JSONL is created
// by its first submit, so a born-stuck session has no file at all.
type sessionLog struct {
	t    *testing.T
	path string
}

func newSessionLog(t *testing.T) *sessionLog {
	t.Helper()
	return &sessionLog{t: t, path: filepath.Join(t.TempDir(), "968649a9-22f8-43a1-825b-9be0e064a9c1.jsonl")}
}

func (x *sessionLog) append(rec map[string]any) {
	x.t.Helper()
	line, err := json.Marshal(rec)
	if err != nil {
		x.t.Fatal(err)
	}
	f, err := os.OpenFile(x.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		x.t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		x.t.Fatal(err)
	}
}

// submitted is what a real submit leaves behind: a user-role record whose
// content is the text the sender authored.
func (x *sessionLog) submitted(text string) {
	x.append(map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": text},
	})
}

// paneCapture is what an agent's own `tmux capture-pane` looks like once it
// lands in that agent's transcript: a user-role record carrying a tool_result.
// The bytes of the unsubmitted payload are in the file, in a record whose role
// is "user" — which is why a raw grep, or any check that reads a record without
// asking which part of it was authored, calls a stuck payload delivered. That
// is how a real loss was retracted as a phantom.
func (x *sessionLog) paneCapture(text string) {
	x.append(map[string]any{
		"type": "user",
		"message": map[string]any{"role": "user", "content": []any{
			map[string]any{
				"type":        "tool_result",
				"tool_use_id": "toolu_capture_pane",
				"content":     "$ tmux capture-pane -p\n> " + text + "\n  [Pasted text #24]",
			},
		}},
	})
}

// enqueued is what the receiving CLI writes when it accepts a message while a
// turn is already running: queue bookkeeping in the same session file, carrying
// the payload verbatim. Note what it is NOT — a user message. A check that
// reads user messages only reports this payload absent, which is precisely the
// false negative the overseer hit at 09:31:24Z against this target's own worker
// and the daemon repeated twice more.
func (x *sessionLog) enqueued(text string) {
	x.append(map[string]any{
		"type":      "queue-operation",
		"operation": "enqueue",
		"content":   text,
	})
}

// drained is the queue handing a message into the turn: the remove record, and
// the queued_command attachment the CLI writes as the turn takes it up. The
// payload NEVER becomes a user message — this is the whole of the evidence
// that it was delivered.
func (x *sessionLog) drained(text string) {
	x.append(map[string]any{
		"type":      "queue-operation",
		"operation": "remove",
		"content":   text,
	})
	x.append(map[string]any{
		"isSidechain": false,
		"attachment":  map[string]any{"type": "queued_command", "prompt": text},
	})
}

// replied is the agent speaking — an ack, or any other compliance.
func (x *sessionLog) replied(text string) {
	x.append(map[string]any{
		"type": "assistant",
		"message": map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "text", "text": text},
		}},
	})
}

func (x *sessionLog) raw() string {
	x.t.Helper()
	b, err := os.ReadFile(x.path)
	if err != nil {
		return ""
	}
	return string(b)
}

// sendingAgent is fakeSender plus the part that actually matters: what the
// agent does with the keystrokes once Send has returned. The defect is that
// Send returns nil either way, so onPaste is the variable under test and the
// return value is not.
type sendingAgent struct {
	alive   bool
	sent    []string
	onPaste func(text string)
}

func (a *sendingAgent) Alive() bool { return a.alive }

func (a *sendingAgent) Send(text string) error {
	a.sent = append(a.sent, text)
	if a.onPaste != nil {
		a.onPaste(text)
	}
	return nil
}

func (a *sendingAgent) Interrupt() error { return nil }

// sendFixture wires one agent whose transcript is watched by the REAL
// instrument — observeTurnFor over the real file, real payload matching. The
// witness seam is used to point the instrument at a fake process, not to fake
// the evidence: everything the product would compute is computed here.
func sendFixture(t *testing.T, agent *sendingAgent, tr *sessionLog) (*Server, *overseerInbox) {
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

// brief is the size and shape of the payload that sticks. The first send to any
// agent is multi-KB whatever the caller does, because the daemon prepends the
// standing fleet brief — "keep sends short" is not a mitigation the sender can
// apply.
const brief = "Execute 🎯T416 clause 2: jevons_agent_send confirms turn-begin.\n" +
	"Establish the boundary before changing anything, as jv-t387 did.\n" +
	"Do not widen the 45s window. Local master only.\n"

// ── both directions over the send path (clause 9, the core) ─────────────

// A large send that is submitted reports begun — the over-strictness side of
// the ledger. If this fails, the fix strands the fleet.
func TestLargeSendIsReportedBegunWhenItBecomesATurn(t *testing.T) {
	tr := newSessionLog(t)
	agent := &sendingAgent{alive: true, onPaste: func(text string) { tr.submitted(text) }}
	s, _ := sendFixture(t, agent, tr)

	payload := strings.Repeat(brief, 20) // ~4.5KB, the size that stuck live
	res, err := s.deliverByName("jv-t416-worker", payload, OriginAgent, false)
	if err != nil {
		t.Fatalf("a submitted send must not error: %v", err)
	}
	if res.Status != "sent" {
		t.Fatalf("status=%q want sent", res.Status)
	}
	if !s.agentHasTurnBegan("jv-t416-worker") {
		t.Fatal("turn-began not marked for a payload that reached the transcript")
	}
	if got := s.flightState("jv-t416-worker"); got != FlightInFlight {
		t.Fatalf("flight=%s want in_flight — a begun turn is running", got)
	}
}

// The defect itself: the paste lands in the composer, Send returns nil, and
// nothing else happens. The caller must be told it failed.
func TestUnsubmittedSendIsReportedAsFailureNotSent(t *testing.T) {
	tr := newSessionLog(t)
	tr.submitted("earlier work that DID land, so the session is not born-stuck")
	agent := &sendingAgent{alive: true} // pastes, never submits
	s, _ := sendFixture(t, agent, tr)
	s.noteTurnEnded("jv-t416-worker") // the daemon watched the last turn end

	res, err := s.deliverByName("jv-t416-worker", strings.Repeat(brief, 20), OriginAgent, false)
	if err == nil {
		t.Fatalf("a payload that never became a turn was reported as %q", res.Status)
	}
	if res.Status == "sent" {
		t.Fatal("status=sent for an unsubmitted payload")
	}
	if s.agentHasTurnBegan("jv-t416-worker") {
		t.Fatal("turn-began marked from a send that never became a turn")
	}

	// Clause 3: an operator reading this must not confuse it with a provider
	// refusal, a billing wall, or an agent that had nothing to say — that
	// confusion cost thirteen hours of misdiagnosis.
	msg := err.Error()
	for _, want := range []string{"not submitted", "composer", "not refused by the provider", "not an empty reply"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error does not name the failure as itself: missing %q in %q", want, msg)
		}
	}
}

// ── EXCLUDED (i): growth is not confirmation ───────────────────────────

// Specimen A, off by one. A send's Enter submits whatever the composer already
// held, so the transcript grows promptly and healthily while the payload the
// caller just handed over stays behind. Growth-based confirmation does not
// merely miss this — it confirms the WRONG message, and live it would have
// called S4 delivered on the strength of S2 and S3 arriving.
func TestGrowthCarryingAnEarlierPayloadIsNotConfirmation(t *testing.T) {
	tr := newSessionLog(t)
	tr.submitted("[fleet brief] the very first send, which stuck")
	agent := &sendingAgent{alive: true, onPaste: func(string) {
		// This send's Enter submits the BACKLOG, concatenated exactly as
		// claudia-po's 8965-byte user message was, and leaves the new payload
		// in the composer.
		tr.submitted("Resend — my earlier message never reached you." +
			"\nAnd the one before that.")
	}}
	s, _ := sendFixture(t, agent, tr)
	s.noteTurnEnded("w")

	before := len(tr.raw())
	if _, err := s.deliverByName("w", strings.Repeat(brief, 20), OriginAgent, false); err == nil {
		t.Fatal("growth carrying an earlier payload was accepted as delivery of this one")
	}
	if len(tr.raw()) <= before {
		t.Fatal("fixture did not grow — the mutant it kills would not have been tempted")
	}

	// The mutant, stated as itself: growth alone, on a durable backend, is not
	// begun at any flight state.
	for _, flight := range []TurnFlight{FlightIdle, FlightUnknown, FlightInFlight} {
		ev := TurnEvidence{Observed: true, Durable: true, ConversationGrew: true}
		if got := ClassifySendOutcome(flight, ev); got == OutcomeBegun {
			t.Errorf("flight=%s: growth alone classified as begun", flight)
		}
	}
}

// ── EXCLUDED (ii): raw file text is not the transcript ─────────────────

// An agent investigating a stuck send captures its own composer, and the
// capture lands in its transcript inside a tool_result on a user-role record.
// The payload's bytes are then in the file — a grep finds them — and it was
// never delivered. The check reads authored user text only.
func TestPayloadQuotedInAPaneCaptureIsNotDelivery(t *testing.T) {
	tr := newSessionLog(t)
	// The payload ends in one long unbroken line, which is what makes this
	// fixture a real temptation rather than a lucky miss: the needle is a slice
	// of that line, so it survives JSON encoding verbatim and a mutant reading
	// raw record bytes DOES find it inside the pane capture. A payload whose
	// tail wrapped would be spared by escaping alone, and the mutant would
	// appear killed while still being wrong.
	marker := strings.Repeat("SPECIMEN-A-S4 pasted and never submitted, ", 20)
	payload := strings.Repeat(brief, 8) + marker
	agent := &sendingAgent{alive: true, onPaste: func(text string) {
		// The agent looks at its own composer and reports what it sees.
		tr.paneCapture(text)
	}}
	s, _ := sendFixture(t, agent, tr)
	s.noteTurnEnded("w")

	_, err := s.deliverByName("w", payload, OriginAgent, false)
	if err == nil {
		t.Fatal("a payload quoted in a pane capture was accepted as delivered")
	}

	// The mutant would pass: what it looks for really is in the file, in a
	// record whose role is "user". Asserting that is the point — without it
	// this test would also pass against a grep that merely happened not to
	// match, and would stop killing the mutant the moment the fixture drifted.
	needle := turnev.Needle(payload)
	if needle == "" {
		t.Fatal("fixture payload is not distinctive enough to be looked for")
	}
	if !strings.Contains(tr.raw(), needle) {
		t.Fatal("fixture is not the mutant's temptation: a raw match would not find the payload either")
	}
}

// ── EXCLUDED (iii): compliance is not delivery ─────────────────────────

// claudia-po's ACK416. It was asked to reply with a single word so delivery
// could be confirmed, and it did — having read the request off its own
// unsubmitted composer while investigating this very bug. The ack was then
// recorded as proof of delivery, and the message it acknowledged is in no
// transcript on disk.
//
// The inversion is the part worth keeping: because that payload never became a
// turn, the ack is evidence FOR the loss, not against it. Compliance proves the
// receiver SAW the text. Seeing is what a composer is for.
func TestReceiverCompliesWhileThePayloadSitsUnsubmitted(t *testing.T) {
	tr := newSessionLog(t)
	payload := strings.Repeat(brief, 8) + "\nReply with the single word ACK416 as your first line."
	agent := &sendingAgent{alive: true, onPaste: func(text string) {
		// Reads its own composer, and does exactly as instructed.
		tr.paneCapture(text)
		tr.replied("ACK416\n\nOn it — starting clause 2 now.")
	}}
	s, _ := sendFixture(t, agent, tr)
	s.noteTurnEnded("claudia-po")

	if _, err := s.deliverByName("claudia-po", payload, OriginAgent, false); err == nil {
		t.Fatal("an ack was accepted as delivery of the message it acked")
	}
	if !strings.Contains(tr.raw(), "ACK416") {
		t.Fatal("fixture did not comply — it is not the mutant's temptation")
	}

	// Compliance in the shape the daemon can actually see: the agent stirred,
	// the transcript moved, a session event fired. None of it identifies the
	// message on a backend where the message can be identified.
	ev := TurnEvidence{Observed: true, Durable: true, ConversationGrew: true, SessionEvent: true}
	if got := ClassifySendOutcome(FlightIdle, ev); got != OutcomeNotSubmitted {
		t.Fatalf("an active, replying, event-publishing agent classified as %s", got)
	}
}

// ── EXERCISED (B): transcript-file absence ─────────────────────────────

// The born-stuck positive. A session's JSONL is created by its first submit, so
// a registry-named session with no file has never begun a turn — cheap, needs
// no tmux, and unlike every other instrument here it ASSERTS the failure rather
// than failing to deny it.
//
// It is what rescues the unknown case: a turn in flight would have created the
// file, so file-absence settles the question the lost in-flight record cannot.
// Reproduced on three sessions in one morning — the overseer's 795525fb, this
// worker's own 968649a9, and claudia-po's ab2326d3 in another repo — each with
// a live pane, a registry row, and no file until a hand-pressed Enter created
// one at that instant.
func TestNoTranscriptFileIsAPositiveBornStuckFinding(t *testing.T) {
	tr := newSessionLog(t) // deliberately never written to
	agent := &sendingAgent{alive: true}
	s, _ := sendFixture(t, agent, tr)
	// No noteTurnEnded: this daemon has never observed a boundary for this
	// agent, which is the post-restart state and the auto-spawn state.
	if got := s.flightState("jv-t155-auto"); got != FlightUnknown {
		t.Fatalf("flight=%s want unknown", got)
	}

	_, err := s.deliverByName("jv-t155-auto", strings.Repeat(brief, 20), OriginAgent, false)
	if err == nil {
		t.Fatal("a born-stuck agent — no transcript at all — was reported as delivered")
	}
	if _, exists := turnev.Size(tr.path); exists {
		t.Fatal("fixture created a transcript; it is not born-stuck")
	}
}

// And the pairing, which is the whole reason instrument (B) is not enough on
// its own: file-exists says nothing about WHICH message landed. A transcript
// that exists and grows, with the payload absent and no flight record, stays
// UNKNOWN — never an accusation.
func TestFileExistenceNeverSubstitutesForPayloadMatch(t *testing.T) {
	present := TurnEvidence{Observed: true, Durable: true, ConversationGrew: true}
	if got := ClassifySendOutcome(FlightUnknown, present); got != OutcomeUnconfirmed {
		t.Errorf("live session, payload unseen, flight unknown: %s want %s", got, OutcomeUnconfirmed)
	}
	absent := TurnEvidence{Observed: true, Durable: true, TranscriptAbsent: true}
	if got := ClassifySendOutcome(FlightUnknown, absent); got != OutcomeNotSubmitted {
		t.Errorf("no transcript at all, flight unknown: %s want %s", got, OutcomeNotSubmitted)
	}
	// Absence outranks the daemon's own bookkeeping, because it is a statement
	// about the agent and flight is a statement about this process's memory.
	if got := ClassifySendOutcome(FlightInFlight, absent); got != OutcomeNotSubmitted {
		t.Errorf("no transcript with a stale in-flight record: %s want %s", got, OutcomeNotSubmitted)
	}
	// And the payload always wins: seeing it arrive settles everything above.
	seen := TurnEvidence{Observed: true, Durable: true, PayloadSeen: true}
	for _, flight := range []TurnFlight{FlightUnknown, FlightIdle, FlightInFlight} {
		if got := ClassifySendOutcome(flight, seen); got != OutcomeBegun {
			t.Errorf("flight=%s: payload seen classified as %s", flight, got)
		}
	}
}

// ── the over-broadness mutant (clause 9) ───────────────────────────────

// A message written to an agent that is mid-turn is ORDINARY — every reply to a
// working agent is this case. Reporting it as a failure would strand the fleet,
// which is the hazard 🎯T387's residual names.
//
// It is also where clause 4's merge half is closed: the message is held by the
// daemon and NOT pasted into a composer that can accumulate it, rotate it away,
// or hand it to a later send's Enter.
func TestMessageBehindALiveTurnIsQueuedNotFailed(t *testing.T) {
	tr := newSessionLog(t)
	tr.submitted("the turn that is currently running")
	agent := &sendingAgent{alive: true}
	s, _ := sendFixture(t, agent, tr)
	s.noteTurnInFlight("w")

	res, err := s.deliverByName("w", strings.Repeat(brief, 20), OriginAgent, false)
	if err != nil {
		t.Fatalf("a message behind a live turn must not fail: %v", err)
	}
	if res.Status != "queued" {
		t.Fatalf("status=%q want queued", res.Status)
	}
	if res.Queued != 1 {
		t.Fatalf("queued=%d want 1", res.Queued)
	}
	if len(agent.sent) != 0 {
		t.Fatalf("queued message was pasted into the composer anyway: %v", agent.sent)
	}
}

// ── clause 6: a lost in-flight record is not an accusation ─────────────

// jevonsd restarted three times on the day this was written, and in-flight
// state is process-local. After a restart, a send to an agent that IS genuinely
// mid-turn has no in-flight record. A three-state discriminator falls through
// to "no turn in flight and no evidence" and reports the exact defect it was
// built to detect, about a healthy send, on the most common path there is.
func TestMidTurnAgentAcrossALostInFlightRecordIsNotAccused(t *testing.T) {
	tr := newSessionLog(t)
	tr.submitted("a turn that began before this daemon started")
	agent := &sendingAgent{alive: true, onPaste: func(string) {
		// The agent is busy: its transcript moves with the turn already
		// running, and the new payload waits in the provider's own queue.
		tr.replied("still working on the previous instruction")
	}}
	s, _ := sendFixture(t, agent, tr)
	// Deliberately NO flight record — this daemon has just restarted.

	res, err := s.deliverByName("w", strings.Repeat(brief, 20), OriginAgent, false)
	if err != nil {
		t.Fatalf("a healthy send across a lost in-flight record was refused: %v", err)
	}
	if res.Status == "sent" {
		t.Fatal("unconfirmed send reported as sent — the caller would believe it")
	}
	if res.Status != "delivered_unconfirmed" {
		t.Fatalf("status=%q want delivered_unconfirmed", res.Status)
	}
	if !strings.Contains(res.Message, "NOT confirmed") {
		t.Fatalf("caller is not told the delivery is unconfirmed: %q", res.Message)
	}
}

// ── clause 4: an undelivered backlog is never silently destroyed ────────

// The queue drains on a turn boundary, where the agent is idle by construction.
// If the drained message sticks, re-queueing it would deliver a SECOND copy the
// instant anything submits the first — the silent merge this target exists to
// end. It stays where it is, and the operator is told.
func TestDrainedMessageThatSticksIsNotRequeuedAndIsSurfaced(t *testing.T) {
	tr := newSessionLog(t)
	tr.submitted("the turn that just ended")
	agent := &sendingAgent{alive: true} // pastes, never submits
	s, inbox := sendFixture(t, agent, tr)

	s.enqueueAgentSend("w", strings.Repeat(brief, 20))
	s.drainAgentSendQueue("w")

	if len(agent.sent) != 1 {
		t.Fatalf("drain sent %d times want 1", len(agent.sent))
	}
	if n := s.pendingAgentSends("w"); n != 0 {
		t.Fatalf("stuck message re-queued (%d pending) — the next drain delivers a duplicate", n)
	}
	if len(inbox.texts) != 1 {
		t.Fatalf("undelivered backlog was not surfaced to the overseer: %v", inbox.texts)
	}
	note := inbox.texts[0]
	for _, want := range []string{"Fleet health", "Undelivered backlog", "composer", "NOT been re-queued"} {
		if !strings.Contains(note, want) {
			t.Errorf("fleet-health note missing %q: %q", want, note)
		}
	}
}

// ── EXERCISED (C): the receiver's own queue records ────────────────────

// The false negative that payload-match-at-user-message-level cannot see, and
// it is the ORDINARY case rather than an edge.
//
// A message accepted behind a live turn is replayed into that turn as a
// queued_command attachment and NEVER becomes a user message. Read user
// messages only — correct against an agent quoting its own pane capture — and
// a message the receiver has already begun working on reads as absent. Live,
// twice: the overseer payload-matched a correction to this target's worker at
// 09:31:24Z and got ABSENT for a message the queue had already drained, and
// the daemon logged delivered_unconfirmed twice more, 21 seconds after the fact.
//
// A raw-file grep would also pass this fixture, which is why the mutant it
// kills is stated precisely: the excluded instrument is a grep that accepts a
// payload QUOTED in a tool_result (still killed, above); the included one reads
// the receiver's own queue bookkeeping.
func TestDrainedQueuedPayloadIsReportedBegunThoughNoUserMessageCarriesIt(t *testing.T) {
	tr := newSessionLog(t)
	tr.submitted("the turn that was running when this arrived")
	payload := strings.Repeat(brief, 20)
	agent := &sendingAgent{alive: true, onPaste: func(text string) {
		// The receiver accepts it behind the live turn, then drains it in.
		tr.enqueued(text)
		tr.drained(text)
	}}
	s, _ := sendFixture(t, agent, tr)
	// No flight record: this daemon has restarted since the turn began, which
	// is exactly when the old discriminator fell through to "unconfirmed".
	res, err := s.deliverByName("w", payload, OriginAgent, false)
	if err != nil {
		t.Fatalf("a payload the receiver drained into its turn was reported failed: %v", err)
	}
	if res.Status != "sent" {
		t.Fatalf("status=%q want sent — the receiver's queue records show it entered the turn", res.Status)
	}
	// The discriminator must be the queue records, not a lucky user message.
	if strings.Contains(tr.raw(), `"role":"user","content":"Execute`) {
		t.Fatal("fixture wrote a user message; it is no longer the queued case")
	}
}

// The other half, and the one that stops instrument (C) from being a rubber
// stamp: an enqueue with no drain is QUEUED, not begun. The receiver has it and
// has not started on it — reporting that as sent would be the same lie one step
// later, and reporting it as not-submitted would accuse a healthy send.
func TestEnqueuedPayloadIsReportedQueuedWithoutAnyFlightRecord(t *testing.T) {
	tr := newSessionLog(t)
	tr.submitted("the turn that is currently running")
	agent := &sendingAgent{alive: true, onPaste: func(text string) { tr.enqueued(text) }}
	s, _ := sendFixture(t, agent, tr)
	// Deliberately no flight record — the fact comes off the receiver's disk.
	res, err := s.deliverByName("w", strings.Repeat(brief, 20), OriginAgent, false)
	if err != nil {
		t.Fatalf("an enqueued message must not be reported as a failure: %v", err)
	}
	if res.Status != "queued" {
		t.Fatalf("status=%q want queued — enqueue is a POSITIVE reading of the queued state", res.Status)
	}
}

// ── EXCLUDED (i) on the START path: 🎯T387's licence, revoked ───────────

// deliverStartPrompt used to confirm with a growth-only watch, licensed by "a
// seat this daemon just launched has an empty composer and an empty transcript".
// The spawn path also RESUMES seats, and a resumed session's file already
// exists and is written to at startup.
//
// Live counter-example this fixture is built from: this target's own worker was
// stopped mid-turn and restarted onto the same session, which already held 158
// lines. The daemon logged prompt_delivered=true while the brief sat in the
// composer as `[Pasted text #1 +10 lines]`, and the transcript gained nothing
// for four minutes until a human pressed Enter.
func TestResumedSeatGrowthDoesNotConfirmAStartPromptThatStuck(t *testing.T) {
	tr := newSessionLog(t)
	tr.submitted("158 lines of an earlier session, resumed")
	agent := &sendingAgent{alive: true, onPaste: func(string) {
		// Startup writes on a resumed session: the agent stirs, the file grows,
		// and the brief is still sitting in the composer.
		tr.replied("resuming; reading my previous context")
	}}
	s, _ := sendFixture(t, agent, tr)

	err := s.deliverStartPrompt("jv-t416-worker", "Continue 🎯T416: the fourth caller's predicate. Local master only.")
	if err == nil {
		t.Fatal("a start prompt that never left the composer was confirmed delivered — mutant (i) is alive on the start path")
	}
	if !strings.Contains(err.Error(), "start prompt not delivered") {
		t.Fatalf("failure does not name itself: %v", err)
	}
	if s.agentHasTurnBegan("jv-t416-worker") {
		t.Fatal("turn-began marked from growth that was not this payload")
	}
}

// And its over-broadness pair: the same path must still confirm a start prompt
// that DID become a turn, or every spawn fails closed and the fleet stops.
func TestStartPromptThatBecomesATurnIsStillConfirmed(t *testing.T) {
	tr := newSessionLog(t)
	tr.submitted("an earlier session, resumed")
	agent := &sendingAgent{alive: true, onPaste: func(text string) { tr.submitted(text) }}
	s, _ := sendFixture(t, agent, tr)

	if err := s.deliverStartPrompt("jv-t416-worker", "Continue 🎯T416: the fourth caller's predicate. Local master only."); err != nil {
		t.Fatalf("a delivered start prompt was refused: %v", err)
	}
	if !s.agentHasTurnBegan("jv-t416-worker") {
		t.Fatal("turn-began not marked for a start prompt that reached the transcript")
	}
}

// ── clause 5: it holds on the overseer channel ─────────────────────────

// The overseer is the fleet's reporting sink, so a silent drop there loses
// every escalation in the hierarchy at once — and did: its composer was found
// holding four accumulated pastes, each answered "Message delivered to
// overseer". The seam returning nil means the notify queue accepted the text,
// which is a statement about the queue.
//
// Its turn boundaries are not visible from this package, so its flight is
// permanently unknown here: a payload that appears is confirmed, and one that
// does not is reported unconfirmed rather than delivered. Never the defect —
// a note legitimately waiting behind an owner turn looks identical from here.
func TestOverseerDeliveryIsNotClaimedWithoutEvidence(t *testing.T) {
	tr := newSessionLog(t)
	tr.submitted("the overseer's own earlier traffic")
	s, inbox := sendFixture(t, &sendingAgent{alive: true}, tr)

	res, err := s.deliverByName("jevons", "po: T416 converging, clause 2 landing", OriginAgent, false)
	if err != nil {
		t.Fatalf("overseer send must not error: %v", err)
	}
	if res.Status == "sent" {
		t.Fatal(`"Message delivered to overseer" with nothing observed — this is the four-chip composer`)
	}
	if len(inbox.texts) != 1 {
		t.Fatalf("the text must still be handed over: %v", inbox.texts)
	}
	if !strings.Contains(res.Message, "Treat as undelivered") {
		t.Fatalf("caller is not told to treat it as undelivered: %q", res.Message)
	}
}

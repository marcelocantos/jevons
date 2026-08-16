// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marcelocantos/claudia"
)

// 🎯T452 — a brief reaches the agent it was composed for.
//
// THE INCIDENT, 2026-08-15. In overseer session 0ac1ec7b three queued_command
// attachments arrived in one daemon-restart window. Two carried the overseer's
// own identity header. The third carried NAME: jv-t443-red-as-proof, ROLE: work
// agent, PARENT: jevons-po, TARGET: 🎯T443, followed by [event:
// post-restart-brief] and the work-agent arm of the standing brief — a brief
// composed for one agent, sitting in another agent's composer.
//
// WHY THAT IS WORSE THAN A STRAY MESSAGE, AND WHY IT IS TESTED HERE RATHER THAN
// AT THE COMPOSER. 🎯T425 built the header precisely so no agent has to consult
// the registry to learn its own name, and the header says so in its own words.
// Delivered to the wrong seat that instruction inverts: it becomes a
// second-person instruction to adopt someone else's name, parent and target,
// carrying the role doctrine of a role the reader does not occupy. Every
// composition in that window was correct — each header named the recipient the
// daemon composed it for. So an oracle that checks composition passes on the
// broken tree. What has to be asserted is the pairing: the header on the
// payload that ARRIVED names the seat it arrived in.
//
// The fanout is where composition and destination are separated by a loop, so
// the loop is what these tests drive: the restart broadcast over the registry
// (NotifyDaemonRestarted) and the post-restart-brief sweep that produced the
// observed payload (SweepIdleNudges with the pusher ResumeOpenMissionWorkers
// builds). The mismatch is injected the way the ledger describes it — a
// rotated/stale session id for one recipient resolving to another agent's seat.

// t452Inbox records what each seat physically received. Keyed by DESTINATION,
// not by the name the sender addressed: the whole defect is those two differing,
// so a fixture that records the addressed name would agree with the bug.
type t452Inbox struct {
	mu     sync.Mutex
	byDest map[string][]string
}

func newT452Inbox() *t452Inbox { return &t452Inbox{byDest: map[string][]string{}} }

func (b *t452Inbox) put(dest, text string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.byDest[dest] = append(b.byDest[dest], text)
}

func (b *t452Inbox) snapshot() map[string][]string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := map[string][]string{}
	for k, v := range b.byDest {
		out[k] = append([]string(nil), v...)
	}
	return out
}

// t452Seat is one agent's pane: an agentSender that knows which seat it is.
type t452Seat struct {
	dest  string
	inbox *t452Inbox
}

func (s t452Seat) Alive() bool { return true }

func (s t452Seat) Send(text string) error {
	s.inbox.put(s.dest, text)
	return nil
}

func (s t452Seat) Interrupt() error { return nil }

// t452Fixture is a daemon with a multi-agent registry, one recording seat per
// agent, and the overseer arm wired to the overseer's own seat — which is what
// makes a misroute observable at all, since the overseer is delivered to
// through the owner-chat seam rather than through a fleet process.
//
// overseerSession is what transcript.GetID reports, i.e. the session id the
// live overseer process claims. The rotation fixture works by giving some other
// agent's registry row that same id.
func t452Fixture(t *testing.T, overseerName, overseerSession string, defs ...claudia.AgentDef) (*Server, *t452Inbox) {
	t.Helper()
	dir := t.TempDir()
	reg, err := claudia.NewRegistry(filepath.Join(dir, "agents.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range defs {
		if d.WorkDir == "" {
			d.WorkDir = dir
		}
		if d.SessionID == "" {
			d.SessionID = "s-" + d.Name
		}
		d.Materialized = true
		d.Provider = "claude"
		if err := reg.Register(d); err != nil {
			t.Fatal(err)
		}
	}
	inbox := newT452Inbox()
	s := &Server{
		registry:     reg,
		fleetBriefed: map[string]bool{},
		transcript:   &TranscriptOps{GetID: func() string { return overseerSession }},
	}
	s.SetSenderResolver(func(name string) (agentSender, bool, error) {
		return t452Seat{dest: name, inbox: inbox}, false, nil
	})
	s.SetOverseerDeliver(func(text string, _ SendOrigin) error {
		inbox.put(overseerName, text)
		return nil
	})
	s.SetTurnWitness(func(_, _ string) turnWatch {
		return func() TurnEvidence {
			return TurnEvidence{Observed: true, Durable: true, PayloadSeen: true}
		}
	})
	return s, inbox
}

// misdeliveries is acceptance clause 2 as a predicate over what arrived: for
// every payload delivered anywhere, the identity header's NAME is the seat it
// landed in. Payloads with no header are not this target's business and are
// reported separately by the caller when it expects one.
func misdeliveries(got map[string][]string) []string {
	var out []string
	for dest, msgs := range got {
		for _, m := range msgs {
			addressed := IdentityHeaderName(m)
			if addressed == "" {
				continue
			}
			if !strings.EqualFold(addressed, dest) {
				out = append(out, "a brief composed for "+addressed+" was delivered into "+dest+"'s seat")
			}
		}
	}
	return out
}

// t452Fleet is the shape the incident happened in: an overseer, a PO under it,
// and work agents under the PO.
func t452Fleet() []claudia.AgentDef {
	return []claudia.AgentDef{
		{Name: "jevons", Purpose: claudia.PurposeOverseer},
		{Name: "jevons-po", Parent: "jevons", Purpose: claudia.PurposeWork},
		{Name: "jv-t443-red-as-proof", Parent: "jevons-po", Purpose: claudia.PurposeWork, TargetID: "T443"},
		{Name: "jv-t452-brief-addressing", Parent: "jevons-po", Purpose: claudia.PurposeWork, TargetID: "T452"},
	}
}

// Acceptance 2, on the restart broadcast: NotifyDaemonRestarted composes per
// recipient inside a loop over the registry, and one of its recipients is
// resolved through the session id rather than by name. Give a subordinate row
// the overseer's session id — the rotation the ledger names — and the
// subordinate's own brief is routed to the overseer seat.
func TestT452RestartBroadcastNeverLandsOneAgentsBriefInAnothersSeat(t *testing.T) {
	const overseer = "jevons"
	const overseerSession = "sid-overseer-live"

	defs := t452Fleet()
	// The rotation: jevons-po's row still carries the session id the overseer
	// process now reports. Nothing about the row says it is the overseer —
	// it has a parent, a work purpose and its own name.
	for i := range defs {
		if defs[i].Name == "jevons-po" {
			defs[i].SessionID = overseerSession
		}
	}
	s, inbox := t452Fixture(t, overseer, overseerSession, defs...)

	s.NotifyDaemonRestarted(overseer, "jevons-po", t.TempDir())

	got := inbox.snapshot()
	if bad := misdeliveries(got); len(bad) > 0 {
		t.Fatalf("restart broadcast misdelivered:\n  %s", strings.Join(bad, "\n  "))
	}

	// Not vacuous: the broadcast must actually have reached both recipients,
	// each with its own identity. A fix that refused every send would satisfy
	// the predicate above and deliver nothing.
	for _, want := range []string{overseer, "jevons-po"} {
		msgs := got[want]
		if len(msgs) == 0 {
			t.Fatalf("%s received nothing from the restart broadcast; inbox: %v", want, keysOf(got))
		}
		for _, m := range msgs {
			if !strings.Contains(m, "[event: ") {
				t.Fatalf("%s got a payload with no event wire prefix:\n%s", want, m)
			}
			if n := IdentityHeaderName(m); !strings.EqualFold(n, want) {
				t.Fatalf("%s received a brief addressed to %q", want, n)
			}
		}
	}
}

// Acceptance 2, on the loop the observed payload came out of: the
// post-restart-brief sweep, driven with the pusher ResumeOpenMissionWorkers
// builds. The product function gates on a live OS process, which a hermetic
// test has none of, so the sweep is called directly with its ProcessRunning
// seam — the composition and the delivery below it are the product's own.
func TestT452PostRestartBriefSweepNeverLandsAWorkersBriefInTheOverseerSeat(t *testing.T) {
	const overseer = "jevons"
	const overseerSession = "sid-overseer-live"

	defs := t452Fleet()
	// The incident's own shape: the work agent whose brief was found in the
	// overseer's composer carries the overseer's session id.
	for i := range defs {
		if defs[i].Name == "jv-t443-red-as-proof" {
			defs[i].SessionID = overseerSession
		}
	}
	s, inbox := t452Fixture(t, overseer, overseerSession, defs...)

	// ResumeOpenMissionWorkers's own pusher, verbatim.
	push := func(target, event, text string) error {
		msg := formatIdleNudgeWire(event, text)
		_, err := s.sendDaemonComposed(target, msg, true)
		return err
	}
	reps := SweepIdleNudges(IdleNudgeSweepArgs{
		Reg:            s.registry,
		Activity:       NewIdleActivityTracker(),
		Push:           push,
		Now:            time.Now(),
		PostRestart:    true,
		OverseerName:   overseer,
		ProcessRunning: func(string) bool { return true },
	})
	if len(reps) == 0 {
		t.Fatal("the sweep classified nothing — fixture no longer drives the loop under test")
	}

	got := inbox.snapshot()
	if bad := misdeliveries(got); len(bad) > 0 {
		t.Fatalf("post-restart-brief sweep misdelivered:\n  %s", strings.Join(bad, "\n  "))
	}
	if len(got[overseer]) > 0 {
		t.Fatalf("the overseer seat received %d worker brief(s) from a sweep that skips the overseer:\n%s",
			len(got[overseer]), strings.Join(got[overseer], "\n---\n"))
	}
	if len(got["jv-t443-red-as-proof"]) == 0 {
		t.Fatalf("the agent whose brief this was received nothing; inbox: %v", keysOf(got))
	}
}

// Acceptance 1: composition and destination are bound at the send, so a lookup
// that resolves elsewhere fails loudly instead of delivering the wrong brief.
// Driven without any registry pathology — a caller that composes for one agent
// and delivers by another name is refused on its own.
func TestT452ASendRefusesAMisaddressedBrief(t *testing.T) {
	const overseer = "jevons"
	s, inbox := t452Fixture(t, overseer, "sid-overseer-live", t452Fleet()...)

	composedFor := "jv-t443-red-as-proof"
	text := s.withIdentity(composedFor, formatIdleNudgeWire("post-restart-brief", "resume your mission"))
	if IdentityHeaderName(text) != composedFor {
		t.Fatalf("fixture did not compose a header for %s:\n%s", composedFor, text)
	}

	_, err := s.deliverByName("jv-t452-brief-addressing", text, OriginAgent, false)
	if err == nil {
		t.Fatal("a brief composed for another agent was delivered without complaint")
	}
	for _, want := range []string{composedFor, "jv-t452-brief-addressing"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not name %q, so an operator cannot tell what was misaddressed: %v", want, err)
		}
	}
	if got := inbox.snapshot(); len(got) != 0 {
		t.Fatalf("the refused brief was delivered anyway: %v", got)
	}
}

// The refusal's own blind spot, and the one message it must never eat. Clause 3
// tells an agent that received someone else's brief to report it "quoting this
// NAME and your own" — so the report carries a foreign header inside its body,
// which is exactly the shape the refusal above is looking for. A header is only
// ever composed at position 0; anywhere else it is a quotation, and both the
// composer and the reader are anchored there so that describing the incident
// accurately is not what stops it being reported.
func TestT452AnIncidentReportQuotingTheStrayHeaderStillSends(t *testing.T) {
	const overseer = "jevons"
	s, inbox := t452Fixture(t, overseer, "sid-overseer-live", t452Fleet()...)

	stray := s.withIdentity("jv-t443-red-as-proof", "[event: post-restart-brief] resume your mission")
	report := "A brief naming someone else arrived in my seat. Quoting it in full:\n\n" + stray

	if _, err := s.deliverByNameAs("jv-t452-brief-addressing", overseer, report, OriginAgent, false); err != nil {
		t.Fatalf("the misdelivery report was refused because it quoted the misdelivered header: %v", err)
	}
	got := inbox.snapshot()
	if len(got[overseer]) != 1 {
		t.Fatalf("the overseer received %d messages, want the report: %v", len(got[overseer]), keysOf(got))
	}
	if !strings.Contains(got[overseer][0], "jv-t443-red-as-proof") {
		t.Fatalf("the report arrived without the evidence it was quoting:\n%s", got[overseer][0])
	}

	// And when the daemon composes such a text, the header it prepends is the
	// destination's own — a quotation deeper in the body is not a header this
	// message already has.
	composed := s.withIdentity(overseer, report)
	if n := IdentityHeaderName(composed); !strings.EqualFold(n, overseer) {
		t.Fatalf("a daemon-composed message quoting a foreign header is addressed to %q, not to %s", n, overseer)
	}
	if bad := misdeliveries(map[string][]string{overseer: {composed}}); len(bad) > 0 {
		t.Fatalf("composing over a quoted header reads as a misdelivery: %v", bad)
	}
}

// The control. misdeliveries is the predicate both fanout tests are decided by,
// so it is itself asserted to detect a planted mismatch — otherwise a change
// that made IdentityHeaderName return "" for real headers would turn this whole
// file green while the product regressed.
func TestT452TheMisdeliveryPredicateDetectsAPlantedMismatch(t *testing.T) {
	s, _ := t452Fixture(t, "jevons", "sid-overseer-live", t452Fleet()...)
	brief := s.withIdentity("jv-t443-red-as-proof", "[event: post-restart-brief] resume")
	if brief == "" {
		t.Fatal("fixture composed no brief")
	}

	// What the incident looked like on disk: the overseer's seat holding a
	// payload whose header names a work agent.
	planted := map[string][]string{"jevons": {brief}}
	if bad := misdeliveries(planted); len(bad) != 1 {
		t.Fatalf("the predicate did not detect the observed incident (%d findings) — it is no longer an oracle", len(bad))
	}
	// And it does not fire on a correctly addressed one.
	if bad := misdeliveries(map[string][]string{"jv-t443-red-as-proof": {brief}}); len(bad) != 0 {
		t.Fatalf("the predicate fires on a correct delivery: %v", bad)
	}
}

// IdentityHeaderName reads the header's own NAME and nothing else. The trap it
// must not fall into is the one the header itself warns about: the brief below
// quotes agent names, including in "- NAME: " shape when a worker pastes a
// header it received, and the answer must still come from the header block.
func TestT452IdentityHeaderNameReadsOnlyTheHeader(t *testing.T) {
	s, _ := t452Fixture(t, "jevons", "sid-overseer-live", t452Fleet()...)

	real := s.withIdentity("jv-t452-brief-addressing", "do the work")
	if got := IdentityHeaderName(real); got != "jv-t452-brief-addressing" {
		t.Fatalf("IdentityHeaderName = %q on a composed header:\n%s", got, real)
	}
	if got := IdentityHeaderName(real + "\n\nquoting an earlier message:\n- NAME: someone-else\n"); got != "jv-t452-brief-addressing" {
		t.Fatalf("a NAME line quoted in the body answered for the header: %q", got)
	}
	if got := IdentityHeaderName("plain text with no header at all\n- NAME: not-a-header\n"); got != "" {
		t.Fatalf("read a name out of a message carrying no header: %q", got)
	}
}

// Acceptance 3: a misdirected brief is detectable by its recipient. The header
// is the instrument an agent uses to learn who it is, so it is the one place
// the "do not believe this if it names someone else" rule has to appear.
func TestT452HeaderTellsItsReaderWhatAWrongNameMeans(t *testing.T) {
	h := FormatIdentityHeader(AgentIdentity{
		Name:    "jv-t452-brief-addressing",
		Role:    RoleWork,
		Purpose: claudia.PurposeWork,
		Parent:  "jevons-po",
	})
	if h == "" {
		t.Fatal("no header composed")
	}
	if !strings.Contains(h, "🎯T452") {
		t.Fatalf("the header does not name the incident doctrine:\n%s", h)
	}
	for _, phrase := range []string{"do not adopt", "report"} {
		if !strings.Contains(strings.ToLower(h), phrase) {
			t.Fatalf("the header does not tell its reader to %q a header naming someone else:\n%s", phrase, h)
		}
	}
}

func keysOf(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

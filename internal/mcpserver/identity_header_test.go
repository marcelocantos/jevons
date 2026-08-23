// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/marcelocantos/claudia"
)

// 🎯T425 — the brief that carries role doctrine names its own reader.
//
// THE INCIDENT THIS PINS, 2026-08-10. bullseye-po declared a 🎯T125 violation
// and said plainly: "before checking the registry I did not know I was
// bullseye-po." It had been sent the standing brief, which states that
// Stratum-1 product owners are spawn-only for Build work — addressed to a role
// the recipient could not match itself to. That shape is not one rule's
// problem: 🎯T125, 🎯T129, 🎯T155 / 🎯T193, 🎯T325.1 and 🎯T176 are all
// role-conditional, and the brief arrived with no statement of which role the
// reader occupies.
//
// WHAT IS ASSERTED, AND WHY IT IS THE PRODUCT PATH AND NOT THE FORMATTER.
// A test that only calls FormatIdentityHeader would be red on the pre-fix tree
// for the uninteresting reason that the function did not exist, and would stay
// green the day a daemon-composed send path is added without the header — which
// is the regression that matters, because "every message the daemon composes"
// is the acceptance. So the two send paths that carry the brief in production
// (the spawn opening prompt and the first agent_send injection) are driven
// end-to-end through the seams, and each runs a CONTROL arm that composes the
// message the way the pre-fix tree did. The control is asserted to fail the
// same predicate the product arm passes: if a later change makes the
// header-free message satisfy it, this suite has stopped testing what it says.

// t425Sender is agentSender, recording what actually went on the wire. Local to
// this suite on purpose: an oracle that borrows a fixture from another target's
// test file goes red when that target is revised, and this one has to keep
// working on its own.
type t425Sender struct {
	mu   sync.Mutex
	sent []string
}

func (r *t425Sender) Alive() bool { return true }

func (r *t425Sender) Send(text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, text)
	return nil
}

func (r *t425Sender) Interrupt() error { return nil }

func (r *t425Sender) delivered() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.sent...)
}

// t425Fixture is a daemon with no provider process: registry rows are real,
// the sender and the turn witness are seams.
func t425Fixture(t *testing.T, defs ...claudia.AgentDef) (*Server, *t425Sender) {
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
	s := &Server{registry: reg, fleetBriefed: map[string]bool{}}
	sender := &t425Sender{}
	s.SetSenderResolver(func(string) (agentSender, bool, error) { return sender, false, nil })
	s.SetTurnWitness(func(_, _ string) turnWatch {
		return func() TurnEvidence {
			return TurnEvidence{Observed: true, Durable: true, PayloadSeen: true}
		}
	})
	return s, sender
}

// namesItsRecipient is the predicate the acceptance states: the message opens
// with the identity header and all five registry fields are present with a
// value. Returned as a reason so the control arm can report WHICH part the
// pre-fix message lacked rather than merely that it lacked something.
func namesItsRecipient(msg string, want AgentIdentity) string {
	if !strings.HasPrefix(strings.TrimSpace(msg), IdentityHeaderMarker) {
		return "does not open with the identity header"
	}
	for _, f := range []struct{ label, value string }{
		{"NAME", want.Name},
		{"ROLE", want.Role},
		{"PARENT", want.Parent},
		{"WORKDIR", want.WorkDir},
		{"TARGET", want.TargetID},
	} {
		if f.value == "" {
			continue
		}
		if !strings.Contains(msg, "- "+f.label+": ") {
			return "missing the " + f.label + " label"
		}
		if !strings.Contains(msg, f.value) {
			return "missing the " + f.label + " value " + f.value
		}
	}
	return ""
}

// Acceptance 1 + 4, on the spawn opening prompt: the first thing a seat ever
// reads, and the one message where "who am I" is least answerable from
// anything already in the session.
func TestT425SpawnOpeningPromptNamesItsRecipient(t *testing.T) {
	const name = "bullseye-po"
	def := claudia.AgentDef{
		Name:     name,
		Parent:   "jevons",
		Purpose:  claudia.PurposeWork, // the live registry's answer for every PO
		TargetID: "T425",
	}

	for _, tc := range []struct {
		arm     string
		product bool
	}{
		// The product: deliverStartPrompt composes brief + header.
		{"spawn prompt composed by the daemon", true},
		// The control: the pre-fix tree — standing brief, no header.
		{"pre-fix composition, brief only", false},
	} {
		t.Run(tc.arm, func(t *testing.T) {
			s, sender := t425Fixture(t, def)
			d := s.registry.Def(name)
			if d == nil {
				t.Fatal("fixture row missing")
			}
			want := IdentityFromDef(*d)

			if tc.product {
				if err := s.deliverStartPrompt(name, "Take 🎯T425."); err != nil {
					t.Fatalf("deliverStartPrompt: %v", err)
				}
			} else {
				text, injected := EnsureFleetBrief(s.fleetBriefed, name, "Take 🎯T425.")
				if !injected {
					t.Fatal("control arm did not inject the standing brief")
				}
				if _, err := deliverToSender(s, name, text, false, sender, false); err != nil {
					t.Fatalf("control send: %v", err)
				}
			}

			got := sender.delivered()
			if len(got) != 1 {
				t.Fatalf("sent %d messages, want 1: %q", len(got), got)
			}
			msg := got[0]

			if !strings.Contains(msg, "Jevons fleet standing brief") {
				t.Fatal("the standing brief itself did not go out — both arms must carry it")
			}
			if !strings.Contains(msg, "Take 🎯T425.") {
				t.Fatal("the caller's own prompt was lost")
			}

			reason := namesItsRecipient(msg, want)
			if tc.product {
				if reason != "" {
					t.Fatalf("spawn prompt %s\n---\n%s", reason, msg)
				}
			} else if reason == "" {
				t.Fatalf("the pre-fix composition satisfies the acceptance — this control no longer detects the regression:\n%s", msg)
			}

			if !tc.product {
				return
			}

			// Acceptance 4: the role rules are addressed to this reader, not
			// tabulated for it to self-match against. bullseye-po carries
			// purpose=work in the registry, so a header that trusted Purpose
			// would hand it the work-agent paragraph — the same wrong answer
			// it gave itself.
			if !strings.Contains(msg, "You are "+name+", a product owner") {
				t.Fatalf("role doctrine is not addressed to the recipient:\n%s", msg)
			}
			for _, rule := range []string{"🎯T125", "🎯T129", "🎯T155", "🎯T193", "🎯T325.1"} {
				if !strings.Contains(msg, rule) {
					t.Fatalf("PO header omits %s:\n%s", rule, msg)
				}
			}
			// The header precedes the brief: a reader who stops early still
			// knows which rows below are its own.
			if strings.Index(msg, IdentityHeaderMarker) > strings.Index(msg, "Jevons fleet standing brief") {
				t.Fatal("identity header lands below the brief it is meant to address")
			}
		})
	}
}

// Acceptance 1, on the other brief-carrying path: the first agent_send to a
// seat that was started without an opening prompt.
func TestT425FirstSendInjectionNamesItsRecipient(t *testing.T) {
	const name = "jv-t425-identity"
	def := claudia.AgentDef{Name: name, Parent: "jevons-po", Purpose: claudia.PurposeWork, TargetID: "T425"}
	s, sender := t425Fixture(t, def)

	// The composition handleAgentSend performs on first send.
	s.mu.Lock()
	text, injected := EnsureFleetBrief(s.fleetBriefed, name, "status?")
	s.mu.Unlock()
	if !injected {
		t.Fatal("first send did not inject the standing brief")
	}
	text = s.withIdentity(name, text)
	if _, err := deliverToSender(s, name, text, false, sender, false); err != nil {
		t.Fatalf("send: %v", err)
	}

	got := sender.delivered()
	if len(got) != 1 {
		t.Fatalf("sent %d messages, want 1", len(got))
	}
	if reason := namesItsRecipient(got[0], IdentityFromDef(*s.registry.Def(name))); reason != "" {
		t.Fatalf("first-send brief %s\n---\n%s", reason, got[0])
	}
	// A work agent must be told that 🎯T125 is NOT its rule: the inverse error
	// (a worker refusing to implement because the brief said owners don't) is
	// the same defect read the other way.
	if !strings.Contains(got[0], "You are "+name+", a work agent") {
		t.Fatalf("work agent not addressed as one:\n%s", got[0])
	}
	if !strings.Contains(got[0], "🎯T125 is NOT yours") {
		t.Fatalf("work agent not told which rules are not its own:\n%s", got[0])
	}
}

// Acceptance 1, on the automated pressure paths: idle nudge, worker-idle,
// daemon-restarted and fleet-recover all send through sendDaemonComposed, and
// the header must not displace the [event: …] wire prefix that lets a worker
// tell automated pressure from owner chat.
func TestT425DaemonComposedEventCarriesHeaderAndWireFormat(t *testing.T) {
	const name = "jv-t425-nudge"
	s, sender := t425Fixture(t, claudia.AgentDef{Name: name, Parent: "jevons-po", Purpose: claudia.PurposeWork})

	body := formatIdleNudgeWire(eventWorkerIdle, "your child went idle")
	if _, err := s.sendDaemonComposed(name, body, false); err != nil {
		t.Fatalf("sendDaemonComposed: %v", err)
	}
	got := sender.delivered()
	if len(got) != 1 {
		t.Fatalf("sent %d messages, want 1", len(got))
	}
	if reason := namesItsRecipient(got[0], IdentityFromDef(*s.registry.Def(name))); reason != "" {
		t.Fatalf("daemon-composed event %s\n---\n%s", reason, got[0])
	}
	if !strings.Contains(got[0], "[event: "+eventWorkerIdle+"]") {
		t.Fatalf("wire prefix lost:\n%s", got[0])
	}

	// Idempotent: a composer that already applied the header, wrapped by a
	// caller that also does, must not stack two identities.
	twice := s.withIdentity(name, s.withIdentity(name, body))
	if n := strings.Count(twice, IdentityHeaderMarker); n != 1 {
		t.Fatalf("header applied %d times, want 1", n)
	}
}

// Acceptance 3: the header is the registry's answer, read at send time — so a
// re-minted session still reports the same durable name, and an unregistered
// recipient gets no identity rather than a fabricated one.
func TestT425HeaderIsReadFromTheRegistryAtSendTime(t *testing.T) {
	const name = "jv-t425-rotate"
	s, _ := t425Fixture(t, claudia.AgentDef{Name: name, Parent: "jevons-po", Purpose: claudia.PurposeWork, TargetID: "T425"})

	before := s.identityHeaderFor(name)
	if before == "" {
		t.Fatal("no header for a registered row")
	}

	// A context-ceiling rotation re-mints the session. Name, parent, workdir
	// and bound target do not move — and neither may the header.
	d := *s.registry.Def(name)
	d.SessionID = "s-rotated"
	if err := s.registry.Register(d); err != nil {
		t.Fatal(err)
	}
	if after := s.identityHeaderFor(name); after != before {
		t.Fatalf("rotation changed the reported identity:\nbefore:\n%s\nafter:\n%s", before, after)
	}

	// Late-bound target: the header reports what the row says NOW, not what it
	// said when the seat was created.
	d.TargetID = "T427"
	if err := s.registry.Register(d); err != nil {
		t.Fatal(err)
	}
	if h := s.identityHeaderFor(name); !strings.Contains(h, "🎯T427") {
		t.Fatalf("header did not re-read the row: %s", h)
	}

	// Unregistered: no row, no identity, message unchanged.
	if h := s.identityHeaderFor("nobody-here"); h != "" {
		t.Fatalf("fabricated an identity for an unregistered name: %s", h)
	}
	if out := s.withIdentity("nobody-here", "plain text"); out != "plain text" {
		t.Fatalf("unregistered recipient's message was altered: %q", out)
	}
}

// Acceptance 5, second clause: a row with no parent and no target renders
// coherently rather than emitting a label with nothing after it.
func TestT425SparseRowRendersCoherently(t *testing.T) {
	h := FormatIdentityHeader(AgentIdentity{Name: "jevons", Role: RoleOverseer, Purpose: claudia.PurposeOverseer})
	if h == "" {
		t.Fatal("no header for a valid sparse row")
	}
	for line := range strings.SplitSeq(h, "\n") {
		if strings.HasPrefix(line, "- ") && strings.HasSuffix(strings.TrimSpace(line), ":") {
			t.Fatalf("empty label rendered: %q", line)
		}
	}
	if !strings.Contains(h, "- PARENT: (none — root agent)") {
		t.Fatalf("root agent's absent parent not rendered as an absence:\n%s", h)
	}
	if !strings.Contains(h, "- TARGET: (none bound)") {
		t.Fatalf("unbound target not rendered as an absence:\n%s", h)
	}
	if strings.Contains(h, "- WORKDIR:") {
		t.Fatalf("workdir label emitted with no workdir:\n%s", h)
	}
	if !strings.Contains(h, "You are jevons, the overseer") {
		t.Fatalf("overseer not addressed as one:\n%s", h)
	}

	// A name is the one field the header cannot do without: it is the answer
	// the whole target exists to supply, and guessing at it is worse than
	// staying silent.
	if h := FormatIdentityHeader(AgentIdentity{Parent: "jevons", WorkDir: "/tmp"}); h != "" {
		t.Fatalf("header composed without a name: %s", h)
	}
}

// Acceptance 4's derivation, stated as a table because the failure mode is a
// role read off the wrong field: every PO in the live registry carries
// purpose=work, so Purpose alone reproduces bullseye-po's own wrong answer.
func TestT425DeriveAgentRole(t *testing.T) {
	for _, tc := range []struct {
		name, purpose, want string
	}{
		{"jevons", claudia.PurposeOverseer, RoleOverseer},
		{"jevons-po", claudia.PurposeWork, RoleProductOwner},
		{"bullseye-po", claudia.PurposeWork, RoleProductOwner},
		{"bullseye_po", "", RoleProductOwner},
		{"jv-t425-identity", claudia.PurposeWork, RoleWork},
		{"ask-about-costs", claudia.PurposeAside, RoleAside},
		// purpose=overseer wins over a -po suffix: an explicit role beats a
		// naming heuristic.
		{"weird-po", claudia.PurposeOverseer, RoleOverseer},
	} {
		if got := DeriveAgentRole(tc.name, tc.purpose, ""); got != tc.want {
			t.Errorf("DeriveAgentRole(%q, %q) = %q, want %q", tc.name, tc.purpose, got, tc.want)
		}
	}
}

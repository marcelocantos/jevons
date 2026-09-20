// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"strings"
	"testing"
	"time"

	"github.com/marcelocantos/jevons/internal/fleetlog"
)

// 🎯T717 — a once-per-X notice is not collapsed into never by the overseer
// replay digest.
//
// T706 found this: the emit-side seam released exactly one notice per hold,
// but the second hold rendered byte-identical to the first, so deliverByName
// logged status=suppressed_replay and the overseer never saw it. These
// oracles count what reached the overseer inbox (and the send status), not
// what the emitter thinks it sent.

const t717StableLine = "Held backlog on reaped agent \"jv-t717\": 1 message(s) waiting 10m0s (killed). " +
	"Recover with jevons_agent_start. This notice is sent once per held backlog."

func TestMixFleetHealthOccurrence(t *testing.T) {
	t.Parallel()
	if got := mixFleetHealthOccurrence(t717StableLine, "entry-aaaa"); !strings.Contains(got, "entry-aaaa") {
		t.Fatalf("occurrence missing from wire: %q", got)
	}
	already := t717StableLine + " hold entry-aaaa"
	if got := mixFleetHealthOccurrence(already, "entry-aaaa"); got != strings.TrimSpace(already) {
		t.Fatalf("already-present occurrence was appended again: %q", got)
	}
	if got := mixFleetHealthOccurrence(t717StableLine, ""); got != "" {
		t.Fatalf("empty occurrence must refuse, got %q", got)
	}
	if got := mixFleetHealthOccurrence("", "entry-aaaa"); got != "" {
		t.Fatalf("empty line must refuse, got %q", got)
	}
}

func TestT717SecondOccurrenceReachesOverseer(t *testing.T) {
	s, inbox := t428Server(t, TurnEvidence{Observed: true, PayloadSeen: true})

	s.notifyFleetHealth("entry-aaaa", t717StableLine)
	s.notifyFleetHealth("entry-bbbb", t717StableLine)
	if len(inbox.texts) != 2 {
		t.Fatalf("overseer received %d notices; want 2 distinct occurrences\n%v",
			len(inbox.texts), inbox.texts)
	}
	if inbox.texts[0] == inbox.texts[1] {
		t.Fatal("the two delivered batches are byte-identical — occurrence was not mixed into the wire")
	}

	// Name the status, not only the inbox: the original bug was
	// status=suppressed_replay with a nil error.
	res, err := s.deliverByName("jevons", inbox.texts[1], OriginAgent, false)
	if err != nil {
		t.Fatalf("re-offer of the second batch: %v", err)
	}
	if res.Status != StatusSuppressedReplay {
		t.Fatalf("re-offer of occurrence B status=%q want %s (echo of the same occurrence)",
			res.Status, StatusSuppressedReplay)
	}
}

func TestT717SameOccurrenceStillSuppressed(t *testing.T) {
	s, inbox := t428Server(t, TurnEvidence{Observed: true, PayloadSeen: true})

	s.notifyFleetHealth("entry-aaaa", t717StableLine)
	s.notifyFleetHealth("entry-aaaa", t717StableLine)
	if len(inbox.texts) != 1 {
		t.Fatalf("overseer received %d copies of one occurrence; want 1 (🎯T428 still holds)\n%v",
			len(inbox.texts), inbox.texts)
	}
	wire := "[Fleet health] " + mixFleetHealthOccurrence(t717StableLine, "entry-aaaa")
	res, err := s.deliverByName("jevons", wire, OriginAgent, false)
	if err != nil {
		t.Fatalf("echo: %v", err)
	}
	if res.Status != StatusSuppressedReplay {
		t.Fatalf("echo status=%q want %s", res.Status, StatusSuppressedReplay)
	}
}

func TestT717EmptyOccurrenceDoesNotSend(t *testing.T) {
	s, inbox := t428Server(t, TurnEvidence{Observed: true, PayloadSeen: true})
	s.notifyFleetHealth("", t717StableLine)
	s.notifyFleetHealth("entry-aaaa", "")
	if len(inbox.texts) != 0 {
		t.Fatalf("empty occurrence or line reached the overseer: %v", inbox.texts)
	}
}

// T568 durable evidence keys on payload bytes. A metadata-only occurrence
// key would still die after bounce; mixing the occurrence into the wire is
// what lets a second hold survive a restart.
func TestT717BounceDoesNotCollapseNewOccurrence(t *testing.T) {
	dir := t.TempDir()
	first := &overseerInbox{}
	s1 := t568Server(t, dir, first)
	s1.notifyFleetHealth("entry-aaaa", t717StableLine)
	if len(first.texts) != 1 {
		t.Fatalf("first process inbox=%v want 1", first.texts)
	}

	second := &overseerInbox{}
	s2 := t568Server(t, dir, second)
	s2.notifyFleetHealth("entry-bbbb", t717StableLine)
	if len(second.texts) != 1 {
		t.Fatalf("post-restart occurrence B suppressed (got %d); T568 must not collapse a new occurrence\n%v",
			len(second.texts), second.texts)
	}
	res, err := s2.deliverByName("jevons", second.texts[0], OriginAgent, false)
	if err != nil {
		t.Fatalf("echo of B after bounce: %v", err)
	}
	if res.Status != StatusSuppressedReplay {
		t.Fatalf("echo of B status=%q want %s", res.Status, StatusSuppressedReplay)
	}
}

// Product path: T582's routed finished-seat notice did not name the hold, so
// a second hold on the same name rendered byte-identical and T428 swallowed
// it. Occurrence is now the hold head id, mixed into the wire.
func TestT717SecondRoutedHoldReachesOverseer(t *testing.T) {
	const agent, parent = "jv-t717-replay-digest", "jevons-po"
	f := t582Server(t, agent, parent, fleetlog.ReasonReapDone)
	q := f.s.sendQueue()

	if _, _, err := q.Append(agent, "first follow-up for the parent",
		f.now.Add(-StalledBacklogAfter-time.Minute)); err != nil {
		t.Fatal(err)
	}
	f.sweepFor(2 * time.Minute)
	if got := heldReapedNotices(f.up.all()); got != 1 {
		t.Fatalf("first hold notices = %d; want 1\n%s", got, strings.Join(f.up.all(), "\n"))
	}

	if _, _, err := q.Append(agent, "second, unrelated follow-up",
		f.now.Add(-StalledBacklogAfter-time.Minute)); err != nil {
		t.Fatal(err)
	}
	f.sweepFor(2 * time.Minute)
	if got := heldReapedNotices(f.up.all()); got != 2 {
		t.Fatalf("notices after a second routed hold = %d; want 2 (one per hold, delivered not merely emitted)\n%s",
			got, strings.Join(f.up.all(), "\n"))
	}
	if f.up.all()[0] == f.up.all()[1] {
		t.Fatal("the two overseer notices are byte-identical — the second hold was an echo of the first")
	}
}

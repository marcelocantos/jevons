// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package sendq

import (
	"fmt"
	"strings"
	"time"
)

// 🎯T726 — AN UNCERTAIN ENTRY IS RESOLVED BY A TOOL, NOT BY AN EDITOR.
//
// 🎯T623 made the daemon refuse to replay an attempt whose outcome it does not
// know, and 🎯T599 made the seat holding one say PINNED instead of looking
// busy. Both are right, and together they left no legal move. On 2026-09-20
// claudia-po sat pinned on message e81b80f752e0 (attempt 4da672f24905) from
// the first fleet listing of the session to near its end; the notice's own
// advice was "reconcile delivery before another send", and no tool was named
// for reconciling. When the same hold blocked jv-t718-gate-dirty-warn from
// hearing its own PO, jevons_agent_kill refused and offered the one escape a
// product owner had: ask the overseer to throw the messages away.
//
// The missing verb is not another automatic policy. The daemon genuinely
// cannot tell, from inside, whether a submission that errored reached the
// receiver — that is why the entry is uncertain. Somebody has to go and look,
// with 🎯T416's three working instruments (payload-match at user-message level
// in the receiver's JSONL, the receiver's own queue-operation records, and
// transcript-file absence), and then WRITE DOWN what they established.
//
// So reconciliation is an operator judgement carried by an API:
//
//   - it names the entry AND its attempt, so a disposition cannot land on a
//     view of the queue that has since moved;
//   - it refuses an entry whose attempt this process still actively owns — a
//     healthy in-flight submission is not an orphan (🎯T623);
//   - it demands an ACTOR and EVIDENCE for every outcome, because the failure
//     it replaces is a hand-edit that leaves no trace of who decided what;
//   - it returns the payload it removed, in full, so no caller can drop an
//     unconfirmed message without something to record.
//
// The alternative that was actually proposed — an editor on
// ~/.jevons/sendq/*.json under a running daemon — loses the write or corrupts
// the queue, and the house rule for durable state (atomic write-and-rename,
// malformed is a hard error) exists precisely so that nobody has to reason
// about which.

// ReconcileOutcome is what an operator ESTABLISHED about a held entry by
// looking, not what the transport implied. The three answers are the three
// 🎯T416 verdicts a human can actually reach.
type ReconcileOutcome string

const (
	// ReconcileConfirmed: the receiver has it. The obligation is discharged
	// and the payload leaves the queue.
	ReconcileConfirmed ReconcileOutcome = "confirmed"
	// ReconcileRequeue: it never landed. The entry returns to Pending and the
	// ordinary drain may offer it again — the one outcome that permits a
	// resend, and only on established non-delivery (🎯T416).
	ReconcileRequeue ReconcileOutcome = "requeue"
	// ReconcileDrop: it may or may not have landed and will not be pursued.
	// A deliberate, attributed abandonment — never a silent one.
	ReconcileDrop ReconcileOutcome = "drop"
	// ReconcileConsolidate marks the survivor of a supersession.
	ReconcileConsolidate ReconcileOutcome = "consolidate"
)

// Reconciliation is the durable record of who decided and on what evidence.
// It rides the entry when the entry survives; the caller records it for the
// entries that do not.
type Reconciliation struct {
	Actor    string           `json:"actor"`
	At       time.Time        `json:"at"`
	Outcome  ReconcileOutcome `json:"outcome"`
	Evidence string           `json:"evidence"`
}

// Describe is the one-liner a log record or a tool reply quotes.
func (r Reconciliation) Describe() string {
	if r.Actor == "" {
		return ""
	}
	return fmt.Sprintf("%s by %s: %s", r.Outcome, r.Actor, r.Evidence)
}

// Receipt is what a reconciliation did. Entries carries the payloads that left
// the queue, in full: a caller that drops a message must have the message.
type Receipt struct {
	Agent   string
	By      Reconciliation
	Removed []Entry
	Kept    []Entry
	Depth   int
}

// RemovedIDs is the ids of the entries this receipt took off the queue.
func (r Receipt) RemovedIDs() []string {
	out := make([]string, 0, len(r.Removed))
	for _, e := range r.Removed {
		out = append(out, e.ID)
	}
	return out
}

// RemovedBytes is the total payload this receipt is accountable for.
func (r Receipt) RemovedBytes() int {
	n := 0
	for _, e := range r.Removed {
		n += len(e.Text)
	}
	return n
}

// validAttribution refuses an anonymous or unevidenced disposition. The whole
// point of the tool is that the record survives the decision; a blank actor or
// a blank evidence line is the hand-edit it replaces, wearing an API.
func validAttribution(actor, evidence string) (string, string, error) {
	actor, evidence = strings.TrimSpace(actor), strings.TrimSpace(evidence)
	if actor == "" {
		return "", "", fmt.Errorf("sendq: reconcile requires an actor (who established this)")
	}
	if evidence == "" {
		return "", "", fmt.Errorf("sendq: reconcile requires evidence (what you observed; 🎯T416 names the three instruments that work)")
	}
	return actor, evidence, nil
}

// Reconcile resolves one held entry by operator judgement.
//
// entryID is required and attemptID must match whenever the entry carries one:
// a disposition is a claim about a specific delivery, and the queue may have
// moved since the caller looked at it.
func (s *Store) Reconcile(agent, entryID, attemptID string, outcome ReconcileOutcome, actor, evidence string, now time.Time) (Receipt, error) {
	if s == nil {
		return Receipt{}, fmt.Errorf("sendq: no store")
	}
	switch outcome {
	case ReconcileConfirmed, ReconcileRequeue, ReconcileDrop:
	default:
		return Receipt{}, fmt.Errorf("sendq: invalid reconcile outcome %q (want confirmed, requeue or drop)", outcome)
	}
	actor, evidence, err := validAttribution(actor, evidence)
	if err != nil {
		return Receipt{}, err
	}
	entryID = strings.TrimSpace(entryID)
	if entryID == "" {
		return Receipt{}, fmt.Errorf("sendq: reconcile requires an entry id")
	}
	attemptID = strings.TrimSpace(attemptID)

	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load(agent)
	if err != nil {
		return Receipt{}, err
	}
	idx := -1
	for i, e := range f.Entries {
		if e.ID == entryID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Receipt{}, fmt.Errorf("sendq: %q holds no entry %s (it may already have been reconciled or delivered)", agent, entryID)
	}
	e := f.Entries[idx]
	if e.AttemptID != "" && attemptID != "" && attemptID != e.AttemptID {
		return Receipt{}, fmt.Errorf("sendq: entry %s now carries attempt %s, not %s — re-read the queue before reconciling",
			e.ID, e.AttemptID, attemptID)
	}
	if e.State != Pending && attemptID == "" {
		return Receipt{}, fmt.Errorf("sendq: entry %s has an unresolved %s attempt %s; name it as attempt_id so the disposition cannot land on a stale view",
			e.ID, e.State, e.AttemptID)
	}
	// A submission this process is still inside is not an orphan (🎯T623): the
	// drain that owns it will resolve it, and reconciling it here would race a
	// live provider call.
	if e.State == Attempting && s.active[agent] == e.AttemptID {
		return Receipt{}, fmt.Errorf("sendq: entry %s is an ACTIVE submission owned by this daemon, not an orphan — wait for it to resolve", e.ID)
	}
	if outcome == ReconcileRequeue && e.State == Pending {
		return Receipt{}, fmt.Errorf("sendq: entry %s is already pending; nothing to requeue", e.ID)
	}

	by := Reconciliation{Actor: actor, At: now.UTC(), Outcome: outcome, Evidence: evidence}
	rec := Receipt{Agent: strings.TrimSpace(agent), By: by}
	if outcome == ReconcileRequeue {
		e.State, e.AttemptID, e.Detail = Pending, "", ""
		e.Reconciled = &by
		f.Entries[idx] = e
		rec.Kept = []Entry{e}
	} else {
		rec.Removed = []Entry{e}
		f.Entries = append(append([]Entry(nil), f.Entries[:idx]...), f.Entries[idx+1:]...)
	}
	if err := s.save(f); err != nil {
		return Receipt{}, err
	}
	if s.active[agent] == e.AttemptID && e.AttemptID != "" {
		delete(s.active, agent)
	}
	rec.Depth = len(f.Entries)
	return rec, nil
}

// Consolidate collapses a queue of superseded messages into one authoritative
// message, so a seat that fell behind does not wake to three versions of the
// same instruction and act on the stalest.
//
// It refuses while ANY entry is unresolved. Removing an entry whose delivery
// outcome is unknown is a claim of non-delivery, and this call has no evidence
// for one — reconcile the attempt first, which is exactly the order the
// ge-t191-ndk-discovery queue needed (confirm 3eeb59a6, then fold the rest).
//
// keepID names the survivor (default: the newest). text replaces its payload
// with an operator-authored message; empty keeps what was accepted. The
// survivor inherits the OLDEST acceptance time in the set, because that is the
// number a stall is visible in and consolidation must not reset it.
func (s *Store) Consolidate(agent, keepID, text, actor, evidence string, now time.Time) (Receipt, error) {
	if s == nil {
		return Receipt{}, fmt.Errorf("sendq: no store")
	}
	actor, evidence, err := validAttribution(actor, evidence)
	if err != nil {
		return Receipt{}, err
	}
	keepID = strings.TrimSpace(keepID)

	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := s.load(agent)
	if err != nil {
		return Receipt{}, err
	}
	if len(f.Entries) < 2 {
		return Receipt{}, fmt.Errorf("sendq: %q holds %d message(s); consolidation needs at least two", agent, len(f.Entries))
	}
	for _, e := range f.Entries {
		// An admitted owner question is not another revision of an
		// instruction. Even an operator-authorized consolidation cannot
		// replace its host identity with the surviving entry's identity.
		if e.RequestID != "" {
			return Receipt{}, fmt.Errorf("sendq: request_id %q on entry %s cannot be consolidated", e.RequestID, e.ID)
		}
		if e.State != Pending {
			return Receipt{}, fmt.Errorf(
				"sendq: entry %s has an unresolved %s attempt %s — reconcile it before consolidating; "+
					"folding it away would claim non-delivery this call has no evidence for (🎯T416)",
				e.ID, e.State, e.AttemptID)
		}
	}
	idx := len(f.Entries) - 1
	if keepID != "" {
		idx = -1
		for i, e := range f.Entries {
			if e.ID == keepID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return Receipt{}, fmt.Errorf("sendq: %q holds no entry %s to keep", agent, keepID)
		}
	}

	by := Reconciliation{Actor: actor, At: now.UTC(), Outcome: ReconcileConsolidate, Evidence: evidence}
	survivor := f.Entries[idx]
	oldest := survivor.EnqueuedAt
	rec := Receipt{Agent: strings.TrimSpace(agent), By: by}
	for i, e := range f.Entries {
		if i == idx {
			continue
		}
		if e.EnqueuedAt.Before(oldest) {
			oldest = e.EnqueuedAt
		}
		rec.Removed = append(rec.Removed, e)
	}
	if t := strings.TrimSpace(text); t != "" {
		survivor.Text = t
	}
	survivor.EnqueuedAt = oldest
	survivor.Reconciled = &by
	f.Entries = []Entry{survivor}
	if err := s.save(f); err != nil {
		return Receipt{}, err
	}
	rec.Kept = []Entry{survivor}
	rec.Depth = 1
	return rec, nil
}

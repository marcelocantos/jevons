// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

// 🎯T428 — THE OVERSEER'S NOTIFICATION CHANNEL DOES NOT REPLAY A BATCH IT HAS
// ALREADY BEEN GIVEN.
//
// Observed twice on 2026-08-10 by two different overseer instances. One
// recorded the same bullseye-po completion report re-surfaced five times with
// no new content, and spent a turn re-reading it each time. In session
// ca276b85 one batch — sentinel snapshot + a closed jv-t370-auto impatience
// incident + RSI coach judgment fingerprint 44c736552294ab9e02e8c3a01dcca666 —
// was delivered four times BYTE-IDENTICALLY inside a single running turn,
// including after the overseer had recorded disposition=act_other for that
// exact fingerprint.
//
// The cost is not the wasted tokens. It is that the overseer cannot tell
// "this is still happening" from "you are being shown this again", which is
// the property that makes a health channel worth ignoring — and once it is
// ignored, every escalation in the fleet routes into a channel nobody reads.
// Worse than waste: a replayed sentinel snapshot RE-ASSERTS a stale phase
// reading (🎯T423 — jv-t374-script-abort-root was classed idle-past-grace
// while its session file was growing, mid-tool-use, 127 lines). Replay does
// not merely cost attention, it re-injects readings already known false.
//
// WHERE THE GUARD SITS. deliverToOverseer is the one arm of deliverByName
// every notification reaches (🎯T309.3), whichever door its source came
// through — sentinel cycle, RSI coach, fleet health, worker report notify.
// Deduping at each source would be four guards with four sets of state and
// four ways to be forgotten by the fifth source; the channel is one place, so
// the guard is one place.
//
// jevons_event_push is the exception, and it is why the ledger is on the
// server rather than inside deliverToOverseer: butler.PushEvent reaches the
// agent process directly and never passes through deliverByName at all. So
// that tool offers to the SAME ledger with the SAME wire bytes
// (butler.FormatEventPush), which is what makes a coach judgment that arrives
// once by send and once by push fallback count as one batch rather than two.
// Two call sites, one piece of state — the property that matters is a single
// ledger, not a single line of code.
//
// WHAT COUNTS AS THE SAME BATCH. The literal bytes, after trimming. Not a
// similarity score, not a summary, not a per-source key: two batches that
// differ anywhere differ, because the whole promise being made to the overseer
// is that what it is shown is content it has not seen. A recurring condition
// whose report carries a count or a timestamp is new content and gets through
// on its own merits — that is the channel reporting a CHANGE, which is what a
// health channel is for.
//
// WHY SUPPRESSION IS NOT ON A TIMER. A timer is precisely the "you are being
// shown this again" generator: it re-asserts identical bytes on a schedule
// that has nothing to do with whether anything changed. So there is no expiry
// clock here at all. The ledger is bounded by SIZE (notifyReplayLedgerLimit),
// and eviction is the only way a sealed batch becomes deliverable again —
// which fails in the safe direction, re-reporting a stale condition rather
// than going permanently silent about a live one.
//
// WHAT SUPPRESSION IS BUILT ON — EVIDENCE THAT THE RECEIVER HAS IT, never this
// process's belief that it sent something. A batch is sealed only when the
// overseer's own records show it arrived: a user message carrying it, a queue
// record draining it into the running turn, or an enqueue record holding it
// behind one (🎯T416 / 🎯T429). Absence of evidence seals nothing — an
// unconfirmed delivery stays retryable after notifyReplayUnconfirmedGrace,
// because the failure mode of over-sealing is a lost escalation and the
// failure mode of under-sealing is one extra copy.
//
// OWNER TURNS ARE NOT NOTIFICATIONS. Only agent-origin text is deduped. The
// owner may say the same thing twice and mean it twice, and swallowing an
// owner turn because it matched an earlier one would be a far worse defect
// than the one being fixed.

// StatusSuppressedReplay is the send status for a batch the channel refused
// because the overseer already has that exact content. It is a SUCCESS status
// with a nil error, deliberately: callers such as deliverSentinel treat an
// error as "this channel is broken" and fall back to butler.PushEvent, which
// would deliver the very copy this guard exists to withhold.
const StatusSuppressedReplay = "suppressed_replay"

const (
	// notifyReplayLedgerLimit bounds how many distinct batches the channel
	// remembers. Eviction is by least-recently-offered.
	notifyReplayLedgerLimit = 512

	// notifyReplayUnconfirmedGrace is how long an UNCONFIRMED delivery holds
	// the channel against an identical retry. It is not a suppression window —
	// sealed batches never expire — it is the interval during which a payload
	// whose fate is unknown is presumed in flight rather than lost. It sits
	// above the turn-confirmation window (🎯T416) so a slow receiver is not
	// re-pushed while the first copy is still being read, and well below any
	// interval on which a genuinely lost escalation matters.
	notifyReplayUnconfirmedGrace = 10 * time.Minute
)

// notifyReplayState is what the channel knows about one batch.
type notifyReplayState int

const (
	// replayInFlight: a delivery of this batch is being attempted right now.
	replayInFlight notifyReplayState = iota
	// replayHeld: the receiver's own records show it has this batch. Sealed.
	replayHeld
	// replayUnconfirmed: the seam accepted it and nothing confirmed arrival.
	replayUnconfirmed
)

// notifyReplayEntry is the ledger row for one batch digest.
type notifyReplayEntry struct {
	state     notifyReplayState
	firstAt   time.Time
	lastAt    time.Time
	settledAt time.Time
	// offers counts every time this batch was presented to the channel,
	// including the refused ones. It is the replay count the operator wants.
	offers int
	// delivered counts how many copies actually reached the seam.
	delivered int
}

// notifyReplayDecision is the channel's answer for one offered batch.
type notifyReplayDecision struct {
	// Admit says the batch may go to the delivery seam.
	Admit bool
	// Digest identifies the batch in logs.
	Digest string
	// Reason names why an inadmissible batch was refused.
	Reason string
	// Offers is how many times this batch has now been presented.
	Offers int
	// Delivered is how many copies have reached the seam.
	Delivered int
	// FirstAt is when the first copy was offered.
	FirstAt time.Time
}

// notifyReplayLedger remembers which batches the overseer already has.
type notifyReplayLedger struct {
	mu      sync.Mutex
	entries map[string]*notifyReplayEntry
	// order is least-recently-offered first; eviction pops the front.
	order []string
	limit int
	now   func() time.Time
}

func newNotifyReplayLedger(limit int, now func() time.Time) *notifyReplayLedger {
	if limit <= 0 {
		limit = notifyReplayLedgerLimit
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &notifyReplayLedger{
		entries: make(map[string]*notifyReplayEntry),
		limit:   limit,
		now:     now,
	}
}

// notifyReplays returns the server's channel ledger, creating it on first use.
func (s *Server) notifyReplays() *notifyReplayLedger {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.notifyReplay == nil {
		s.notifyReplay = newNotifyReplayLedger(notifyReplayLedgerLimit, nil)
	}
	return s.notifyReplay
}

// SetNotifyReplayClock overrides the ledger clock. Test seam: the unconfirmed
// grace is the one interval here that a hermetic oracle has to cross, and
// crossing it by sleeping would put ten minutes in the suite.
func (s *Server) SetNotifyReplayClock(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.notifyReplay == nil {
		s.notifyReplay = newNotifyReplayLedger(notifyReplayLedgerLimit, now)
		return
	}
	s.notifyReplay.mu.Lock()
	defer s.notifyReplay.mu.Unlock()
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	s.notifyReplay.now = now
}

// Now reads the ledger's clock, so a caller rendering an age uses the same
// time base the ledger decided on rather than a second one.
func (l *notifyReplayLedger) Now() time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.now()
}

// notifyBatchDigest identifies a batch by its bytes. Trimming is the only
// normalisation: a batch that differs by a trailing newline is the same batch,
// and anything beyond that starts guessing which differences the overseer
// would have considered material — which is the overseer's judgement to make,
// not this channel's.
func notifyBatchDigest(text string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(text)))
	return hex.EncodeToString(sum[:])
}

// notifyReplayTicket is the caller's handle on an admitted batch. A ticket
// MUST be settled or abandoned, or the batch stays in flight and every later
// copy is refused for a reason that has stopped being true.
type notifyReplayTicket struct {
	ledger *notifyReplayLedger
	digest string
}

// Offer presents a batch to the channel and decides whether it may go out.
//
// The returned ticket is live only when the decision admits.
func (l *notifyReplayLedger) Offer(text string) (notifyReplayTicket, notifyReplayDecision) {
	digest := notifyBatchDigest(text)
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()

	e, ok := l.entries[digest]
	if !ok {
		e = &notifyReplayEntry{state: replayInFlight, firstAt: now}
		l.entries[digest] = e
		l.touch(digest)
		e.offers = 1
		e.lastAt = now
		l.evict()
		return notifyReplayTicket{ledger: l, digest: digest},
			notifyReplayDecision{Admit: true, Digest: digest, Offers: 1, FirstAt: now}
	}

	e.offers++
	e.lastAt = now
	l.touch(digest)
	dec := notifyReplayDecision{
		Digest:    digest,
		Offers:    e.offers,
		Delivered: e.delivered,
		FirstAt:   e.firstAt,
	}
	switch e.state {
	case replayInFlight:
		// The copy ahead of this one has not come back yet. Two identical
		// batches racing into the same turn is exactly the observed specimen.
		dec.Reason = "identical batch already in flight to the overseer"
		return notifyReplayTicket{}, dec
	case replayHeld:
		dec.Reason = "the overseer's own records already show this exact batch"
		return notifyReplayTicket{}, dec
	case replayUnconfirmed:
		if now.Sub(e.settledAt) < notifyReplayUnconfirmedGrace {
			// 🎯T429: a payload whose fate is unknown is far more often
			// delivered than lost, and a re-push on one that landed stacks a
			// second copy in the composer.
			dec.Reason = fmt.Sprintf(
				"an identical batch was accepted %s ago and its arrival is still unconfirmed",
				now.Sub(e.settledAt).Round(time.Second))
			return notifyReplayTicket{}, dec
		}
		// Past the grace with no evidence it ever arrived: a retry is the
		// safer error, so the batch goes out again.
		e.state = replayInFlight
		dec.Admit = true
		return notifyReplayTicket{ledger: l, digest: digest}, dec
	}
	dec.Reason = "unknown replay state"
	return notifyReplayTicket{}, dec
}

// Settle closes an admitted batch. confirmed says the RECEIVER's own records
// showed it — not that the send call returned nil.
func (t notifyReplayTicket) Settle(confirmed bool) {
	if t.ledger == nil {
		return
	}
	t.ledger.mu.Lock()
	defer t.ledger.mu.Unlock()
	e, ok := t.ledger.entries[t.digest]
	if !ok {
		return
	}
	e.delivered++
	e.settledAt = t.ledger.now()
	if confirmed {
		e.state = replayHeld
		return
	}
	e.state = replayUnconfirmed
}

// Abandon drops an admitted batch that never reached the seam, so a genuine
// retry is not refused as a replay of a delivery that did not happen.
func (t notifyReplayTicket) Abandon() {
	if t.ledger == nil {
		return
	}
	t.ledger.mu.Lock()
	defer t.ledger.mu.Unlock()
	e, ok := t.ledger.entries[t.digest]
	if !ok {
		return
	}
	if e.delivered == 0 {
		delete(t.ledger.entries, t.digest)
		t.ledger.forget(t.digest)
		return
	}
	// An earlier copy did reach the seam, so the row still means something.
	e.state = replayUnconfirmed
	e.settledAt = t.ledger.now()
}

// touch moves a digest to the most-recently-offered end. Caller holds mu.
func (l *notifyReplayLedger) touch(digest string) {
	l.forget(digest)
	l.order = append(l.order, digest)
}

// forget removes a digest from the recency order. Caller holds mu.
func (l *notifyReplayLedger) forget(digest string) {
	for i, d := range l.order {
		if d == digest {
			l.order = append(l.order[:i], l.order[i+1:]...)
			return
		}
	}
}

// evict trims the ledger to its limit, oldest first. In-flight rows are never
// evicted: dropping one would admit the copy racing behind it, which is the
// defect. Caller holds mu.
func (l *notifyReplayLedger) evict() {
	for len(l.order) > l.limit {
		for i, d := range l.order {
			if e, ok := l.entries[d]; ok && e.state == replayInFlight {
				continue
			}
			delete(l.entries, d)
			l.order = append(l.order[:i], l.order[i+1:]...)
			break
		}
		// Nothing evictable (every row in flight) — leave the ledger over its
		// limit rather than break the in-flight guarantee.
		if len(l.order) > l.limit && l.allInFlight() {
			return
		}
	}
}

// allInFlight reports whether every remembered batch is mid-delivery.
// Caller holds mu.
func (l *notifyReplayLedger) allInFlight() bool {
	for _, e := range l.entries {
		if e.state != replayInFlight {
			return false
		}
	}
	return true
}

// describeReplaySuppression is what the caller is told instead of a delivery.
// It names the count and the age, because the operator reading this line needs
// to know whether the channel is guarding one stuck source or the whole fleet.
func describeReplaySuppression(name string, dec notifyReplayDecision, now time.Time) string {
	age := ""
	if !dec.FirstAt.IsZero() {
		age = fmt.Sprintf(", first offered %s ago", now.Sub(dec.FirstAt).Round(time.Second))
	}
	return fmt.Sprintf(
		"Not delivered to overseer %q — byte-identical to a batch the channel has already handled (🎯T428): %s. "+
			"This is offer #%d of that batch%s; %d copy/copies reached the overseer. "+
			"Nothing was lost: re-send only content the overseer has not seen.",
		name, dec.Reason, dec.Offers, age, dec.Delivered)
}

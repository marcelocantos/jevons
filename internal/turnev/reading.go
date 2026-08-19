// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package turnev

// 🎯T447 — THE INSTRUMENT SET HAD NO WAY TO OBSERVE WAITING.
//
// Every instrument 🎯T416 documented is a way of failing to observe delivery:
// the payload is not in a user message, the queue records do not carry it, the
// file does not exist. Absences. A reader who runs all of them faithfully on a
// message that was accepted and is sitting in the receiver's composer queue
// gets the same answer as one run on a message that was dropped — nothing —
// and "nothing" reads as lost.
//
// It did, on 2026-08-14, to the overseer, about its own briefs to jevons-po.
// It searched the receiver's new session for the payloads at user-message
// level, found none, read the queue records, saw `enqueue` with no matching
// user message, and concluded a rotation had destroyed them. It then re-sent
// 17KB labelled "first delivery, not a duplicate" (it was a duplicate of a
// message already answered) and reported the incident to jevons-po as a clean
// specimen of a defect class (it was correct behaviour). The daemon had
// delivered the first message in full and the later three were sitting in
// `queued_command` attachments, accepted and waiting, exactly as designed.
//
// So the file gains its POSITIVE test. A `queued_command` attachment carrying
// the payload is the artefact that identifies a message as accepted-and-
// waiting; combined with the user-message match it makes a reading with three
// outcomes rather than two, and the third one is the one that was missing:
//
//	an authored user message carries it        ⇒ DELIVERED
//	a queue record or attachment carries it    ⇒ IN FLIGHT — the receiver has it
//	neither, past a boundary                   ⇒ LOST
//
// WHY IN-FLIGHT COVERS BOTH QUEUE FATES, which is the one judgement call here.
// FateQueued (an enqueue with no drain yet) and FateEnteredTurn (a drain, or
// the attachment the CLI writes when the queued message is replayed into the
// turn) differ in how far the message has got INSIDE the receiver. They do not
// differ at all in what a reader diagnosing a loss must conclude — the
// receiver holds it, nobody may re-send it, and waiting is the correct action.
// Reading answers the loss question and deliberately stops there; Delivered()
// remains the finer answer for the send path, which needs to know whether a
// turn began. Two questions, one decoder, no second opinion.

// Reading is the three-outcome answer to "what became of this message", for a
// reader deciding whether anything was lost. It is derived from Fate rather
// than measured separately: a second instrument for the same fact is a second
// answer waiting to disagree with the first.
type Reading int

const (
	// ReadingUndecided: nothing was read, so there is no finding in either
	// direction. Distinct from ReadingLost for the reason FateUnknown is
	// distinct from FateUnseen — "I stopped looking" is not "it is not there",
	// and a reader handed the first as the second manufactures a loss out of
	// its own failure to look.
	ReadingUndecided Reading = iota
	// ReadingLost: the region was read to its end and no record carries the
	// payload — not a user message, not a queue record, not an attachment.
	// The only reading on which a re-send is even a candidate, and only past a
	// boundary (🎯T417: the same absence is what a mid-turn read looks like).
	ReadingLost
	// ReadingInFlight: the receiver has the message and no turn has begun on
	// it as an authored user message. An enqueue record, a drain record, or a
	// queued_command attachment. Waiting is the action; re-sending stacks a
	// duplicate and flushing submits the whole accumulated backlog.
	ReadingInFlight
	// ReadingDelivered: an authored user message carries it. A turn began.
	ReadingDelivered
)

func (r Reading) String() string {
	switch r {
	case ReadingLost:
		return "lost"
	case ReadingInFlight:
		return "in_flight"
	case ReadingDelivered:
		return "delivered"
	default:
		return "undecided"
	}
}

// Reading collapses a Fate into what a reader diagnosing a loss should
// conclude from it.
func (f Fate) Reading() Reading {
	switch f {
	case FateUserMessage:
		return ReadingDelivered
	case FateEnteredTurn, FateQueued:
		return ReadingInFlight
	case FateUnseen:
		return ReadingLost
	default:
		return ReadingUndecided
	}
}

// ReadingFor is Reading with mid-turn honesty (🎯T417).
//
// When the receiver is still composing a reply, an absence in the scanned
// region is NOT "lost" — it is the same shape as a payload that landed and
// has not yet been flushed, or a queued_command that has not yet been
// written. Treating that absence as ReadingLost is the false negative that
// produced a phantom defect class on 2026-08-10 when the overseer read
// claudia-po mid-turn. composing=true forces Undecided on an Unseen fate;
// positive fates are unchanged.
func ReadingFor(f Fate, composing bool) Reading {
	r := f.Reading()
	if composing && r == ReadingLost {
		return ReadingUndecided
	}
	return r
}

// Held reports whether the receiver demonstrably has the message — delivered
// or waiting. The predicate a sender needs before deciding it lost anything.
func (r Reading) Held() bool { return r >= ReadingInFlight }

// PermitsResend reports whether this reading leaves re-sending open at all,
// and it is true for exactly one of the four (🎯T447 clause 4).
//
// Stated as a positive permission rather than as a "not delivered" check on
// purpose: every false-loss episode in this file's history took a negative —
// not seen, not confirmed, not answered — and read it as licence to send
// again. Nine composer copies were stacked that way on 2026-08-10 and a 17KB
// duplicate on 2026-08-14. An undecided reading does not permit a re-send; it
// asks for another look.
//
// It is a NECESSARY condition and not a sufficient one, which is the honest
// limit of what this instrument can decide. A payload pasted into a composer
// and never submitted (🎯T416's defect) is absent from the receiver's records
// too, so it reads as lost here while re-sending it would stack a second copy
// behind the first. What this settles is the other direction, which is where
// the damage came from: a message the records show the receiver holding is
// never re-sendable, whatever else is true.
func (r Reading) PermitsResend() bool { return r == ReadingLost }

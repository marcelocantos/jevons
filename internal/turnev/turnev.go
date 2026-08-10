// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package turnev reads a receiving agent's own transcript to answer what
// became of a message that was handed to it (🎯T416).
//
// It exists as its own package because FOUR CALLERS ASK THE SAME QUESTION and
// the whole defect class here is two sources for one fact. The MCP send path,
// the spawn path, the send queue's drain and the handover dispatcher all need
// "did this payload become a turn"; when the answer lived inside mcpserver,
// the handover arm in internal/fleet could not reach it and used a different
// predicate instead — reply completion — which then condemned a delivery that
// merely took longer than one timeout. A second implementation of the load-
// bearing instrument is a second answer waiting to disagree with the first.
//
// WHAT THE RECEIVER RECORDS, and why this is better than what the daemon
// remembers. The sending daemon's notion of whether a turn was in flight is
// process-local, and jevonsd restarted three times on the day this was
// written; after a restart it knows nothing, and a discriminator built on that
// memory reports a defect about a perfectly healthy send. The receiver's
// transcript has no such gap. It records the message's fate on disk, in the
// receiving project, and it survives the sender's death:
//
//	authored user message carrying the payload  ⇒ a turn began on it
//	queue-operation enqueue carrying it         ⇒ accepted behind a live turn
//	queue-operation remove/dequeue/popAll, or a
//	  queued_command attachment carrying it     ⇒ it left the queue into a turn
//	none of the above                           ⇒ pasted and never submitted
//
// The queue records are what rescue the QUEUED answer, and getting there cost
// a false accusation: reading user messages only — correct against an agent
// quoting its own pane capture — reports ABSENT for a message that has already
// entered the receiver's turn, because a queued message lands as an attachment
// and never as a user message. The overseer payload-matched a correction to
// this target's own worker at 09:31:24Z, got absent, and had already delivered
// it; the daemon repeated the error twice more, twenty-one seconds after the
// queue had drained it.
//
// The asymmetry that makes the queue records worth building on: enqueue is a
// POSITIVE test for the queued state. Every instrument that lied on 2026-08-10
// was a failure to observe — transcript growth, a raw-file grep, a receiver's
// own compliance — and a failure to observe cannot distinguish "not there"
// from "not there yet". A record that says enqueue says something happened.
package turnev

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"strings"
)

// Fate is what the receiver's transcript says became of one payload. Ordered
// by how far the message got, so a scan can keep the strongest sighting.
type Fate int

const (
	// FateUnseen: nothing in the scanned region carries this payload. An
	// absence, and absences are the weak half of this instrument — consistent
	// with born-stuck, with a mid-turn read (🎯T417) and with a slow disk.
	FateUnseen Fate = iota
	// FateQueued: an enqueue record carries it. The receiver accepted it
	// behind a live turn and has not yet drained it. Not begun, not lost.
	FateQueued
	// FateEnteredTurn: a dequeue/remove/popAll record or a queued_command
	// attachment carries it — it left the queue into the turn. Delivery is
	// settled even though no user message will ever carry it.
	//
	// Residual: `remove` also covers a message CANCELLED out of the queue by
	// hand. That is a rare manual act, and the queued_command attachment is
	// unambiguous where it appears; reading a cancellation as delivered is the
	// known over-read, recorded here rather than papered over.
	FateEnteredTurn
	// FateUserMessage: an authored user message carries it. A turn began on
	// this payload. Nothing outranks it.
	FateUserMessage
)

func (f Fate) String() string {
	switch f {
	case FateQueued:
		return "queued"
	case FateEnteredTurn:
		return "entered_turn"
	case FateUserMessage:
		return "user_message"
	default:
		return "unseen"
	}
}

// Delivered reports whether the payload got far enough that the receiver owns
// it — a turn began, or the queue handed it into one.
func (f Fate) Delivered() bool { return f >= FateEnteredTurn }

// needleLen bounds the needle in runes. The daemon prepends the fleet standing
// brief to an agent's first send, so a multi-KB payload is mostly boilerplate
// every other agent also received; the distinguishing part is the caller's own
// text at the END. Taking the tail identifies the message rather than the
// brief, and keeps the scan cheap.
const needleLen = 512

// scanMax bounds a single transcript record. Records carrying large tool
// results run big; a record longer than this is not a message we need to read,
// and refusing to buffer it keeps a runaway line from being an allocation
// hazard.
const scanMax = 8 * 1024 * 1024

// Needle reduces a payload to what will be looked for in a transcript. Empty
// when there is nothing distinctive enough to recognise — which disables
// matching rather than asserting a match on noise.
func Needle(payload string) string {
	norm := Normalize(payload)
	if len([]rune(norm)) < 16 {
		// Too short to identify: "ok", "continue", a bare ack. Matching on
		// these would confirm from an unrelated message that happens to
		// contain the word.
		return ""
	}
	r := []rune(norm)
	if len(r) > needleLen {
		r = r[len(r)-needleLen:]
	}
	return string(r)
}

// Normalize collapses whitespace so a payload still matches after the CLI has
// re-wrapped or re-indented it. Both sides of the comparison run through this;
// nothing else is altered, because anything more aggressive starts matching
// messages that merely resemble the payload.
func Normalize(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// Size reports the size of a durable transcript, and whether one exists. An
// empty path (live-stream backend) is reported as absent.
func Size(path string) (int64, bool) {
	if strings.TrimSpace(path) == "" {
		return 0, false
	}
	fi, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	return fi.Size(), true
}

// Missing reports the born-stuck positive: this agent keeps a durable
// transcript, and no such file exists.
//
// Read the two guards together. An empty path is a live-stream backend, which
// has no transcript to be missing — reporting absence there would turn every
// healthy Grok send into a born-stuck finding. A non-empty path with no file
// is the real thing: the session was named, the pane is up, and nothing has
// ever been submitted into it.
func Missing(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	_, exists := Size(path)
	return !exists
}

// Scan reads the region appended since `from` and reports how far the payload
// got. It reads from a pre-send baseline so an earlier occurrence of the same
// text — a resend of a message that DID land before — cannot confirm this one.
//
// hadTranscript false means the file did not exist when the baseline was
// taken, so the whole file is new and is read from byte zero.
func Scan(path string, from int64, hadTranscript bool, needle string) Fate {
	if strings.TrimSpace(path) == "" || needle == "" {
		return FateUnseen
	}
	if !hadTranscript {
		from = 0
	}
	f, err := os.Open(path)
	if err != nil {
		return FateUnseen
	}
	defer f.Close()
	if from > 0 {
		if _, err := f.Seek(from, io.SeekStart); err != nil {
			return FateUnseen
		}
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), scanMax)
	best := FateUnseen
	first := true
	for sc.Scan() {
		line := sc.Bytes()
		if first && from > 0 {
			// The baseline may have landed mid-record if a write was in
			// flight. That partial line is an earlier record, never ours.
			first = false
			if !json.Valid(line) {
				continue
			}
		}
		first = false
		if fate := classify(line, needle); fate > best {
			best = fate
			if best == FateUserMessage {
				return best
			}
		}
	}
	return best
}

// record is every shape of transcript line this reader cares about, decoded
// once. The three live side by side in one file: conversation records carry
// `message`, queue bookkeeping carries `operation`+`content`, and a drained
// queue entry reappears as an `attachment`.
type record struct {
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	Operation   string `json:"operation"`
	Content     string `json:"content"`
	Attachment  *struct {
		Type   string `json:"type"`
		Prompt string `json:"prompt"`
	} `json:"attachment"`
	Message *struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// classify decides what one transcript line says about the payload.
func classify(line []byte, needle string) Fate {
	var rec record
	if err := json.Unmarshal(line, &rec); err != nil {
		return FateUnseen
	}
	switch {
	case rec.Type == "queue-operation":
		if !strings.Contains(Normalize(rec.Content), needle) {
			return FateUnseen
		}
		switch rec.Operation {
		case "enqueue":
			return FateQueued
		case "dequeue", "remove", "popAll":
			return FateEnteredTurn
		}
		return FateQueued
	case rec.Attachment != nil && rec.Attachment.Type == "queued_command":
		if strings.Contains(Normalize(rec.Attachment.Prompt), needle) {
			return FateEnteredTurn
		}
		return FateUnseen
	case rec.Message != nil:
		if text, ok := authoredUserText(rec); ok && strings.Contains(Normalize(text), needle) {
			return FateUserMessage
		}
	}
	return FateUnseen
}

// authoredUserText extracts what was actually SAID to the agent in one
// transcript record, and reports whether the record is such a message at all.
//
// This is the distinction clause 1 turns on. A user-role record can carry two
// very different things: text the sender authored, and tool_result blocks
// holding whatever the agent's own tools returned — pane captures, file reads,
// command output. An agent investigating a stuck send captures its own
// composer, and the unsubmitted payload lands in its transcript inside a
// tool_result. A raw file grep, or any check that reads a record's bytes
// without asking which part of it was authored, calls that delivery. It is not
// delivery; it is the agent looking at the message it never received.
func authoredUserText(rec record) (string, bool) {
	if rec.Message == nil || rec.Message.Role != "user" {
		return "", false
	}
	if rec.IsSidechain {
		// A subagent's own conversation, not the agent we addressed.
		return "", false
	}
	var asString string
	if err := json.Unmarshal(rec.Message.Content, &asString); err == nil {
		return asString, true
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(rec.Message.Content, &blocks); err != nil {
		return "", false
	}
	var b strings.Builder
	for _, blk := range blocks {
		if blk.Type == "text" {
			b.WriteString(blk.Text)
			b.WriteString("\n")
		}
	}
	if b.Len() == 0 {
		return "", false
	}
	return b.String(), true
}

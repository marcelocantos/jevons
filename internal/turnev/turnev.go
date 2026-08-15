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
//
// 🎯T422 — AND EVERY OTHER READER OF A SESSION FILE DECODES IT HERE TOO.
//
// The send path was repaired first and the read paths were left querying the
// old shape, so the repair landed green in one place while the other readers
// kept producing false answers about the same files. jevons_transcript_read
// errored "105 lines but no user turns (unrecognized format?)" on a live 286KB
// session for the same reason confirmation false-negated on it: both were
// looking for authored user messages, and a message accepted behind a live
// turn structurally cannot be one. Of 33 queued payloads measured on
// 2026-08-10, 32 carried a queued_command attachment and ZERO carried a user
// record.
//
// So Decode is the only code in this repository that knows what a session
// JSONL line looks like, and Fate/Scan the only answer to "did payload P reach
// session S, and in what state". internal/transcript renders from Decode, the
// phase/idle derivation reads those same entries, and the send confirmation
// scans them — one decoder, three questions. A second implementation of a
// load-bearing instrument is a second answer waiting to disagree with the
// first; that sentence was already in this file about four callers, and the
// read paths were the callers it did not reach.
package turnev

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"
)

// Fate is what the receiver's transcript says became of one payload. Ordered
// by how far the message got, so a scan can keep the strongest sighting.
type Fate int

const (
	// FateUnknown: nothing was READ. The transcript could not be opened, the
	// region could not be scanned to its end, or the payload is too short to
	// identify — so there is no observation here at all, in either direction.
	//
	// It sorts below FateUnseen so it can never win the scan's keep-the-
	// strongest comparison, and it is a separate answer from FateUnseen
	// because the difference is the whole of 🎯T422 clause 5: "I read the
	// records and this payload is in none of them" and "I stopped looking"
	// are not the same finding, and a decoder that returns the second as the
	// first manufactures an absence out of its own failure to look.
	FateUnknown Fate = iota - 1
	// FateUnseen: nothing in the scanned region carries this payload. An
	// absence, and absences are the weak half of this instrument — consistent
	// with born-stuck, with a mid-turn read (🎯T417) and with a slow disk.
	FateUnseen
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
	case FateUnseen:
		return "unseen"
	default:
		return "unknown"
	}
}

// Decided reports whether the scan observed the records at all. False is the
// answer to give a caller instead of "absent" when nothing was read.
func (f Fate) Decided() bool { return f != FateUnknown }

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
//
// Every path that fails to READ returns FateUnknown rather than FateUnseen
// (🎯T422 clause 5). No file, no needle, a seek that failed, a region that
// ended mid-record: none of those are evidence the payload is absent, and the
// caller has to be able to tell an observation from a non-observation. Only a
// region that was scanned to its end without a match answers "unseen".
func Scan(path string, from int64, hadTranscript bool, needle string) Fate {
	if strings.TrimSpace(path) == "" || needle == "" {
		return FateUnknown
	}
	if !hadTranscript {
		from = 0
	}
	f, err := os.Open(path)
	if err != nil {
		return FateUnknown
	}
	defer f.Close()
	if from > 0 {
		if _, err := f.Seek(from, io.SeekStart); err != nil {
			return FateUnknown
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
	if err := sc.Err(); err != nil && best == FateUnseen {
		// A record too long for the buffer, or a read that failed partway:
		// the region was not scanned to its end, so this is not an absence.
		return FateUnknown
	}
	return best
}

// Kind is what one decoded record IS — the classification every reader of a
// session file needs before it can ask its own question of the line.
type Kind int

const (
	// KindOther: bookkeeping this decoder has no opinion about — snapshots,
	// mode changes, the deferred-tool and skill attachments, summaries.
	KindOther Kind = iota
	// KindUserMessage: a record carrying text somebody AUTHORED to the agent.
	KindUserMessage
	// KindToolResult: a user-role record whose content is tool output only —
	// pane captures, file reads, command output. Never delivery, never a turn.
	KindToolResult
	// KindAssistant: the agent's own reply.
	KindAssistant
	// KindQueueOp: the CLI's queue bookkeeping — enqueue when it accepts a
	// message behind a live turn, remove/dequeue/popAll when it drains one.
	KindQueueOp
	// KindQueuedCommand: the attachment the CLI writes when a queued message
	// enters the turn. This is what a queued payload becomes; it never becomes
	// a KindUserMessage, which is why a reader that knows only about user
	// messages reports an empty conversation for a busy session.
	KindQueuedCommand
	// KindError: a provider or harness error notice recorded in the
	// conversation — `{"type":"error","content":…}`. It carries no role, so a
	// decoder that derives everything from role reads it as bookkeeping and
	// drops its text; the RSI coach's own copy of this decoder knew about it
	// precisely because an error notice is the single most load-bearing line in
	// a session for anyone asking what went wrong. It is not delivery and never
	// a turn boundary.
	KindError
)

func (k Kind) String() string {
	switch k {
	case KindUserMessage:
		return "user_message"
	case KindToolResult:
		return "tool_result"
	case KindAssistant:
		return "assistant"
	case KindQueueOp:
		return "queue_op"
	case KindQueuedCommand:
		return "queued_command"
	case KindError:
		return "error"
	default:
		return "other"
	}
}

// Record is one transcript line decoded once, by the only code in this
// repository that knows the file's shapes (🎯T422 clause 1). Four live side by
// side in one file: conversation records carry `message` (Claude) or a
// top-level `content` (Grok), queue bookkeeping carries `operation`+`content`,
// and a drained queue entry reappears as an `attachment`.
type Record struct {
	// Kind is what this record is. Read this rather than Type: the raw type
	// says "user" for both an authored message and a tool result.
	Kind Kind
	// Type is the raw `type` field, kept for callers that report what they
	// could not interpret rather than dropping it silently.
	Type string
	// Role is message.role when present, else derived from Type.
	Role string
	// Text is what was authored — a user's prompt, or the agent's reply text.
	// Empty for KindToolResult by construction: tool output is not authored.
	Text string
	// Queued is the payload a queue record carries (queue-operation content,
	// or a queued_command attachment's prompt).
	Queued string
	// Operation is the queue operation: enqueue | dequeue | remove | popAll.
	Operation string
	// AttachmentType names the attachment when there is one.
	AttachmentType string
	// StopReason is the assistant message's stop_reason when present.
	StopReason string
	// Model is the model id the provider stamped on this record —
	// message.model on a Claude assistant turn, a top-level model elsewhere.
	// It is here rather than in a second reader because a session file with two
	// decoders is the defect this package exists to end (🎯T422 clause 1), and
	// the badge resolver was one of the two.
	Model string
	// Timestamp is the line's timestamp when it parses as RFC3339.
	Timestamp time.Time
	// HasToolUse: an assistant content block of type tool_use.
	HasToolUse bool
	// IsSidechain: a subagent's own conversation, not the agent addressed.
	IsSidechain bool
	// IsMeta: harness-injected rather than authored by the sender.
	IsMeta bool
}

// Delivers reports whether this record can carry a delivered payload at all.
// A tool result cannot, however exactly its bytes match: an agent
// investigating a stuck send captures its own composer, and the unsubmitted
// payload lands in its transcript inside a tool_result (🎯T422 clause 3).
func (r Record) Delivers() bool {
	switch r.Kind {
	case KindUserMessage, KindQueueOp, KindQueuedCommand:
		return true
	}
	return false
}

// Payload is the text a delivery match runs against for this record — the
// authored prompt, or the queued payload. Empty for everything else.
func (r Record) Payload() string {
	switch r.Kind {
	case KindUserMessage:
		return r.Text
	case KindQueueOp, KindQueuedCommand:
		return r.Queued
	}
	return ""
}

// rawRecord is the wire shape, spanning both providers. Nothing outside Decode
// looks at it.
type rawRecord struct {
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	IsMeta      bool   `json:"isMeta"`
	Timestamp   string `json:"timestamp"`
	Operation   string `json:"operation"`
	Model       string `json:"model"`
	// Content is a queue record's payload (a string) or a Grok top-level
	// conversation payload (a string or an array of blocks), so it is decoded
	// twice from raw rather than typed once.
	Content    json.RawMessage `json:"content"`
	Attachment *struct {
		Type   string `json:"type"`
		Prompt string `json:"prompt"`
	} `json:"attachment"`
	Message *struct {
		Role       string          `json:"role"`
		StopReason string          `json:"stop_reason"`
		Model      string          `json:"model"`
		Content    json.RawMessage `json:"content"`
	} `json:"message"`
}

// Decode interprets one transcript line. ok is false only for a line that is
// not JSON at all — an unparseable line is a fact the caller may need to
// report, and reporting it is 🎯T422 clause 7's "names precisely what it
// cannot parse" rather than silently shrinking the conversation.
func Decode(line []byte) (Record, bool) {
	var raw rawRecord
	if err := json.Unmarshal(line, &raw); err != nil {
		return Record{}, false
	}
	rec := Record{
		Type:        raw.Type,
		Operation:   raw.Operation,
		IsSidechain: raw.IsSidechain,
		IsMeta:      raw.IsMeta,
		Model:       raw.Model,
	}
	if raw.Timestamp != "" {
		if ts, err := time.Parse(time.RFC3339, raw.Timestamp); err == nil {
			rec.Timestamp = ts
		}
	}
	if raw.Attachment != nil {
		rec.AttachmentType = raw.Attachment.Type
	}

	switch {
	case raw.Type == "queue-operation":
		rec.Kind = KindQueueOp
		var s string
		if json.Unmarshal(raw.Content, &s) == nil {
			rec.Queued = s
		}
		return rec, true
	case raw.Attachment != nil && raw.Attachment.Type == "queued_command":
		rec.Kind = KindQueuedCommand
		rec.Queued = raw.Attachment.Prompt
		rec.Role = "user"
		rec.Text = raw.Attachment.Prompt
		return rec, true
	}

	// Conversation records: Claude nests them under message, Grok puts the
	// content at the top level.
	content := raw.Content
	if raw.Message != nil {
		rec.Role = raw.Message.Role
		rec.StopReason = raw.Message.StopReason
		if raw.Message.Model != "" {
			rec.Model = raw.Message.Model
		}
		content = raw.Message.Content
	} else if raw.Type == "user" || raw.Type == "assistant" {
		rec.Role = raw.Type
	}
	if rec.Role == "" && raw.Type != "error" {
		return rec, true
	}

	text, hasToolUse, hasToolResult := decodeContent(content)
	rec.Text = text
	rec.HasToolUse = hasToolUse
	switch {
	case raw.Type == "error":
		// An error notice, whatever role it does or does not claim. Checked
		// before the role arms because a provider that stamps one with
		// role=assistant is still reporting a failure, not replying.
		rec.Kind = KindError
	case rec.Role == "assistant":
		rec.Kind = KindAssistant
	case rec.Role != "user":
		rec.Kind = KindOther
	case strings.TrimSpace(text) != "":
		rec.Kind = KindUserMessage
	case hasToolResult:
		rec.Kind = KindToolResult
	default:
		rec.Kind = KindOther
	}
	return rec, true
}

// decodeContent extracts the AUTHORED text of a content payload and reports
// what other block kinds it held.
//
// The authored/tool split is the distinction clause 3 turns on. A user-role
// record can carry two very different things: text the sender wrote, and
// tool_result blocks holding whatever the agent's own tools returned. A raw
// file grep, or any check that reads a record's bytes without asking which
// part of it was authored, calls a quoted pane capture delivery. It is not
// delivery; it is the agent looking at the message it never received.
func decodeContent(content json.RawMessage) (text string, hasToolUse, hasToolResult bool) {
	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" || trimmed == "null" {
		return "", false, false
	}
	if trimmed[0] == '"' {
		var s string
		if json.Unmarshal(content, &s) != nil {
			return "", false, false
		}
		return s, false, false
	}
	if trimmed[0] != '[' {
		return "", false, false
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		return "", false, false
	}
	var parts []string
	for _, blk := range blocks {
		switch blk.Type {
		case "text":
			if blk.Text != "" {
				parts = append(parts, blk.Text)
			}
		case "tool_use":
			hasToolUse = true
		case "tool_result":
			hasToolResult = true
		}
	}
	return strings.Join(parts, "\n"), hasToolUse, hasToolResult
}

// classify decides what one transcript line says about the payload.
func classify(line []byte, needle string) Fate {
	rec, ok := Decode(line)
	if !ok || !rec.Delivers() {
		return FateUnseen
	}
	if rec.IsSidechain {
		// A subagent's own conversation, not the agent we addressed.
		return FateUnseen
	}
	if !strings.Contains(Normalize(rec.Payload()), needle) {
		return FateUnseen
	}
	switch rec.Kind {
	case KindUserMessage:
		return FateUserMessage
	case KindQueuedCommand:
		return FateEnteredTurn
	default:
		switch rec.Operation {
		case "dequeue", "remove", "popAll":
			return FateEnteredTurn
		default:
			return FateQueued
		}
	}
}

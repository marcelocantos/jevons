// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package noopwedge decides whether a work agent has answered its work order
// with a bare acknowledgement and stopped, as distinct from working (🎯T402).
//
// WHY THE OBVIOUS DETECTOR IS WRONG. The symptom that raised 🎯T402 is a
// worker whose only visible output is the 22-byte string "No response
// requested." — prompt_delivered=true, status=running, phase=idle, a healthy
// pane, and nothing done. The tempting check is to look for that string. It
// is measurably a disaster. Across the 179 sessions in this owner's Claude
// project directory that contain the string at all, the number in which it is
// the agent's ONLY output is zero. Every single one also contains real turns:
// cl-t2.1-broker-wire and cl-t23-grok-deny-rules each emitted it at
// 20260810T030052Z and then kept working on their missions. A detector keyed
// on the string alone would have reaped 179 healthy workers to catch none.
//
// The ack is a turn-boundary artefact. It is what the model says when a
// message arrives that reads as informational — a restart notice, a status
// ping, a delivery whose payload is somebody else's. Healthy agents emit it
// constantly BETWEEN real reports. Report shape is therefore a poor proxy for
// worker health, and that, not the ack, is the finding worth keeping.
//
// THE DISCRIMINATOR, in the order it must be applied:
//
//  1. RATIO, at turn granularity, not string granularity. A turn is one
//     authored user message and every assistant message that follows it. A
//     turn is a no-op when it called no tools and its whole text is a
//     content-free acknowledgement. The agent is shape-suspect only when
//     EVERY turn since the work order is a no-op — one real turn anywhere in
//     the history clears it. Turn granularity matters on its own: "Now the
//     hermetic test:" is a short, content-free-looking assistant message that
//     appears 4 times in the corpus, always as the preamble to a turn that
//     then runs the test. String-level checks flag it; turn-level ones do not.
//
//  2. COMMITS. Shape-suspect and the mission target has commits behind it →
//     working. The agent's output shape was uninformative; the repository is
//     the oracle.
//
//  3. TREE. Shape-suspect, no commits, but paths touched in the working tree
//     → working. Uncommitted work is still work.
//
// Only an agent that fails all three is a wedge. Stages 2 and 3 cost a
// subprocess each, so ShapeNoOp is exported separately: callers run it first
// and skip the corroboration entirely for the overwhelming majority of agents
// that clear it. Verdict is deliberately narrow — this package answers "did
// this agent answer a work order with a no-op and stop", and nothing else. An
// agent with no turns at all is Unobserved, not a wedge: never having taken a
// turn is 🎯T387/🎯T416's phantom-seat class, with its own evidence rules.
//
// DIRECTION OF FAILURE. Over-broadness reaps a healthy worker mid-mission and
// destroys real work; over-narrowness leaves a wedged seat for the grace
// timer and the next sentinel cycle to catch. The asymmetry is the whole
// argument for requiring all three stages to agree, and it is why the
// acknowledgement vocabulary below is an exact-match set rather than a
// substring or prefix match.
package noopwedge

import (
	"fmt"
	"strings"
)

// Turn is one authored-user-message → assistant exchange, as the
// discriminator sees it. Text is the concatenated assistant text for the
// turn; ToolCalls is how many tool_use blocks it issued.
type Turn struct {
	Text      string
	ToolCalls int
}

// Verdict is what the discriminator concluded about an agent.
type Verdict int

const (
	// Unobserved — the agent has taken no turn since the work order. Not this
	// package's call: see 🎯T387 (spawn never began a turn) and 🎯T416 (the
	// message never left the composer).
	Unobserved Verdict = iota
	// Working — a real turn, or commits, or touched paths. The default, and
	// the answer for every agent in the measured corpus.
	Working
	// NoOpOnly — every turn since the work order was a content-free
	// acknowledgement, and neither the ledger nor the tree shows any work.
	NoOpOnly
)

// String renders a verdict for logs and wire reports.
func (v Verdict) String() string {
	switch v {
	case Working:
		return "working"
	case NoOpOnly:
		return "noop_only"
	default:
		return "unobserved"
	}
}

// Wedged reports whether the verdict is the fault this package detects.
func (v Verdict) Wedged() bool { return v == NoOpOnly }

// Evidence is everything the discriminator is given about one agent.
type Evidence struct {
	// Turns since the work order, oldest first.
	Turns []Turn
	// Commits attributable to the agent's mission (stage 2). Negative means
	// the caller could not look, which is treated as zero — absence of a
	// lookup is not evidence of work.
	Commits int
	// TreePaths the agent has touched but not committed (stage 3).
	TreePaths int
}

// ackMaxRunes bounds how long a turn's whole text may be and still count as
// content-free. The longest acknowledgement in the measured corpus is 22
// bytes; the bound is generous enough for punctuation and a trailing newline
// and far short of anything that could carry a report.
const ackMaxRunes = 64

// bareAcks is the exact-match vocabulary of content-free acknowledgements,
// normalized (lowercased, surrounding whitespace and trailing punctuation
// stripped). Exact match, never prefix or substring: "no response requested"
// is an ack, "no response requested — but the build is broken, see below" is
// a report that happens to start with one.
//
// TWO ENTRIES THE CORPUS THREW OUT, both of which were in the first draft and
// both of which the corpus check in corpus_test.go failed on immediately:
//
//   - "ok" / "okay" / "roger". These are the natural answer to a yes/no
//     question — "shall I proceed?" "ok" — and 241 sessions consist of exactly
//     that and nothing else. They are content-free as strings but not as
//     answers, and this package cannot see the question. Excluded: the cost of
//     a false positive is a healthy worker reaped.
//   - The empty string. A turn with no text and no tool calls is an
//     INTERRUPTED turn, not an answered one, and 376 sessions look like that.
//     🎯T402 is about answering a work order with a no-op; producing nothing
//     at all is the phantom-seat class that 🎯T387 and 🎯T416 own, with their
//     own evidence rules. Excluded so the two faults stay distinguishable.
//
// What is left is the vocabulary of explicitly DECLINING to act, which is the
// only shape that means "I read the work order and chose to do nothing".
var bareAcks = map[string]bool{
	"no response requested": true,
	"no response required":  true,
	"no response needed":    true,
	"nothing to report":     true,
	"acknowledged":          true,
	"ack":                   true,
	"understood":            true,
	"noted":                 true,
	"will do":               true,
}

// IsBareAck reports whether a turn's whole assistant text is a content-free
// acknowledgement — an explicit decline to act. Empty text is deliberately
// NOT an ack; see the note on bareAcks.
func IsBareAck(text string) bool {
	t := strings.TrimSpace(text)
	if t == "" || len([]rune(t)) > ackMaxRunes {
		return false
	}
	// A [silent] marker with nothing after it is an ack; with a payload after
	// it, it is an ops report that the owner merely does not see (🎯T238).
	lower := strings.ToLower(t)
	if stripped, ok := strings.CutPrefix(lower, "[silent]"); ok {
		lower = strings.TrimSpace(stripped)
		if lower == "" {
			return true
		}
	}
	return bareAcks[strings.TrimSpace(strings.TrimRight(lower, " \t\r\n.!。"))]
}

// silent reports a turn that produced nothing at all — no text, no tool
// calls. That is an interrupted or never-started turn, not an answer, and it
// belongs to 🎯T387 / 🎯T416 rather than here.
func silent(t Turn) bool {
	return t.ToolCalls == 0 && strings.TrimSpace(t.Text) == ""
}

// realTurn reports a turn that did something: it called a tool, or it said
// something that is not a content-free acknowledgement.
//
// Realness is MONOTONE as a turn accumulates — text only grows, tool calls
// only increase, and the acknowledgement vocabulary is exact-match under a
// length bound, so nothing a turn goes on to say can make it content-free
// again. ScanShape depends on that to stop reading early.
func realTurn(t Turn) bool {
	return t.ToolCalls > 0 || (strings.TrimSpace(t.Text) != "" && !IsBareAck(t.Text))
}

// Shape is stage 1's finding: what the agent's own turns look like, before
// the repository is consulted.
type Shape int

const (
	// ShapeNone — no turn answered anything. An empty history, or one of
	// nothing but silent turns.
	ShapeNone Shape = iota
	// ShapeReal — at least one turn did something.
	ShapeReal
	// ShapeAllNoOp — the agent answered, and every answer was a content-free
	// acknowledgement. The only shape stages 2 and 3 are worth paying for.
	ShapeAllNoOp
)

// shapeOf reads stage 1 off a turn history.
func shapeOf(turns []Turn) Shape {
	shape := ShapeNone
	for _, t := range turns {
		if silent(t) {
			continue
		}
		if realTurn(t) {
			return ShapeReal
		}
		shape = ShapeAllNoOp
	}
	return shape
}

// ShapeNoOp is stage 1: the agent answered, and every answer was a
// content-free acknowledgement. It is the cheap gate — false here means the
// caller spends no subprocess on stages 2 and 3, which is the outcome for
// essentially every agent. False for an empty history and false for a history
// of nothing but silent turns, so that a seat which never really answered is
// never mistaken for one that answered with a refusal.
func ShapeNoOp(turns []Turn) bool { return shapeOf(turns) == ShapeAllNoOp }

// Classify applies all three stages. Callers that want to skip the cost of
// gathering Commits and TreePaths should call ShapeNoOp first and only fill
// them in when it returns true; Classify re-runs stage 1 itself so a caller
// that does not bother still gets the same answer.
func Classify(ev Evidence) Verdict {
	return ClassifyShape(shapeOf(ev.Turns), ev.Commits, ev.TreePaths)
}

// ClassifyShape is Classify for a caller that scanned a transcript rather than
// holding its turns — the fleet view's path (see ScanShape). Same three
// stages in the same order, with stage 1 already decided.
//
// The two not-suspect shapes must not collapse together: ShapeNone is a seat
// that produced nothing observable, which is a phantom seat rather than a
// healthy worker, and the repository is the only thing that can tell the two
// apart when the transcript cannot.
func ClassifyShape(shape Shape, commits, treePaths int) Verdict {
	if shape == ShapeReal {
		return Working
	}
	if commits > 0 || treePaths > 0 {
		return Working
	}
	if shape == ShapeAllNoOp {
		return NoOpOnly
	}
	return Unobserved
}

// Detail renders the discriminator's reasoning for a wire report, so the
// reader can see which stage decided rather than only the verdict.
func Detail(ev Evidence) string {
	noop := 0
	for _, t := range ev.Turns {
		if t.ToolCalls == 0 && IsBareAck(t.Text) {
			noop++
		}
	}
	return fmt.Sprintf("%s: %d/%d turns no-op, %d commits, %d tree paths",
		Classify(ev), noop, len(ev.Turns), ev.Commits, ev.TreePaths)
}

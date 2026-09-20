// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package turnev

import (
	"bufio"
	"io"
	"strings"
)

// Phase is a symmetric positive reading of a session (🎯T423).
//
// Idle is not "we failed to see a user message." Working is not "we failed
// to see a stop." Each state requires evidence; UNKNOWN is a first-class
// answer and is never treated as idle by a control.
type Phase int

const (
	// PhaseUnknown: nothing was read, or the tape does not contain a
	// terminal or in-flight signal. Not idle. Not working.
	PhaseUnknown Phase = iota
	// PhaseIdle: an observed terminal — a completed assistant turn with
	// no pending queue record after it.
	PhaseIdle
	// PhaseWorking: observed evidence a turn is in flight — an enqueue
	// without a matching drain, a queued_command, tool_use without
	// end_turn, or assistant text that has not stopped.
	PhaseWorking
)

func (p Phase) String() string {
	switch p {
	case PhaseIdle:
		return "idle"
	case PhaseWorking:
		return "working"
	default:
		return "unknown"
	}
}

// Positive reports whether p is a decided idle or working reading.
func (p Phase) Positive() bool { return p == PhaseIdle || p == PhaseWorking }

// ClassifyPhase reads decoded session records (the 🎯T422 decoder) and
// answers idle / working / unknown. It never infers one from the absence
// of the other.
func ClassifyPhase(recs []Record) Phase {
	var f phaseFold
	for _, r := range recs {
		f.step(r)
	}
	return f.phase()
}

// step folds one record into the running state. ClassifyPhase runs it over
// a slice; the incremental path resumes it across calls (🎯T705). One
// implementation, so the two cannot drift.
func (f *phaseFold) step(r Record) {
	switch r.Kind {
	case KindQueueOp:
		switch strings.ToLower(strings.TrimSpace(r.Operation)) {
		case "enqueue":
			f.pending++
			f.last = PhaseWorking
		case "dequeue", "remove", "popall", "pop_all":
			if f.pending > 0 {
				f.pending--
			}
			// A drain without a later terminal is still in-flight unless
			// we have already seen an end_turn.
			if f.last != PhaseIdle {
				f.last = PhaseWorking
			}
		}
	case KindQueuedCommand:
		f.last = PhaseWorking
	case KindUserMessage:
		if strings.TrimSpace(r.Text) != "" {
			f.last = PhaseWorking
		}
	case KindAssistant:
		stop := strings.ToLower(strings.TrimSpace(r.StopReason))
		terminal := stop == "end_turn" || stop == "stop_sequence" ||
			stop == "max_tokens" || stop == "stop"
		switch {
		case r.HasToolUse && !terminal:
			f.last = PhaseWorking
		case terminal:
			f.last = PhaseIdle
		case strings.TrimSpace(r.Text) != "":
			f.last = PhaseWorking
		}
	default:
		// System records after an assistant close the turn
		// (session 76cef0a9 lines 112–113).
		if f.last == PhaseWorking && strings.EqualFold(r.Type, "system") {
			f.last = PhaseIdle
		}
	}
}

// phase is the fold's reading: a pending queue outranks the last signal.
func (f phaseFold) phase() Phase {
	if f.pending > 0 {
		return PhaseWorking
	}
	return f.last
}

// DecodeAll decodes every JSONL line from r using Decode. Unparseable
// lines are skipped rather than aborting the scan — the caller still
// gets a reading from what could be read.
func DecodeAll(r io.Reader) []Record {
	if r == nil {
		return nil
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), scanMax)
	var out []Record
	for sc.Scan() {
		rec, ok := Decode(sc.Bytes())
		if !ok {
			continue
		}
		out = append(out, rec)
	}
	return out
}

// ClassifyPhaseFile opens path and classifies it. A missing or unreadable
// file is PhaseUnknown — never idle.
func ClassifyPhaseFile(path string) Phase {
	if strings.TrimSpace(path) == "" {
		return PhaseUnknown
	}
	return classifyFile(path)
}

// CapsSystemicActions reports whether a single pass that would act on n
// agents is a systemic misread (🎯T423 clause 5). One notice, not a
// fleet-wide cull.
func CapsSystemicActions(n, capN int) bool {
	if capN <= 0 {
		capN = DefaultSystemicCap
	}
	return n >= capN
}

// DefaultSystemicCap is how many same-pass idle/repair acts become a
// report rather than execution. Two genuine idle workers is ordinary;
// a fleet-wide idle reading in one tick is the 2026-08-10 sentinel
// misread shape.
const DefaultSystemicCap = 5

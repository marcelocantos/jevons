// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package thread

import (
	"fmt"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/transcript"
)

// State is the derived lifecycle state of a thread, computed on demand
// from its transcript tail plus liveness signals. It is never persisted.
type State string

const (
	// StateActive: another writer is currently driving the session (a
	// live claude process holds the transcript open) — the two-writer
	// case where the butler observes but must not direct.
	StateActive State = "active"
	// StateWorking: a turn is in flight — the last event is an
	// unfinished assistant message (streaming or a tool_use) or a fresh
	// user prompt still awaiting its reply.
	StateWorking State = "working"
	// StateBlocked: a user prompt has gone unanswered past the idle
	// threshold — the session appears stuck (or parked at a prompt).
	StateBlocked State = "blocked"
	// StateDone: the last turn concluded (terminal stop_reason) within
	// the recent window.
	StateDone State = "done"
	// StateIdle: quiescent — concluded and old, or no transcript activity.
	StateIdle State = "idle"
)

// DefaultIdleThreshold is how long a thread may go without new transcript
// activity before it is considered idle rather than freshly done, and
// how long an unanswered prompt waits before it is considered blocked.
const DefaultIdleThreshold = 5 * time.Minute

// terminalStopReasons are the assistant stop_reasons that mark a turn as
// concluded rather than mid-flight (tool_use is explicitly non-terminal).
var terminalStopReasons = map[string]bool{
	"end_turn":      true,
	"stop_sequence": true,
	"max_tokens":    true,
}

// Status is the on-demand answer to "where is this thread right now?".
type Status struct {
	State        State     `json:"state"`
	Summary      string    `json:"summary"`       // one-line recent-activity summary
	LastActivity time.Time `json:"last_activity"` // timestamp of the newest entry
	ProcessUp    bool      `json:"process_up"`    // butler owns a live process for it
}

// StatusInput carries everything DeriveStatus needs. It is a pure
// function of these inputs so the state machine is fully testable
// without a filesystem or a live process.
type StatusInput struct {
	Entries          []transcript.Entry // chronological transcript tail
	Now              time.Time
	ExternallyActive bool          // a foreign claude process holds the transcript open
	ProcessUp        bool          // the butler owns a live process for this thread
	IdleThreshold    time.Duration // defaults to DefaultIdleThreshold when zero
}

// DeriveStatus computes a thread's live status from its transcript tail
// and liveness signals. The rules, in priority order:
//
//  1. externally active → active (someone else is driving it now)
//  2. no transcript activity → idle
//  3. last entry is a still-open assistant turn (no terminal stop_reason,
//     or a tool_use) → working
//  4. last entry is an unanswered user prompt → working if recent, else
//     blocked (stuck / parked)
//  5. last entry is a concluded assistant turn → done if recent, else idle
func DeriveStatus(in StatusInput) Status {
	threshold := in.IdleThreshold
	if threshold <= 0 {
		threshold = DefaultIdleThreshold
	}

	last, ok := lastMeaningful(in.Entries)
	if !ok {
		return Status{State: StateIdle, Summary: "no transcript activity", ProcessUp: in.ProcessUp}
	}

	lastActivity := last.Timestamp
	age := in.Now.Sub(lastActivity)
	recent := !lastActivity.IsZero() && age < threshold
	// A zero timestamp means we cannot judge recency; treat as recent so
	// we never mislabel a live-looking tail as idle purely for lack of a
	// clock reading.
	if lastActivity.IsZero() {
		recent = true
	}

	st := Status{LastActivity: lastActivity, ProcessUp: in.ProcessUp}

	switch {
	case in.ExternallyActive:
		st.State = StateActive
	case last.Type == "assistant" && (last.HasToolUse || !terminalStopReasons[last.StopReason]):
		st.State = StateWorking
	case last.IsUserTurn:
		if recent {
			st.State = StateWorking
		} else {
			st.State = StateBlocked
		}
	default: // concluded assistant turn (or other terminal event)
		if recent {
			st.State = StateDone
		} else {
			st.State = StateIdle
		}
	}

	st.Summary = summarise(in.Entries, st.State, in.Now, lastActivity)
	return st
}

// lastMeaningful returns the newest entry that carries a role or text,
// skipping bookkeeping lines (snapshots, empty system frames).
func lastMeaningful(entries []transcript.Entry) (transcript.Entry, bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Role != "" || e.Text != "" || e.HasToolUse {
			return e, true
		}
	}
	return transcript.Entry{}, false
}

// summarise renders a one-line recent-activity summary.
func summarise(entries []transcript.Entry, state State, now, lastActivity time.Time) string {
	var lastUser, lastAssistant string
	for _, e := range entries {
		switch {
		case e.IsUserTurn && e.Text != "":
			lastUser = e.Text
		case e.Type == "assistant" && e.Text != "":
			lastAssistant = e.Text
		}
	}

	var head string
	switch state {
	case StateBlocked, StateWorking:
		if lastAssistant == "" && lastUser != "" {
			head = "awaiting response to: " + oneLine(lastUser)
		} else if lastAssistant != "" {
			head = "last reply: " + oneLine(lastAssistant)
		} else {
			head = "in progress"
		}
	default:
		if lastAssistant != "" {
			head = "last reply: " + oneLine(lastAssistant)
		} else if lastUser != "" {
			head = "last prompt: " + oneLine(lastUser)
		} else {
			head = "no activity"
		}
	}

	if lastActivity.IsZero() {
		return head
	}
	return fmt.Sprintf("%s (%s ago)", head, roundDuration(now.Sub(lastActivity)))
}

const summaryTextLimit = 120

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > summaryTextLimit {
		return s[:summaryTextLimit-1] + "…"
	}
	return s
}

// roundDuration renders a coarse, human-friendly age.
func roundDuration(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

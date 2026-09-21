// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package thread

import (
	"fmt"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/seatstate"
	"github.com/marcelocantos/jevons/internal/transcript"
)

// State is where a thread's seat is, as the parties that know report it.
//
// 🎯T766.2: this used to be derived from the transcript tail — a five-rule
// machine that read an unanswered prompt older than five minutes as
// "blocked" and a quiet tail as "idle". That was census derivation 6, and
// the butler's idle reaper acted on it. The reaper is gone, and a state
// inferred from a quiet transcript is the guess the seat-state authority
// exists to end, so the state now comes from the authority (claudia and the
// provider event stream) and the transcript supplies content only.
type State string

const (
	// StateActive: another writer is driving the session right now (the
	// scanner sees a foreign process holding it open). An observation, not
	// an inference: the two-writer case where the butler must not direct.
	StateActive State = "active"
	// StateWorking: the provider reports a turn in flight.
	StateWorking State = "working"
	// StateIdle: the process is alive and no turn is in flight.
	StateIdle State = "idle"
	// StateStopped: the process is reported not alive.
	StateStopped State = "stopped"
	// StateUnknown: nobody has reported this seat, or the report is stale.
	// Never rounded to idle.
	StateUnknown State = "unknown"
)

// Status is the on-demand answer to "where is this thread right now?".
type Status struct {
	State        State     `json:"state"`
	Summary      string    `json:"summary"`       // one-line recent-activity summary
	LastActivity time.Time `json:"last_activity"` // timestamp of the newest entry
	ProcessUp    bool      `json:"process_up"`    // butler owns a live process for it
	// Integrity issues from transcript.CheckUserTurns (🎯T33). Empty when clean.
	Integrity []transcript.IntegrityIssue `json:"integrity,omitempty"`
}

// StatusInput carries everything DeriveStatus needs. It is a pure function
// of these inputs so it is fully testable without a filesystem or a process.
type StatusInput struct {
	Entries          []transcript.Entry // chronological transcript tail: content only
	Now              time.Time
	ExternallyActive bool // a foreign process holds the session open
	ProcessUp        bool // the butler owns a live process for this thread
	// Seat is what the seat-state authority says; SeatKnown is false when it
	// has never been told about this thread.
	Seat      seatstate.State
	SeatKnown bool
}

// StateOf maps an authority reading to a thread state. Unknown stays
// unknown: a seat nobody has reported is not idle.
func StateOf(seat seatstate.State, known bool) State {
	switch {
	case !known:
		return StateUnknown
	case seat.Alive == seatstate.No:
		return StateStopped
	case seat.InFlight == seatstate.Yes:
		return StateWorking
	case seat.InFlight == seatstate.No:
		return StateIdle
	default:
		return StateUnknown
	}
}

// DeriveStatus assembles a thread's status: its state from the authority,
// and its summary, last activity and integrity findings from the transcript.
func DeriveStatus(in StatusInput) Status {
	// 🎯T33: always attach decidable integrity findings for butler list/status.
	integrity := transcript.CheckUserTurns(in.Entries)
	state := StateOf(in.Seat, in.SeatKnown)
	if in.ExternallyActive {
		state = StateActive
	}
	st := Status{State: state, ProcessUp: in.ProcessUp, Integrity: integrity}
	last, ok := lastMeaningful(in.Entries)
	if !ok {
		st.Summary = "no transcript activity"
		return st
	}
	st.LastActivity = last.Timestamp
	st.Summary = summarise(in.Entries, state, in.Now, last.Timestamp)
	if len(integrity) > 0 {
		// Live thread-health signal for list/status (🎯T33).
		st.Summary = "⚠ integrity: " + string(integrity[0].Kind) + " — " + st.Summary
	}
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
	case StateWorking:
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

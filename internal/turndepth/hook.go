// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package turndepth

import (
	"encoding/json"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// The in-band half of 🎯T392.4.
//
// The daemon can SEE a turn's depth — claudia collapses every tool call to a
// progress event — but it cannot SPEAK into a turn that is running. The send
// path deliberately refuses to (🎯T418: a message handed to a provider CLI
// mid-turn lands in a store the daemon can neither see nor replay), and the
// only other channel it has is the interrupt, which is the thing this design
// is trying not to use.
//
// The harness's own PreToolUse hook is the channel that exists. It runs
// inside the agent's process, on every tool call, and what it writes on
// stderr with exit 2 goes to the model. That is the whole reason the ask is
// delivered from here rather than from the daemon: it is the one place in
// the system where the daemon's policy can reach a running turn's reasoning.
//
// It costs exactly one refused tool call, once per turn, and the ask says so
// — retry and it proceeds. That is what makes this a soft ceiling rather
// than a cap: the agent is informed and may overrule, and the daemon records
// which it did.

// DisableEnv set to "off" (or "0"/"false"/"no") disables the ceiling. A
// deliberate, loud choice, and what the negative-control test sets to show
// the deep turn really does run unimpeded without it.
const DisableEnv = "JEVONS_TURNDEPTH"

// CeilingEnv overrides the configured ceiling for one agent's environment.
const CeilingEnv = "JEVONS_TURNDEPTH_CEILING"

// GraceEnv overrides how many further calls follow the ask before the
// fallback is considered. Read by the daemon; the hook never interrupts.
const GraceEnv = "JEVONS_TURNDEPTH_GRACE"

// InterruptEnv arms the daemon-side interrupt fallback, which is off unless
// it is set to a true value. Deliberately its own switch rather than a
// consequence of the ceiling being on: see Policy.InterruptEnabled.
const InterruptEnv = "JEVONS_TURNDEPTH_INTERRUPT"

// PolicyFromEnv resolves the ceiling both halves of 🎯T392.4 are judged
// against.
//
// One resolver, two callers, on purpose. The hook and the daemon count
// different things in different processes, but they must not disagree about
// WHERE THE CEILING IS: an agent asked to checkpoint at 8 while the daemon
// believes the ceiling is 12 would be nagged for four calls the daemon
// considered ordinary, and the opposite pairing arms a fallback against a
// turn nobody ever asked to stop.
func PolicyFromEnv(getenv func(string) string) Policy {
	pol := Policy{
		Disabled:         isOff(getenv(DisableEnv)),
		InterruptEnabled: isOn(getenv(InterruptEnv)),
	}
	if n, err := strconv.Atoi(strings.TrimSpace(getenv(CeilingEnv))); err == nil {
		pol.Ceiling = n
	}
	if n, err := strconv.Atoi(strings.TrimSpace(getenv(GraceEnv))); err == nil {
		pol.Grace = n
	}
	return pol
}

// Hook event names this package acts on. PreToolUse counts; the other two
// are turn boundaries.
const (
	EventPreToolUse       = "PreToolUse"
	EventUserPromptSubmit = "UserPromptSubmit"
	EventStop             = "Stop"
)

// Payload is the subset of the harness hook JSON this hook needs. Unknown
// fields are ignored, so a payload shape change degrades to a missing field
// rather than a parse failure.
type Payload struct {
	SessionID     string `json:"session_id"`
	HookEventName string `json:"hook_event_name"`
	ToolName      string `json:"tool_name"`
}

// Env is the resolved runtime the hook acts against.
type Env struct {
	Store  *Store
	Policy Policy
	Now    func() time.Time
}

// NewEnv resolves the hook's runtime from the process environment.
func NewEnv() *Env {
	return &Env{
		Store:  &Store{Root: DefaultStoreRoot()},
		Policy: PolicyFromEnv(os.Getenv),
		Now:    time.Now,
	}
}

// DecodePayload reads one hook payload from r.
func DecodePayload(r io.Reader) (*Payload, error) {
	var p Payload
	if err := json.NewDecoder(r).Decode(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

// HookResult is what the hook binary should do about one payload.
type HookResult struct {
	// Ask is the text to put in front of the model, empty when there is
	// nothing to say. Non-empty means the tool call is refused this once.
	Ask string
	// Decision is the policy's own record, carried out so the binary can
	// report it and tests can assert on it without re-deriving it.
	Decision Decision
}

// Observe applies one hook payload and returns what to do about it.
//
// Turn boundaries reset the count; a tool call increments it. Everything
// else — an event this hook is matched on but does not care about — passes
// through untouched, because a hook that reset state on events it did not
// understand would silently disable the ceiling the day a new event appeared.
func (e *Env) Observe(p *Payload) (HookResult, error) {
	now := e.Now()
	switch p.HookEventName {
	case EventUserPromptSubmit, EventStop:
		// A turn boundary. Both are wired, and either alone would do: the
		// submit is the start of the next turn and the stop is the end of
		// this one. Two signals for one edge is cheap, and the cost of
		// missing the edge is an ask on the first call of every later turn.
		return HookResult{}, e.Store.Reset(p.SessionID)
	case EventPreToolUse:
		// Fall through.
	default:
		return HookResult{}, nil
	}

	turn, err := e.Store.Load(p.SessionID, now)
	if err != nil {
		return HookResult{}, err
	}
	turn.Calls++
	d := e.Policy.Evaluate(State{
		Agent:     p.SessionID,
		Calls:     turn.Calls,
		Requested: turn.Requested,
	})
	res := HookResult{Decision: d}
	if d.Action == ActionRequestCheckpoint {
		// Latch BEFORE returning the ask, so a hook that is killed between
		// deciding and being read still leaves the turn marked as asked. The
		// failure that matters here is asking twice, not asking once too
		// few: an agent told to checkpoint on every call after the ceiling
		// learns to ignore the message, and then the ceiling has no channel
		// at all.
		turn.Requested = true
		res.Ask = CheckpointAsk(d)
	}
	if err := e.Store.Save(p.SessionID, turn, now); err != nil {
		return res, err
	}
	return res, nil
}

func isOff(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "off", "0", "false", "no":
		return true
	}
	return false
}

// isOn is not !isOff. An unset variable is neither: the ceiling defaults on
// and the interrupt defaults off, so each switch asks about the value it is
// looking for rather than about the absence of the other.
func isOn(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "on", "1", "true", "yes":
		return true
	}
	return false
}

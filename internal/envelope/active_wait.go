// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package envelope

import (
	"fmt"
	"strings"
)

// WaitClass names the kind of alive external process a seat is actively
// waiting on (🎯T1024, typed by 🎯T766.4.1). The slot exists because the
// 2026-10-07 reports narrated the wait in monitoring vocabulary ("alive and
// still queued", "the monitor armed last turn is still watching") that no
// phrase list heard, and a phrase list is not how a control decides. The
// worker declares the wait; the daemon reads the slot.
type WaitClass string

const (
	// WaitGate: the seat's own gate run (bin/gate, a test suite), queued or
	// running, whose verdict it has not yet read.
	WaitGate WaitClass = "gate"
	// WaitLease: a shared lease, lock, or queue held by someone else — the
	// heavy gate lease the 2026-10-07 seats were queued fifth on.
	WaitLease WaitClass = "lease"
	// WaitProcess: a background process, job, or monitor the seat launched
	// and is watching.
	WaitProcess WaitClass = "process"
	// WaitCI: a remote CI run.
	WaitCI WaitClass = "ci"
)

// allWaitClasses is the canonical list. Instruction files point here
// rather than restating it.
var allWaitClasses = []WaitClass{WaitGate, WaitLease, WaitProcess, WaitCI}

// AllWaitClasses returns every known wait class in definition order.
func AllWaitClasses() []WaitClass { return append([]WaitClass(nil), allWaitClasses...) }

// ParseWaitClass maps a slot token onto a WaitClass. Unknown names fail:
// a wait the daemon cannot name is a malformed slot, not a guess.
func ParseWaitClass(raw string) (WaitClass, bool) {
	c := WaitClass(strings.ToLower(strings.TrimSpace(raw)))
	for _, known := range allWaitClasses {
		if c == known {
			return c, true
		}
	}
	return "", false
}

func (c WaitClass) String() string { return string(c) }

// ActiveWait is one declared active external wait: what class of process,
// and which one (a gate name or id, a lease, a pid, a CI run URL).
//
//	jevons: active-wait gate t41-slowpath (queued fifth on the shared heavy lease)
//
// Repeatable. A seat that carries this slot is not finished, whatever its
// payload says: a finish-report that carries it is contradictory and is
// not a done claim (ClassifyDone answers DoneUnknown), and the reap, stop,
// and idle paths park the seat rather than reap it (🎯T985: an external
// block parks).
type ActiveWait struct {
	Class WaitClass
	On    string
}

// String renders the slot value: class, then what is waited on.
func (w ActiveWait) String() string {
	return string(w.Class) + " " + w.On
}

func parseActiveWait(value string) (ActiveWait, error) {
	class, on, _ := strings.Cut(strings.TrimSpace(value), " ")
	c, ok := ParseWaitClass(class)
	if !ok {
		return ActiveWait{}, fmt.Errorf("unknown active-wait class %q (want %s)", class, waitClassList())
	}
	on = strings.TrimSpace(unquoteSlot(strings.TrimSpace(on)))
	if on == "" {
		return ActiveWait{}, fmt.Errorf("active-wait %s requires what it waits on", c)
	}
	return ActiveWait{Class: c, On: on}, nil
}

func waitClassList() string {
	names := make([]string, len(allWaitClasses))
	for i, c := range allWaitClasses {
		names[i] = string(c)
	}
	return strings.Join(names, "|")
}

// HasActiveWait is true when the envelope declares at least one active
// external wait.
func (m *Message) HasActiveWait() bool {
	return m != nil && len(m.ActiveWaits) > 0
}

// ActiveWaitIn reads a stored report: when its envelope declares an active
// external wait, it returns the first one and true, whatever the kind.
// A slot error elsewhere in the fence does not hide a wait that did parse
// — like BlockedOn, keeping the seat is the cheap side of the mistake.
func ActiveWaitIn(text string) (ActiveWait, bool) {
	m, _ := Parse(text)
	if !m.HasActiveWait() {
		return ActiveWait{}, false
	}
	return m.ActiveWaits[0], true
}

func activeWaitFingerprint(m *Message) string {
	if !m.HasActiveWait() {
		return ""
	}
	var b strings.Builder
	b.WriteString("active-wait=")
	for _, w := range m.ActiveWaits {
		b.WriteByte('|')
		b.WriteString(w.String())
	}
	return b.String()
}

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package panecensus decides which fleet tmux panes may be reaped (🎯T459).
//
// On 2026-08-15 the fleet tmux server held 85 panes against 48 registered
// agents: 21 unregistered orphans and 16 unbounded claudia-pool-* warm
// spares, together 613 processes and 9.7 GB RSS doing no product work, while
// the host sat at load 304 with swap exhausted. 🎯T36 already flags orphans
// for spend; nothing reaped them for host resources.
//
// Everything here is pure: panes + a registry set in, a reap plan out. The
// daemon lists tmux and kills what this package names. A pane is reaped only
// when it has no registry entry AND no in-flight turn — dropping the
// in-flight check is the mutation the oracle is written to catch.
package panecensus

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

const (
	// DefaultWarmPoolMax is the stated bound on idle claudia-pool-* panes.
	// The 2026-08-15 census held 16 with no number written down anywhere;
	// a bound that is only whatever happened to be running is not a bound.
	DefaultWarmPoolMax = 2

	// TypicalChildrenPerAgent is the MCP-server node fan-out observed
	// alongside one claude/grok process (~15 children).
	TypicalChildrenPerAgent = 15

	// TypicalRSSPerAgentBytes is the working-set observed per agent
	// (~260 MB including those children).
	TypicalRSSPerAgentBytes = 260 * 1024 * 1024
)

// Flight is what is known about a pane's turn.
type Flight int

const (
	// FlightIdle: no turn in flight. Unregistered idle panes are reapable.
	FlightIdle Flight = iota
	// FlightInFlight: a turn is running. Never reap, even if unregistered.
	FlightInFlight
	// FlightUnknown: cannot tell. Treated as in-flight so a working agent
	// is not killed on a missing signal.
	FlightUnknown
)

// Pane is one tmux pane in the fleet server.
type Pane struct {
	// Session is the tmux session name (typically claudia-anchor).
	Session string
	// Window is the tmux window name. Claude Session windows are
	// claudia-<first-8-of-session-id> (tmuxagent.SessionWindowName),
	// not the registry agent name. Warm-pool windows are claudia-pool-*.
	Window string
	// ID is the tmux pane id ("%12").
	ID string
	// PID is the pane's shell/process id, when known.
	PID int
	// Title is the pane title, used to infer flight when the caller has not
	// already set Flight.
	Title string
	// AgentName is the @claudia-agent-name option, when set.
	AgentName string
	// SessionID is the @claudia-session-id option, when set.
	SessionID string
	// Flight is the caller's reading. FlightIdle with an empty Title is
	// still idle; InferFlight is applied only when Flight is zero-value
	// AND Title is non-empty — callers that already know set Flight.
	Flight Flight
	// flightSet is true when the caller assigned Flight explicitly,
	// including FlightIdle. Distinguishes "unset, infer" from "idle".
	flightSet bool
}

// WithFlight returns a copy of p with Flight assigned explicitly, so a
// fixture can say idle without the title heuristic overriding it.
func (p Pane) WithFlight(f Flight) Pane {
	p.Flight = f
	p.flightSet = true
	return p
}

// Name is the identity used to look the pane up in the registry: the
// claudia agent name if present, otherwise the window name.
func (p Pane) Name() string {
	if n := strings.TrimSpace(p.AgentName); n != "" {
		return n
	}
	return strings.TrimSpace(p.Window)
}

// IsPool reports a claudia warm-pool pane.
func (p Pane) IsPool() bool {
	n := p.Name()
	return strings.HasPrefix(n, "claudia-pool-") || strings.HasPrefix(p.Window, "claudia-pool-")
}

// Action is what the census decides to do with one pane.
type Action string

const (
	// ActionKeep: registered, or otherwise must live.
	ActionKeep Action = "keep"
	// ActionKeepInFlight: unregistered but mid-turn — killing it is the
	// mutation the oracle exists to catch.
	ActionKeepInFlight Action = "keep_inflight"
	// ActionKeepPool: idle warm-pool pane inside the stated bound.
	ActionKeepPool Action = "keep_pool"
	// ActionReap: no registry entry and no in-flight turn.
	ActionReap Action = "reap"
)

// Decision is one pane's fate, with a reason the eventlog can name.
type Decision struct {
	Pane   Pane
	Action Action
	Reason string
}

// Cost is the host cost the fleet can see — not just "48 agents".
type Cost struct {
	Agents    int
	Processes int
	RSSBytes  int64
}

// Report is the census of one fleet tmux server.
type Report struct {
	Decisions  []Decision
	Registered int
	Orphans    int
	PoolKept   int
	PoolReaped int
	Cost       Cost
}

// Reap returns the panes the daemon must kill, in stable order.
func (r Report) Reap() []Decision {
	var out []Decision
	for _, d := range r.Decisions {
		if d.Action == ActionReap {
			out = append(out, d)
		}
	}
	return out
}

// EstimateCost is the per-agent host cost the fleet can see (🎯T459 §2).
// agent_list reading "48" is not enough when the host is deciding whether
// to admit more: each agent is a claude process plus ~15 MCP children and
// ~260 MB.
func EstimateCost(agents int) Cost {
	if agents < 0 {
		agents = 0
	}
	return Cost{
		Agents:    agents,
		Processes: agents * (1 + TypicalChildrenPerAgent),
		RSSBytes:  int64(agents) * TypicalRSSPerAgentBytes,
	}
}

// FormatCost is the agent_list / capacity-status line for EstimateCost.
func FormatCost(c Cost) string {
	return fmt.Sprintf("host cost (est.): %d agents × ~%d processes ≈ %d processes, ~%.1f GB RSS (🎯T459)",
		c.Agents, 1+TypicalChildrenPerAgent, c.Processes, float64(c.RSSBytes)/(1<<30))
}

// FormatCensus is the agent_list footer naming panes vs registry vs pool.
func FormatCensus(r Report, poolMax int) string {
	if poolMax <= 0 {
		poolMax = DefaultWarmPoolMax
	}
	return fmt.Sprintf("census: %d panes, %d registered, %d reapable orphans, %d warm-pool (bound %d, reaped %d) (🎯T459)",
		len(r.Decisions), r.Registered, r.Orphans, r.PoolKept+r.PoolReaped, poolMax, r.PoolReaped)
}

// InferFlight reads a pane title for the busy patterns the Claude/Grok
// TUIs paint while a turn is running. Anything else is idle — the 2026-08-15
// orphans sat at an empty prompt and must classify as reapable.
func InferFlight(title string) Flight {
	t := strings.ToLower(title)
	switch {
	case strings.Contains(t, "esc to interrupt"),
		strings.Contains(t, "esc to stop"),
		strings.Contains(t, "running…"),
		strings.Contains(t, "running..."):
		return FlightInFlight
	default:
		return FlightIdle
	}
}

func (p Pane) flight() Flight {
	if p.flightSet {
		return p.Flight
	}
	if strings.TrimSpace(p.Title) != "" {
		return InferFlight(p.Title)
	}
	return p.Flight
}

// sessionWindowPrefix / sessionWindowIDLen match claudia
// tmuxagent.SessionWindowName. That is the live identity of a Claude
// Session pane: Start names the window claudia-<first-8>, and
// WindowsForSession treats that name as belonging to the session even
// when @claudia-session-id is not visible.
const (
	sessionWindowPrefix = "claudia-"
	sessionWindowIDLen  = 8
)

// SessionWindowName is the tmux window name claudia Start uses for a
// session. Duplicated from claudia/internal/tmuxagent so this package
// stays a pure classifier (🎯T514).
func SessionWindowName(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	if len(sessionID) > sessionWindowIDLen {
		sessionID = sessionID[:sessionWindowIDLen]
	}
	return sessionWindowPrefix + sessionID
}

func looksLikeSessionID(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) <= sessionWindowIDLen {
		return false
	}
	for i := 0; i < sessionWindowIDLen; i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

func registered(p Pane, names map[string]bool) bool {
	if names == nil {
		return false
	}
	if names[p.Name()] {
		return true
	}
	if id := strings.TrimSpace(p.SessionID); id != "" && names[id] {
		return true
	}
	// list-panes #{@claudia-session-id} is a pane user option; claudia
	// writes the id with set-option -w. The window name is then the
	// only identity on the pane, and it is SessionWindowName(sid).
	if p.IsPool() {
		return false
	}
	win := strings.TrimSpace(p.Window)
	if win == "" {
		win = p.Name()
	}
	if !strings.HasPrefix(win, sessionWindowPrefix) {
		return false
	}
	for key := range names {
		if looksLikeSessionID(key) && SessionWindowName(key) == win {
			return true
		}
	}
	return false
}

// Plan classifies every pane. names is the live registry: agent names and
// session ids. A Claude Session window named claudia-<first-8-of-sid>
// is registered when that sid is in names (🎯T514). A name that is not
// in names and does not match a session window is unregistered.
//
// Warm-pool panes are counted separately and only the excess over
// DefaultWarmPoolMax (or opts.WarmPoolMax) are reaped, idle ones first.
func Plan(panes []Pane, names map[string]bool, warmPoolMax int) Report {
	if warmPoolMax <= 0 {
		warmPoolMax = DefaultWarmPoolMax
	}
	type item struct {
		i int
		p Pane
	}
	var (
		pool []item
		rest []item
	)
	for i, p := range panes {
		if p.IsPool() && !registered(p, names) {
			pool = append(pool, item{i, p})
		} else {
			rest = append(rest, item{i, p})
		}
	}
	// Deterministic keep-set: lowest window/name first, so a bound of 2
	// always keeps the same two panes given the same input.
	sort.SliceStable(pool, func(a, b int) bool {
		return pool[a].p.Name() < pool[b].p.Name()
	})

	decisions := make([]Decision, len(panes))
	var registeredN, orphans, poolKept, poolReaped int

	idlePool := 0
	for _, it := range pool {
		p := it.p
		f := p.flight()
		d := Decision{Pane: p}
		switch {
		case f != FlightIdle:
			d.Action = ActionKeepInFlight
			d.Reason = "warm-pool pane has an in-flight turn; not reaped"
			poolKept++
		case idlePool < warmPoolMax:
			d.Action = ActionKeepPool
			d.Reason = fmt.Sprintf("warm-pool spare within bound %d", warmPoolMax)
			poolKept++
			idlePool++
		default:
			d.Action = ActionReap
			d.Reason = fmt.Sprintf("warm-pool excess over bound %d (no registry, idle)", warmPoolMax)
			poolReaped++
		}
		decisions[it.i] = d
	}

	for _, it := range rest {
		p := it.p
		d := Decision{Pane: p}
		if registered(p, names) {
			d.Action = ActionKeep
			d.Reason = "live registry entry"
			registeredN++
			decisions[it.i] = d
			continue
		}
		switch p.flight() {
		case FlightInFlight, FlightUnknown:
			d.Action = ActionKeepInFlight
			d.Reason = "unregistered but in-flight (or flight unknown); not reaped"
		default:
			d.Action = ActionReap
			d.Reason = "no registry entry and no in-flight turn"
			orphans++
		}
		decisions[it.i] = d
	}

	return Report{
		Decisions:  decisions,
		Registered: registeredN,
		Orphans:    orphans,
		PoolKept:   poolKept,
		PoolReaped: poolReaped,
		Cost:       EstimateCost(registeredN),
	}
}

// ParseListPanes parses `tmux list-panes -a -F` output. Fields, tab-separated:
//
//	session  window  pane_id  pane_pid  title  agent_name  session_id
func ParseListPanes(raw string) []Pane {
	var out []Pane
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\t")
		for len(f) < 7 {
			f = append(f, "")
		}
		pid, _ := strconv.Atoi(strings.TrimSpace(f[3]))
		p := Pane{
			Session:   strings.TrimSpace(f[0]),
			Window:    strings.TrimSpace(f[1]),
			ID:        strings.TrimSpace(f[2]),
			PID:       pid,
			Title:     f[4], // title may carry leading/trailing space
			AgentName: strings.TrimSpace(f[5]),
			SessionID: strings.TrimSpace(f[6]),
		}
		out = append(out, p)
	}
	return out
}

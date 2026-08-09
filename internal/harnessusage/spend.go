// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package harnessusage

import (
	"fmt"
	"io/fs"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/marcelocantos/jevons/internal/cost"
)

// Fleet spend measurement (🎯T392.6).
//
// CollectAll answers "what did each harness cost"; this answers "why".
// The T392 cost identity is
//
//	tokens = turns × calls-per-turn × context-per-call
//
// and each factor is a separate lever, so a report that sums tokens
// without splitting them tells you nothing about which lever to pull.
// Every field here is read from the provider's own per-turn usage frames
// (Grok turn_completed usage.modelUsage, Claude message.usage) and never
// from an estimate the fleet computes about itself — the executor must
// not own the numerator of its own gate.
//
// Cache reads are counted in Input for Grok, which reports inputTokens
// inclusive of cachedReadTokens, and excluded for Claude, which reports
// them separately. Context() is therefore the honest cross-provider
// quantity: what one API call actually carried.

// SpendReport is one window of fleet spend, decomposed along the axes the
// levers act on.
type SpendReport struct {
	Since time.Time `json:"since"`
	Until time.Time `json:"until"`

	Turns      int64 `json:"turns"`
	ModelCalls int64 `json:"model_calls"`
	Sessions   int   `json:"sessions"`

	Input     int64 `json:"input_tokens"`
	Output    int64 `json:"output_tokens"`
	CacheRead int64 `json:"cache_read_tokens"`

	// MeanContext is Input/ModelCalls: the average conversation size each
	// API call carried. This is what a context ceiling binds.
	MeanContext float64 `json:"mean_context"`

	// Coordinator work is the overseer and product owners — agents that
	// route rather than implement. Unattributed is spend whose session the
	// daemon never recorded a name for; it is reported, never folded into
	// a bucket to make the split look clean.
	CoordinatorInput  int64 `json:"coordinator_input"`
	ImplementerInput  int64 `json:"implementer_input"`
	UnattributedInput int64 `json:"unattributed_input"`

	// Cancelled turns billed a full context for work that was discarded.
	CancelledTurns int64 `json:"cancelled_turns"`
	CancelledInput int64 `json:"cancelled_input"`

	// UnparsedLines counts session-log lines too large to read. Non-zero
	// means these totals are a floor, not a measurement.
	UnparsedLines int `json:"unparsed_lines,omitempty"`

	ByAgent []AgentSpend `json:"by_agent"`
	Notes   []string     `json:"notes,omitempty"`
}

// AgentSpend is one agent's slice of a window.
type AgentSpend struct {
	Agent       string  `json:"agent"` // "" when the session was never named
	Coordinator bool    `json:"coordinator"`
	Sessions    int     `json:"sessions"`
	Turns       int64   `json:"turns"`
	ModelCalls  int64   `json:"model_calls"`
	Input       int64   `json:"input_tokens"`
	MeanContext float64 `json:"mean_context"`
}

// CoordinatorShare is the fraction of input spent by agents that route
// rather than implement. Unattributed spend stays in the denominator: a
// share that quietly drops what it cannot classify overstates itself.
func (r SpendReport) CoordinatorShare() float64 {
	if r.Input == 0 {
		return 0
	}
	return float64(r.CoordinatorInput) / float64(r.Input)
}

// CancelledShare is the fraction of input burned on discarded turns.
func (r SpendReport) CancelledShare() float64 {
	if r.Input == 0 {
		return 0
	}
	return float64(r.CancelledInput) / float64(r.Input)
}

// SpendArgs selects the window and supplies fleet attribution.
type SpendArgs struct {
	Collect CollectArgs

	// Since and Until bound the window; a zero Until means "now".
	Since time.Time
	Until time.Time

	// AgentBySession names the agent that owned each provider session.
	// Sessions absent from the map are counted as unattributed.
	AgentBySession map[string]string
	// Coordinators are the agent names that route rather than implement
	// (the overseer and the product owners).
	Coordinators map[string]bool

	// Harnesses restricts the walk. Empty means Grok and Claude both.
	// The 🎯T392 baseline is Grok-only, so reproducing it requires saying
	// so rather than silently summing whatever else ran in the window.
	Harnesses []Harness
}

func (a SpendArgs) wants(h Harness) bool {
	return len(a.Harnesses) == 0 || slices.Contains(a.Harnesses, h)
}

// CollectSpend walks the Grok and Claude session roots and decomposes the
// window. A harness whose root is missing contributes a note rather than
// an error: a machine that never ran Grok is not a broken measurement.
func CollectSpend(args SpendArgs) (SpendReport, error) {
	until := args.Until
	if until.IsZero() {
		until = args.Collect.now()
	}
	rep := SpendReport{Since: args.Since, Until: until}

	type agentKey struct {
		name  string
		coord bool
	}
	agents := map[agentKey]*AgentSpend{}
	agentSessions := map[agentKey]map[string]struct{}{}
	sessions := map[string]struct{}{}
	seenReq := map[string]struct{}{}

	add := func(ev *cost.Event, fallbackSession string) {
		sid := ev.SessionID
		if sid == "" {
			sid = fallbackSession
		}
		// Dedup only where the provider supplies a key. Grok's prompt_id
		// repeats across the turn's frames; Claude's request id is unique
		// per call. A source with no key is counted as-is rather than
		// guessed at (the failure mnemo reports for exactly this reason).
		if ev.RequestID != "" {
			k := sid + "\x00" + ev.RequestID
			if _, dup := seenReq[k]; dup {
				return
			}
			seenReq[k] = struct{}{}
		}
		sessions[sid] = struct{}{}

		name := args.AgentBySession[sid]
		key := agentKey{name: name, coord: name != "" && args.Coordinators[name]}
		a := agents[key]
		if a == nil {
			a = &AgentSpend{Agent: name, Coordinator: key.coord}
			agents[key] = a
			agentSessions[key] = map[string]struct{}{}
		}
		agentSessions[key][sid] = struct{}{}
		a.Turns++
		a.ModelCalls += ev.ModelCalls
		a.Input += ev.Usage.Input

		rep.Turns++
		rep.ModelCalls += ev.ModelCalls
		rep.Input += ev.Usage.Input
		rep.Output += ev.Usage.Output
		rep.CacheRead += ev.Usage.CacheRead
		switch {
		case name == "":
			rep.UnattributedInput += ev.Usage.Input
		case key.coord:
			rep.CoordinatorInput += ev.Usage.Input
		default:
			rep.ImplementerInput += ev.Usage.Input
		}
		if strings.EqualFold(ev.StopReason, "cancelled") {
			rep.CancelledTurns++
			rep.CancelledInput += ev.Usage.Input
		}
	}

	inWindow := func(ts time.Time) bool {
		if !args.Since.IsZero() && ts.Before(args.Since) {
			return false
		}
		return !ts.After(until)
	}

	// One pass per harness. Grok writes one turn_completed frame per turn
	// carrying the call count; Claude writes one assistant frame per API
	// call, so there a frame is a call. cost.ParseLine normalises both.
	scan := func(h Harness, defaultRel string, resolve func(string) string,
		match func(string) bool, sessionID func(string) string) {
		if !args.wants(h) {
			return
		}
		root, err := args.Collect.root(h, defaultRel)
		if err != nil {
			rep.Notes = append(rep.Notes, string(h)+" root unavailable: "+err.Error())
			return
		}
		dir := resolve(root)
		unparsed := 0
		_, werr := walkMatching(dir, args.Collect.MaxFiles, func(path string, _ fs.DirEntry) bool {
			return match(path)
		}, func(path string) error {
			sid := sessionID(path)
			skipped, err := forEachJSONLLine(path, func(line []byte) error {
				ev := cost.ParseLine(line, sid, until)
				if ev == nil || !inWindow(ev.Timestamp) {
					return nil
				}
				add(ev, sid)
				return nil
			})
			unparsed += skipped
			return err
		})
		if werr != nil {
			rep.Notes = append(rep.Notes, string(h)+" walk: "+werr.Error())
		}
		// Declared, never swallowed: a skipped line is spend this report
		// cannot see, and the reader has to know the floor is soft.
		if unparsed > 0 {
			rep.UnparsedLines += unparsed
			rep.Notes = append(rep.Notes, fmt.Sprintf(
				"%s: %d line(s) exceeded %d bytes and were skipped — any usage they carried is missing from these totals",
				h, unparsed, maxJSONLLine))
		}
	}
	scan(HarnessGrok, ".grok", resolveGrokSessions, isGrokUpdatesJSONL, sessionIDFromGrokPath)
	scan(HarnessClaude, ".claude", resolveClaudeProjects, isClaudeSessionJSONL, sessionIDFromClaudePath)

	rep.Sessions = len(sessions)
	if rep.ModelCalls > 0 {
		rep.MeanContext = float64(rep.Input) / float64(rep.ModelCalls)
	}
	for key, a := range agents {
		a.Sessions = len(agentSessions[key])
		if a.ModelCalls > 0 {
			a.MeanContext = float64(a.Input) / float64(a.ModelCalls)
		}
		rep.ByAgent = append(rep.ByAgent, *a)
	}
	sort.Slice(rep.ByAgent, func(i, j int) bool {
		if rep.ByAgent[i].Input != rep.ByAgent[j].Input {
			return rep.ByAgent[i].Input > rep.ByAgent[j].Input
		}
		return rep.ByAgent[i].Agent < rep.ByAgent[j].Agent
	})
	return rep, nil
}

// BaselineCoordinators names the agents that route rather than implement.
// Membership is by role, not by spend: an agent is a coordinator because
// its job is to dispatch work, which is also why it accumulates the whole
// fleet's chatter and carries the largest context.
var BaselineCoordinators = map[string]bool{
	"jevons":        true, // the overseer
	"jevons-po":     true,
	"orthograph-po": true,
	"claudia-po":    true,
	"vellum-po":     true,
	"bullseye-po":   true,
	"slacker-po":    true,
	"yourworld2-po": true,
}

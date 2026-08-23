// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package converge

import (
	"fmt"
	"strings"
	"time"
)

// Owner-visible mini-postmortems for closed impatience incidents (🎯T319).
//
// Every episode that made noise gets exactly one short report to the owner —
// what was stuck, how long, which rungs fired, how it cleared — no matter
// which pathway resolved it: daemon re-pressure, PO recovery, a root claim,
// human intervention, or spontaneous recovery. The ladder does not care why
// the gap went away, so neither does the report.
//
// The postmortem is a *report step*, never satisfaction (acceptance 4). It
// does not close a gap, and it is emphatically not a reason to keep human
// chrome lit: ActClearHuman is emitted by the ladder on the same tick the
// incident closes, independently of whether this report has been delivered
// yet — or ever.

// CloseCause is how an incident ended, for the report's "how it cleared"
// line. It is deliberately coarse: the ladder observes gap lifecycle, not
// the actor that fixed things, and the postmortem must never imply a
// pathway it cannot see.
//
// Named apart from the 🎯T316 Resolution vocabulary on purpose — that type
// classifies a reconcile outcome per tick, this one classifies an episode.
type CloseCause int

const (
	// ClosedBySatisfaction: the gap was still in the reconcile set and
	// 🎯T316 returned a satisfaction verdict — the agent is working on its
	// open mission (or produced a substantive turn).
	ClosedBySatisfaction CloseCause = iota
	// ClosedByDeparture: the gap left the reconcile set entirely — mission
	// closed or reaped, or the agent is gone. Equally satisfying, different
	// story to tell.
	ClosedByDeparture
	// ClosedByProviderResume: the provider began accepting calls again after
	// a refusal wall (🎯T454). Distinct from the agent resuming mission work
	// — a false all-clear of the second kind is how the 2026-08-15 spend-
	// limit outage withdrew human alerts for agents that were still blocked.
	ClosedByProviderResume
)

func (c CloseCause) String() string {
	switch c {
	case ClosedByDeparture:
		return "departed"
	case ClosedByProviderResume:
		return "provider_resume"
	default:
		return "satisfied"
	}
}

// Postmortem is one owner-visible report, ready to deliver as root.
type Postmortem struct {
	// Key identifies the incident for exactly-once bookkeeping.
	Key     string
	Agent   string
	Mission string
	Dwell   time.Duration
	Text    string
	// Satisfies is always false. A postmortem reports on a gap that has
	// already cleared; it never clears one itself (acceptance 4).
	Satisfies bool
}

// PostmortemSink delivers a report as root so it lands owner-visible in the
// daily chat. Implemented by the daemon; the pure layer only decides what to
// say and how often. A sink that errors leaves the report pending for the
// next tick — "always emits" (acceptance 3) outranks tidiness.
type PostmortemSink interface {
	DeliverPostmortem(text string) error
}

// PostmortemJournal turns closed incidents into exactly one delivered report
// each (acceptance 5). It is the coalescing point: an incident is recorded
// once ever, and a tick that closes several incidents delivers them as one
// digest rather than a burst of messages.
//
// Not safe for concurrent use; the converge loop drives it from the same
// goroutine as the Ladder.
type PostmortemJournal struct {
	seen    map[string]bool
	pending []Postmortem
}

// NewPostmortemJournal returns an empty journal.
func NewPostmortemJournal() *PostmortemJournal {
	return &PostmortemJournal{seen: map[string]bool{}}
}

// Record queues a report for each incident not seen before and returns the
// ones newly queued. Re-recording an incident is a no-op, so a caller that
// replays a tick cannot double-report.
func (j *PostmortemJournal) Record(incidents []Incident) []Postmortem {
	var fresh []Postmortem
	for _, inc := range incidents {
		key := incidentKey(inc)
		if key == "" || j.seen[key] {
			continue
		}
		j.seen[key] = true
		pm := Postmortem{
			Key:     key,
			Agent:   inc.Agent,
			Mission: inc.Mission,
			Dwell:   inc.Dwell,
			Text:    RenderPostmortem(inc),
		}
		j.pending = append(j.pending, pm)
		fresh = append(fresh, pm)
	}
	return fresh
}

// Pending returns the reports recorded but not yet delivered.
func (j *PostmortemJournal) Pending() []Postmortem {
	return append([]Postmortem(nil), j.pending...)
}

// Flush records the incidents and delivers everything outstanding through
// sink as a single coalesced message, returning the reports that went out.
// On sink error nothing is marked delivered and the reports stay pending for
// the next tick; the incident is still never re-reported once it lands.
//
// A nil sink leaves everything pending — useful before the daemon seam is
// wired, and honest about it: the reports are not lost, just undelivered.
func (j *PostmortemJournal) Flush(incidents []Incident, sink PostmortemSink) ([]Postmortem, error) {
	j.Record(incidents)
	if len(j.pending) == 0 {
		return nil, nil
	}
	if sink == nil {
		return nil, nil
	}
	if err := sink.DeliverPostmortem(RenderDigest(j.pending)); err != nil {
		return nil, err
	}
	sent := j.pending
	j.pending = nil
	return sent, nil
}

// incidentKey identifies an episode: one agent's gap that opened at one
// instant. A later episode for the same agent opens at a different instant
// (🎯T316 restarts the dwell clock) and reports separately.
func incidentKey(inc Incident) string {
	if inc.Agent == "" {
		return ""
	}
	return inc.Agent + "@" + inc.Opened.UTC().Format(time.RFC3339Nano)
}

// RenderPostmortem writes the owner-visible report for one incident: what
// was stuck, how long, which rungs fired, how it cleared (acceptance 3).
func RenderPostmortem(inc Incident) string {
	mission := inc.Mission
	if mission == "" {
		mission = "unbound mission"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**Impatience incident closed — %s** (%s)\n", inc.Agent, mission)
	fmt.Fprintf(&b, "- Stuck: open-mission idle for %s (%s → %s)\n",
		humanDwell(inc.Dwell), inc.Opened.Format("15:04"), inc.Closed.Format("15:04 MST"))
	fmt.Fprintf(&b, "- Rungs fired: %s\n", summarizeRungs(inc.Rungs))
	fmt.Fprintf(&b, "- Cleared: %s\n", clearedLine(inc))
	if inc.HumanLit {
		b.WriteString("- Your alert was raised and has been withdrawn automatically — no ack needed.\n")
	} else if inc.Acked {
		b.WriteString("- You dismissed the alert early; the gap cleared afterwards on its own.\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// RenderDigest coalesces a tick's reports into one owner-visible message so
// a burst of closures is a single interruption, not several.
func RenderDigest(pms []Postmortem) string {
	if len(pms) == 1 {
		return pms[0].Text
	}
	parts := make([]string, 0, len(pms)+1)
	parts = append(parts, fmt.Sprintf("**%d impatience incidents closed**", len(pms)))
	for _, pm := range pms {
		parts = append(parts, pm.Text)
	}
	return strings.Join(parts, "\n\n")
}

func clearedLine(inc Incident) string {
	switch inc.Cause {
	case ClosedByDeparture:
		return "mission closed or reaped — the gap left the reconcile set"
	case ClosedByProviderResume:
		return "provider began accepting calls again — not evidence the agent resumed its mission"
	default:
		return "agent returned to working on its open mission"
	}
}

// summarizeRungs collapses repeats: "re-pressure ×3, overseer-noise".
func summarizeRungs(rungs []Rung) string {
	if len(rungs) == 0 {
		return "none"
	}
	var (
		order []Rung
		count = map[Rung]int{}
	)
	for _, r := range rungs {
		if count[r] == 0 {
			order = append(order, r)
		}
		count[r]++
	}
	parts := make([]string, 0, len(order))
	for _, r := range order {
		if n := count[r]; n > 1 {
			parts = append(parts, fmt.Sprintf("%s ×%d", r, n))
		} else {
			parts = append(parts, r.String())
		}
	}
	return strings.Join(parts, ", ")
}

// humanDwell renders a duration the way an owner reads it: whole minutes
// past a minute, whole seconds below.
func humanDwell(d time.Duration) string {
	if d < time.Minute {
		return d.Round(time.Second).String()
	}
	return d.Round(time.Minute).String()
}

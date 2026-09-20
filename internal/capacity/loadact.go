// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package capacity

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// The governor's lever over load that is already running (🎯T708).
//
// 🎯T460 is a gate on new panes: AdmitSpawn refuses a worker at critical.
// It has nothing to say about load already present, which is precisely the
// case where the fleet is being starved. On 2026-09-20 one seat's detached
// `while :; do go test -race; done` loops held the host at load 121–127 for
// forty minutes; the governor read critical the whole time, refused spawns
// correctly, and named nothing. A governor that can see 127 and do nothing
// about it certifies the outage it exists to prevent.
//
// So the ladder below is about existing sources: named at elevated, carried
// to someone who can act at tight, and at critical either terminated (when
// nothing turn-scoped can reach the load) or put in front of the owner. The
// one invariant an oracle can hold the band to: at critical, a known load
// source is never merely observed.

// LoadSource is one seat's contribution to host load, as the governor reads
// it. It mirrors what internal/seatload measures; the policy stays pure over
// the numbers so it can be driven from a synthetic reading.
type LoadSource struct {
	Seat string
	// Procs is how many processes the seat owns beyond its root.
	Procs int
	// CPUPercent is their summed share of one core (100 == one core pinned).
	CPUPercent float64
	// Age is the age of the longest-running owned process.
	Age time.Duration
	// Unbounded is true when a command looks like a loop with no stop
	// condition — no timeout, no iteration cap, no deadline.
	Unbounded bool
	// Orphaned is how many owned processes have lost their parent. A
	// non-zero count is the sharp case: the seat's own turn, however it
	// ends, can no longer reach them.
	Orphaned int
	// SeatIdle is true when the seat's turn has ended. The 2026-09-20
	// specimen went idle with its loops still running, so no turn-scoped
	// cleanup could ever have fired.
	SeatIdle bool
	// Heaviest names the worst command, for the sentence a human reads.
	Heaviest string
}

// Unreachable reports whether the seat's own turn can still reach this
// load. Orphaned processes and an idle seat both mean it cannot.
func (s LoadSource) Unreachable() bool { return s.Orphaned > 0 || s.SeatIdle }

// LoadVerdict is what the governor does about an existing load source.
type LoadVerdict string

const (
	// LoadObserve: measured, nothing owed. Only below elevated.
	LoadObserve LoadVerdict = "observe"
	// LoadName: the source is named back to the seat that created it.
	LoadName LoadVerdict = "name"
	// LoadNotify: carried to someone who can act while the seat cannot.
	LoadNotify LoadVerdict = "notify"
	// LoadTerminate: the daemon reaps the process group itself.
	LoadTerminate LoadVerdict = "terminate"
)

// LoadAudience is who the action is addressed to.
type LoadAudience string

const (
	AudienceSeat         LoadAudience = "seat"
	AudienceProductOwner LoadAudience = "product_owner"
	AudienceOwner        LoadAudience = "owner"
)

// LoadAction is one decision about one source.
type LoadAction struct {
	Source   LoadSource   `json:"source"`
	Verdict  LoadVerdict  `json:"verdict"`
	Audience LoadAudience `json:"audience"`
	Pressure Pressure     `json:"pressure"`
	Reason   string       `json:"reason"`
}

// ActOnLoad decides what to do about load already running, given the band.
//
// Oracle shape: at critical a known source yields at least one action that
// is not LoadObserve, and an unreachable one yields LoadTerminate; the
// normal-band control yields nothing at all, so a fix that simply shouts at
// every band fails it.
func ActOnLoad(a Assessment, sources []LoadSource) []LoadAction {
	live := make([]LoadSource, 0, len(sources))
	for _, s := range sources {
		if strings.TrimSpace(s.Seat) == "" || s.Procs <= 0 {
			continue
		}
		live = append(live, s)
	}
	if len(live) == 0 || a.Pressure <= PressureNormal {
		return nil
	}
	sort.SliceStable(live, func(i, j int) bool {
		if live[i].CPUPercent != live[j].CPUPercent {
			return live[i].CPUPercent > live[j].CPUPercent
		}
		if live[i].Procs != live[j].Procs {
			return live[i].Procs > live[j].Procs
		}
		return live[i].Seat < live[j].Seat
	})

	out := make([]LoadAction, 0, len(live))
	for i, s := range live {
		act := LoadAction{Source: s, Pressure: a.Pressure}
		switch a.Pressure {
		case PressureElevated:
			// Only the heaviest: an elevated host is a hint, not an alarm,
			// and an alarm per seat is an alarm nobody reads.
			if i > 0 {
				continue
			}
			act.Verdict, act.Audience = LoadName, AudienceSeat
			act.Reason = "host elevated; naming the heaviest seat-created load source to the seat that created it"
		case PressureTight:
			act.Verdict, act.Audience = LoadNotify, AudienceProductOwner
			act.Reason = "host tight; the seat's product owner is told, because an idle seat cannot act on its own load"
			if s.Unreachable() {
				act.Reason = "host tight; this load is unreachable from the seat's own turn, so it is carried to its product owner"
			}
		default: // PressureCritical
			if s.Unreachable() {
				act.Verdict, act.Audience = LoadTerminate, AudienceOwner
				act.Reason = "host critical and nothing turn-scoped can reach this load; the daemon reaps the seat's process group (🎯T708)"
			} else {
				act.Verdict, act.Audience = LoadNotify, AudienceOwner
				act.Reason = "host critical; the seat is still taking turns, so it is told to bound its own load before the daemon does"
			}
		}
		out = append(out, act)
	}
	return out
}

// ActedOn reports whether the actions do something about the load rather
// than only record it. This is the clause an oracle holds the critical band
// to: measured-and-tolerated is the failure being fixed.
func ActedOn(actions []LoadAction) bool {
	for _, a := range actions {
		if a.Verdict != LoadObserve {
			return true
		}
	}
	return false
}

// FormatLoadAction is the sentence the seat, the PO or the owner reads.
func FormatLoadAction(a LoadAction) string {
	bound := "bounded"
	if a.Source.Unbounded {
		bound = "no timeout, iteration cap or deadline"
	}
	reach := "reachable from the seat's turn"
	if a.Source.Unreachable() {
		reach = fmt.Sprintf("unreachable from the seat's turn (%d orphaned, seat_idle=%v)", a.Source.Orphaned, a.Source.SeatIdle)
	}
	return fmt.Sprintf("LOAD %s (🎯T708): seat %q holds %d process(es) at %.0f%% CPU for %s — %s, %s. %s. Heaviest: %s",
		strings.ToUpper(string(a.Verdict)), a.Source.Seat, a.Source.Procs, a.Source.CPUPercent,
		a.Source.Age.Round(time.Second), bound, reach, a.Reason, strings.TrimSpace(a.Source.Heaviest))
}

// QuietVerdict tells a seat that is being starved apart from one that is
// stuck (🎯T708 clause 4).
type QuietVerdict string

const (
	// QuietActive: the seat is inside the bar; nothing to say.
	QuietActive QuietVerdict = "active"
	// QuietStalled: quiet past the bar on a host with room. The seat owes
	// an explanation.
	QuietStalled QuietVerdict = "stalled"
	// QuietStarved: quiet past the bar while the host has no CPU left to
	// give. The seat owes nothing; the load does.
	QuietStarved QuietVerdict = "starved"
)

// HostLoadCritical reports whether the host's run queue — not the budget,
// not a provider cap — is the saturated dimension.
func HostLoadCritical(a Assessment) bool {
	return a.LoadAverageHeadroom != unknownHeadroom && a.LoadAverageHeadroom <= 0
}

// ClassifyQuiet decides what a seat's silence means. On 2026-09-20 every
// running claude worker was far past the stall bar — 71, 48 and 47 minutes
// — while the host sat at load 121. Not one of them was stuck; they were
// starved by a sibling's loops, and counting them against the stall bar
// would have had the fleet nudging, reminting and reaping seats whose only
// fault was wanting a CPU.
func ClassifyQuiet(quietFor, bar time.Duration, a Assessment) (QuietVerdict, string) {
	if bar <= 0 || quietFor < bar {
		return QuietActive, ""
	}
	if HostLoadCritical(a) {
		return QuietStarved, fmt.Sprintf(
			"quiet %s (bar %s) while host load is critical: starved, not stalled — the stall bar does not count this (🎯T708)",
			quietFor.Round(time.Second), bar.Round(time.Second))
	}
	return QuietStalled, fmt.Sprintf("quiet %s past the %s stall bar on a host with room",
		quietFor.Round(time.Second), bar.Round(time.Second))
}

// CountsAgainstStallBar reports whether a verdict may drive the stuck-seat
// machinery (nudge, remint, reap).
func CountsAgainstStallBar(v QuietVerdict) bool { return v == QuietStalled }

// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package capacity

import (
	"strings"
	"testing"
	"time"
)

// The 2026-09-20 source: one seat, 26 detached shells, forty minutes old,
// no bound, parent gone, and the seat itself idle.
func burnSource() LoadSource {
	return LoadSource{
		Seat:       "cl-t33-codex-load",
		Procs:      26,
		CPUPercent: 2400,
		Age:        40 * time.Minute,
		Unbounded:  true,
		Orphaned:   26,
		SeatIdle:   true,
		Heaviest:   "/bin/sh -c while :; do go test -race -count=1 ./...; done",
	}
}

func band(p Pressure, loadHeadroom float64) Assessment {
	return Assessment{Pressure: p, LoadAverageHeadroom: loadHeadroom, Headroom: loadHeadroom}
}

// The clause the target is about: at critical, an existing load source is
// acted on and named, not merely observed.
func TestT708CriticalActsOnLoadAlreadyRunning(t *testing.T) {
	acts := ActOnLoad(band(PressureCritical, 0), []LoadSource{burnSource()})
	if len(acts) == 0 {
		t.Fatal("critical with a known load source produced no action — the governor certified the outage it exists to prevent")
	}
	if !ActedOn(acts) {
		t.Fatal("every action was observe-only at critical")
	}
	if acts[0].Verdict != LoadTerminate {
		t.Fatalf("verdict = %q, want terminate: the seat is idle and every process orphaned, so nothing turn-scoped can reach this load", acts[0].Verdict)
	}
	if acts[0].Audience != AudienceOwner {
		t.Fatalf("audience = %q, want owner", acts[0].Audience)
	}
	line := FormatLoadAction(acts[0])
	for _, want := range []string{"cl-t33-codex-load", "26", "🎯T708", "unreachable"} {
		if !strings.Contains(line, want) {
			t.Errorf("action line does not name %q: %s", want, line)
		}
	}
}

// The control an over-broad fix fails: a quiet host owes nothing.
func TestT708NormalBandActsOnNothing(t *testing.T) {
	if acts := ActOnLoad(band(PressureNormal, 1), []LoadSource{burnSource()}); len(acts) != 0 {
		t.Fatalf("normal band produced %d action(s); measuring is not acting", len(acts))
	}
}

// A seat still taking turns is told to bound its own load; the daemon does
// not reach into a live turn.
func TestT708CriticalReachableSeatIsNotifiedNotTerminated(t *testing.T) {
	src := burnSource()
	src.SeatIdle, src.Orphaned = false, 0
	acts := ActOnLoad(band(PressureCritical, 0), []LoadSource{src})
	if len(acts) != 1 || acts[0].Verdict != LoadNotify {
		t.Fatalf("acts = %+v, want a single notify", acts)
	}
	if acts[0].Audience != AudienceOwner {
		t.Fatalf("audience = %q, want owner at critical", acts[0].Audience)
	}
}

func TestT708LadderNamesThenCarriesThenActs(t *testing.T) {
	src := burnSource()
	quiet := LoadSource{Seat: "jv-quiet", Procs: 1, CPUPercent: 4}
	cases := []struct {
		name     string
		a        Assessment
		wantN    int
		wantVerd LoadVerdict
		wantAud  LoadAudience
	}{
		{"elevated names the heaviest to its seat", band(PressureElevated, 0.4), 1, LoadName, AudienceSeat},
		{"tight carries it to the product owner", band(PressureTight, 0.1), 2, LoadNotify, AudienceProductOwner},
		{"critical acts", band(PressureCritical, 0), 2, LoadTerminate, AudienceOwner},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			acts := ActOnLoad(c.a, []LoadSource{quiet, src})
			if len(acts) != c.wantN {
				t.Fatalf("acts = %d, want %d: %+v", len(acts), c.wantN, acts)
			}
			// Heaviest first, always: an alarm nobody can rank is an alarm
			// nobody reads.
			if acts[0].Source.Seat != src.Seat {
				t.Fatalf("first action names %q, want the heaviest source %q", acts[0].Source.Seat, src.Seat)
			}
			if acts[0].Verdict != c.wantVerd || acts[0].Audience != c.wantAud {
				t.Fatalf("verdict/audience = %q/%q, want %q/%q", acts[0].Verdict, acts[0].Audience, c.wantVerd, c.wantAud)
			}
		})
	}
}

func TestT708SourcesWithNoProcessesAreNotSources(t *testing.T) {
	acts := ActOnLoad(band(PressureCritical, 0), []LoadSource{{Seat: "jv-empty"}, {Procs: 4}})
	if len(acts) != 0 {
		t.Fatalf("acts = %+v, want none: a seat that owns no processes is not a load source", acts)
	}
}

// Clause 4: starvation is not a stall, and the stall bar must not count it.
func TestT708QuietUnderCriticalLoadIsStarvedNotStalled(t *testing.T) {
	bar := 20 * time.Minute
	v, why := ClassifyQuiet(71*time.Minute, bar, band(PressureCritical, 0))
	if v != QuietStarved {
		t.Fatalf("verdict = %q, want starved: the host had no CPU to give", v)
	}
	if CountsAgainstStallBar(v) {
		t.Fatal("a starved seat was counted against the stall bar — the fleet would nudge, remint and reap seats whose only fault was wanting a CPU")
	}
	if !strings.Contains(why, "starved") {
		t.Errorf("reason does not say starved: %s", why)
	}
}

func TestT708QuietOnAHostWithRoomIsStillAStall(t *testing.T) {
	bar := 20 * time.Minute
	v, _ := ClassifyQuiet(71*time.Minute, bar, band(PressureNormal, 1))
	if v != QuietStalled || !CountsAgainstStallBar(v) {
		t.Fatalf("verdict = %q: a quiet seat on an idle host still owes an explanation", v)
	}
}

func TestT708QuietInsideTheBarIsActive(t *testing.T) {
	if v, _ := ClassifyQuiet(time.Minute, 20*time.Minute, band(PressureCritical, 0)); v != QuietActive {
		t.Fatalf("verdict = %q, want active", v)
	}
}

// Unknown load is not critical load: a missing reading must never mark a
// seat starved, which would silence a real stall.
func TestT708UnknownLoadIsNotStarvation(t *testing.T) {
	a := Assessment{Pressure: PressureCritical, LoadAverageHeadroom: unknownHeadroom}
	if HostLoadCritical(a) {
		t.Fatal("unknown load-average headroom read as critical")
	}
	if v, _ := ClassifyQuiet(time.Hour, time.Minute, a); v != QuietStalled {
		t.Fatalf("verdict = %q, want stalled when the load reading is unknown", v)
	}
}

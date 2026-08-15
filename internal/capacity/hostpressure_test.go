// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package capacity

import (
	"strings"
	"testing"
)

// The 2026-08-15 overload, as numbers: a 16-core M4 Max at a 1-minute load
// average of 247 with swap pinned at 36.3 of 37.9 GB. Admission control read
// this host as pressure "normal", load headroom 1, every class admitted at
// tier "full" — because cost and tokens, the only dimensions it had, were both
// fine. These fixtures are the reading that was available and ignored.
const (
	meltedLoad1 = 247.0
	meltedCores = 16
	idleLoad1   = 3.2
)

var (
	meltedSwapUsed  = gibBytes(36.3)
	meltedSwapTotal = gibBytes(37.9)
	idleSwapUsed    = gibBytes(4)
)

// gibBytes is the fixtures' unit: the kernel reports swap in gibibytes.
func gibBytes(g float64) int64 { return int64(g * (1 << 30)) }

// meltedHost is the snapshot as it stood during the overload: nothing but the
// host is saturated, which is precisely why the old policy admitted everything.
func meltedHost() Snapshot {
	return Snapshot{
		HostLoad1:          meltedLoad1,
		HostCores:          meltedCores,
		HostSwapUsedBytes:  meltedSwapUsed,
		HostSwapTotalBytes: meltedSwapTotal,
		HostSource:         "test fixture (2026-08-15)",
	}
}

// idleHost is the control: the same machine doing ordinary work. An
// over-broad fix that simply refuses background work fails here.
func idleHost() Snapshot {
	return Snapshot{
		HostLoad1:          idleLoad1,
		HostCores:          meltedCores,
		HostSwapUsedBytes:  idleSwapUsed,
		HostSwapTotalBytes: meltedSwapTotal,
		HostSource:         "test fixture (idle)",
	}
}

func TestT463HostSaturationDefersBackground(t *testing.T) {
	pol := DefaultPolicy()

	a := Assess(meltedHost(), pol)
	if a.Pressure == PressureNormal {
		t.Fatalf("melted host assessed as pressure normal: %+v", a)
	}
	if a.LoadHeadroom > 0 {
		t.Errorf("load headroom %v, want <= 0 at load %.0f on %d cores", a.LoadHeadroom, meltedLoad1, meltedCores)
	}
	if a.HostHeadroom > 0 {
		t.Errorf("host headroom %v, want <= 0", a.HostHeadroom)
	}
	var named bool
	for _, r := range a.Reasons {
		if strings.Contains(r, "host load average") {
			named = true
		}
	}
	if !named {
		t.Errorf("no reason names the host reading: %q", a.Reasons)
	}

	// A background class asking for a cycle is deferred, and the machine token
	// says the host — not the budget — is why.
	d := Admit(Request{Class: ClassResearch, Name: "research.schedule", Degradable: true}, meltedHost(), pol)
	if d.Verdict != VerdictDefer {
		t.Errorf("research verdict %q, want %q (%s)", d.Verdict, VerdictDefer, d.Detail)
	}
	if d.Reason != ReasonHostSaturated {
		t.Errorf("research reason %q, want %q", d.Reason, ReasonHostSaturated)
	}

	// 🎯T359 ordering survives: the owner and open Build work are never the
	// thing that gets shed for a busy host.
	for _, class := range []Class{ClassOwnerTurn, ClassBuildMission} {
		if d := Admit(Request{Class: class}, meltedHost(), pol); !d.Admitted() {
			t.Errorf("%s was %q at host saturation, want admitted (%s)", class, d.Verdict, d.Detail)
		}
	}
}

func TestT463IdleHostStillAdmitsBackground(t *testing.T) {
	pol := DefaultPolicy()

	a := Assess(idleHost(), pol)
	if a.Pressure != PressureNormal {
		t.Fatalf("idle host assessed as %s, want normal: %+v", a.Pressure, a)
	}
	d := Admit(Request{Class: ClassResearch, Name: "research.schedule", Degradable: true}, idleHost(), pol)
	if d.Verdict != VerdictAdmit || d.Tier != TierFull {
		t.Errorf("idle host gave research %q tier %q, want %q/%q (%s)",
			d.Verdict, d.Tier, VerdictAdmit, TierFull, d.Detail)
	}
}

// A host can run out of memory without the run queue growing: the swap
// dimension has to bind on its own. Removing it is the mutation this test
// exists to catch — the melted fixture reads healthy again the moment swap
// stops counting.
func TestT463SwapBindsWithoutLoad(t *testing.T) {
	pol := DefaultPolicy()
	snap := Snapshot{
		HostLoad1:          8, // half a core deep: nothing at all
		HostCores:          meltedCores,
		HostSwapUsedBytes:  meltedSwapUsed,
		HostSwapTotalBytes: meltedSwapTotal,
	}
	a := Assess(snap, pol)
	if a.Pressure != PressureCritical {
		t.Fatalf("swap at %.0f%% assessed as %s, want critical: %+v",
			float64(meltedSwapUsed)/float64(meltedSwapTotal)*100, a.Pressure, a)
	}
	if d := Admit(Request{Class: ClassResearch}, snap, pol); d.Verdict != VerdictDefer {
		t.Errorf("research verdict %q under swap exhaustion, want defer", d.Verdict)
	}

	// Control: the same load with swap half empty is an ordinary machine.
	snap.HostSwapUsedBytes = idleSwapUsed
	if a := Assess(snap, pol); a.Pressure != PressureNormal {
		t.Errorf("idle swap assessed as %s, want normal: %+v", a.Pressure, a)
	}
}

// provider_soft_caps read {claude: 0, codex: 6, grok: 12} with 47 claude
// agents running, and 0 meant "no limit" — so the provider carrying every
// agent was invisible to admission. 0 is an unpublished cap now, not an
// unlimited one.
func TestT463ProviderZeroCapIsNotUnlimited(t *testing.T) {
	pol := DefaultPolicy()
	snap := Snapshot{
		ProviderSoftCaps: map[string]int{"claude": 0, "codex": 6, "grok": 12},
		ProviderLoad:     map[string]int{"claude": 47, "codex": 1, "grok": 2},
	}
	a := Assess(snap, pol)
	if a.LoadHeadroom != 0 {
		t.Errorf("load headroom %v with 47 agents on an uncapped provider, want 0", a.LoadHeadroom)
	}
	if a.Pressure != PressureCritical {
		t.Errorf("pressure %s, want critical: %+v", a.Pressure, a)
	}

	// Control: the same uncapped provider carrying a normal number of agents
	// must not be treated as saturated.
	snap.ProviderLoad = map[string]int{"claude": 3, "codex": 1, "grok": 2}
	if a := Assess(snap, pol); a.Pressure != PressureNormal {
		t.Errorf("3 agents on an uncapped provider assessed as %s, want normal: %+v", a.Pressure, a)
	}
}

// An unread host contributes nothing rather than a fabricated ceiling: unknown
// and fine are different statements, and conflating them is the defect.
func TestT463UnreadHostIsUnknownNotFine(t *testing.T) {
	a := Assess(Snapshot{}, DefaultPolicy())
	if a.HostHeadroom != unknownHeadroom {
		t.Errorf("host headroom %v with no reading, want %v", a.HostHeadroom, unknownHeadroom)
	}
	if a.Pressure != PressureNormal {
		t.Errorf("empty snapshot assessed as %s, want normal", a.Pressure)
	}
}

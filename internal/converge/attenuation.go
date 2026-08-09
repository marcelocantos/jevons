// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package converge

import (
	"time"

	"github.com/marcelocantos/jevons/internal/converge/attenuate"
)

// 🎯T318 wiring: the progress-attenuation policy modulates this ladder's
// timing. The policy itself is pure and lives in the attenuate subpackage;
// this file is the whole of its contact with the ladder.
//
// Attenuation reaches the ladder through exactly two values, both produced by
// attenuate.Attenuator.Adjust:
//
//   - EffectiveDwell — the gap's dwell less the delay progress has bought.
//     Feeding it to dueRung shifts every threshold out by that much.
//   - Ceiling — the loudest rung permitted while progress holds. It is passed
//     into dueRung rather than clamped onto its result, so a rung the ceiling
//     excludes is skipped and quieter rungs still respect their own
//     anti-thrash intervals.
//
// What attenuation cannot do is end anything. The gap stays in 🎯T316's
// reconcile set, stays Tracked, and keeps its actuator: the ceiling floors at
// RungRepressure, so progress buys quiet from the overseer and the owner and
// never from the agent sitting on an open mission. Only satisfaction — T316's
// verdict, or the gap leaving the set — drops attenuation state, in the same
// close path that closes the incident.

// SetAttenuator installs the 🎯T318 progress-attenuation policy. Nil (the
// default) runs the ladder at full timing, which is exactly the pre-T318
// behaviour. Safe to call before or between ticks; not during one.
func (l *Ladder) SetAttenuator(a *attenuate.Attenuator) {
	l.atten = a
}

// ObserveProgress records one progress signal against an agent's open gap.
// The daemon calls this from the paths that witness real movement: the 🎯T315
// actuator when a re-pressure deliver lands (weak — buys delay, never
// de-escalates), and the fleet paths that see a parent enter working and
// deliver, a restart or rehydrate, a fix worker spawned against the gap, or
// the stuck agent transitioning idle→working.
//
// Signals that are not progress (notify-only, empty ack) may be passed in
// freely; the policy records them as seen and grants nothing.
//
// This never marks the gap satisfied, whatever the signal — including
// SignalTargetWorking, which 🎯T316 separately reads as satisfaction through
// Gap.Satisfied. Attenuation observes; it does not adjudicate.
func (l *Ladder) ObserveProgress(agent string, kind attenuate.SignalKind, now time.Time) {
	if l.atten == nil || agent == "" {
		return
	}
	l.atten.Observe(agent, attenuate.Signal{Kind: kind, At: now}, now)
}

// Attenuation reports the current modulation for an agent, for logs, the
// incident record, and tests. With no attenuator installed it reports the
// unattenuated ladder: full dwell, full ceiling.
func (l *Ladder) Attenuation(agent string, now time.Time) attenuate.Adjustment {
	st, ok := l.agents[agent]
	if !ok {
		return attenuate.Adjustment{Ceiling: attenuate.CeilingHuman, GapOpen: false, Reason: "gap not tracked"}
	}
	raw := now.Sub(st.since)
	dwell, ceiling := l.attenuated(agent, raw, now)
	return attenuate.Adjustment{
		EffectiveDwell: dwell,
		Ceiling:        attenuate.Ceiling(ceiling),
		Credit:         raw - dwell,
		Attenuated:     dwell < raw || ceiling < RungHumanAlert,
		GapOpen:        true,
	}
}

// attenuated returns the dwell and the ceiling dueRung should decide on.
// Without an attenuator installed it is the identity: raw dwell, full ceiling.
func (l *Ladder) attenuated(agent string, dwell time.Duration, now time.Time) (time.Duration, Rung) {
	if l.atten == nil {
		return dwell, RungHumanAlert
	}
	adj := l.atten.Adjust(agent, dwell, now)
	// Ceiling ordinals mirror Rung, guarded by
	// attenuate.TestCeilingOrdinalsMirrorLadderRungs and by
	// TestAttenuationCeilingMapsOntoRung here.
	return adj.EffectiveDwell, Rung(adj.Ceiling)
}

// forgetAttenuation drops an agent's attenuation state. It is called only
// from the close path — satisfaction or departure from the reconcile set —
// and never because progress made the agent look busy, which is the
// progress-as-satisfaction error 🎯T318 exists to prevent.
func (l *Ladder) forgetAttenuation(agent string) {
	if l.atten != nil {
		l.atten.Forget(agent)
	}
}

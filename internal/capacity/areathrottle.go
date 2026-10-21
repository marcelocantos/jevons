// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

package capacity

import "strings"

// 🎯T1104 — merge-conflict-risk fan-out throttle.
//
// The flat per-backend seat cap (session soft caps, 🎯T566.2 seat-count
// runaway) is a crude proxy for the owner's real hazard: concurrent workers
// landing writes on overlapping files/packages, which is what produces
// merge-conflict churn against local master (🎯T104). The owner is
// explicitly willing to spend more tokens for more parallel throughput as
// long as writers stay disjoint — gating on a flat number of seats answers
// the wrong question.
//
// This file is the pure decision half: given the file/package areas a
// candidate spawn is predicted to touch, and the areas every currently
// in-flight worker is already touching, decide admit (disjoint — allowed to
// exceed the flat cap), serialize (overlaps a live writer — queue behind
// it), or fall back to the flat cap when the footprint is unknown (we
// cannot prove disjointness, so we do not gamble on it). It composes with,
// rather than replaces, AdmitSpawn's seat-count check: a caller with
// predicted areas available should consult this first and only fall through
// to the flat cap when AreaThrottleUnknownFootprint is returned.

// AreaThrottleVerdict is the outcome of one area-overlap admission check.
type AreaThrottleVerdict string

const (
	// AreaThrottleAdmitDisjoint: the candidate's predicted areas do not
	// overlap any in-flight worker's areas. Admitted regardless of the flat
	// per-backend seat cap — this is the whole point of 🎯T1104.
	AreaThrottleAdmitDisjoint AreaThrottleVerdict = "admit_disjoint"
	// AreaThrottleSerialize: the candidate overlaps at least one live
	// writer's predicted areas. Queue behind it rather than spawn
	// concurrently, to avoid simultaneous writers on the same files/packages.
	AreaThrottleSerialize AreaThrottleVerdict = "serialize"
	// AreaThrottleUnknownFootprint: neither the candidate nor the comparison
	// can establish disjointness (no predicted areas on one or both sides).
	// Overlap cannot be ruled out, so the caller falls back to the flat
	// per-backend cap rather than assuming safety.
	AreaThrottleUnknownFootprint AreaThrottleVerdict = "unknown_footprint"
)

// TargetFootprint is one worker's (or candidate spawn's) predicted set of
// touched areas: file paths, directories, or Go package import paths.
// Areas are caller-supplied best-effort predictions (e.g. from the
// target's acceptance text, a prior commit's diff, or a declared scope) —
// this package only computes overlap, never predicts the areas itself.
type TargetFootprint struct {
	// TargetID names the bullseye target (or worker) this footprint
	// belongs to, for reporting in AreaThrottleDecision.ConflictsWith.
	TargetID string
	// Areas are the predicted touched paths/packages. Empty means unknown
	// footprint.
	Areas []string
}

// normalizeArea lowercases, trims, and strips a trailing slash so
// "internal/capacity/" and "internal/capacity" compare equal.
func normalizeArea(a string) string {
	a = strings.ToLower(strings.TrimSpace(a))
	return strings.TrimSuffix(a, "/")
}

// areasOverlap reports whether two single areas conflict: exact match, or
// one is a path/package prefix of the other (so "internal/capacity" and
// "internal/capacity/spawn.go" overlap, but "internal/capacity" and
// "internal/capacityx" do not — the prefix boundary is a path separator).
func areasOverlap(a, b string) bool {
	a, b = normalizeArea(a), normalizeArea(b)
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	short, long := a, b
	if len(long) < len(short) {
		short, long = long, short
	}
	if !strings.HasPrefix(long, short) {
		return false
	}
	// long == short + rest; overlap only if rest starts at a boundary
	// (path separator), not mid-component.
	rest := long[len(short):]
	return strings.HasPrefix(rest, "/")
}

// FootprintsOverlap reports whether two footprints share any conflicting
// area. Two empty (unknown) footprints are NOT reported as overlapping —
// callers must check for unknown footprints separately via
// HasUnknownFootprint, since "unknown" is a distinct verdict from both
// "disjoint" and "overlapping".
func FootprintsOverlap(a, b TargetFootprint) bool {
	for _, x := range a.Areas {
		for _, y := range b.Areas {
			if areasOverlap(x, y) {
				return true
			}
		}
	}
	return false
}

// HasUnknownFootprint reports whether f carries no predicted areas at all.
func HasUnknownFootprint(f TargetFootprint) bool {
	return len(f.Areas) == 0
}

// AreaThrottleDecision is the outcome of AdmitByAreaOverlap.
type AreaThrottleDecision struct {
	Verdict AreaThrottleVerdict
	// ConflictsWith names the in-flight TargetIDs the candidate overlaps
	// (AreaThrottleSerialize only).
	ConflictsWith []string
	// Reason is a stable code for logs/tests.
	Reason string
	// Detail is the human-readable line.
	Detail string
}

// AdmitByAreaOverlap decides whether a candidate spawn may proceed
// concurrently with the given in-flight workers, based on predicted
// file/package area overlap rather than a flat seat count (🎯T1104).
//
//   - If the candidate's footprint is unknown, or every in-flight footprint
//     is unknown, overlap cannot be ruled out: verdict is
//     AreaThrottleUnknownFootprint and the caller should fall back to the
//     flat per-backend cap (AdmitSpawn / AdmitSpawnDest).
//   - If the candidate overlaps one or more in-flight footprints with known
//     areas, verdict is AreaThrottleSerialize naming every conflicting
//     TargetID — queue the candidate behind them.
//   - Otherwise (every in-flight footprint is either known-disjoint or
//     itself unknown-but-absent from consideration) verdict is
//     AreaThrottleAdmitDisjoint: admit even above the flat cap.
//
// Workers with an unknown footprint are conservatively treated as
// potentially overlapping everything only when THEY are the candidate (see
// above); an unknown in-flight worker does not itself block an otherwise
// disjoint candidate, because a flat unknown-vs-unknown collision would
// degenerate back into "no two workers may ever run together". Callers that
// want that stricter reading can pre-filter inFlight to drop entries with
// HasUnknownFootprint before calling.
func AdmitByAreaOverlap(candidate TargetFootprint, inFlight []TargetFootprint) AreaThrottleDecision {
	if HasUnknownFootprint(candidate) {
		return AreaThrottleDecision{
			Verdict: AreaThrottleUnknownFootprint,
			Reason:  "candidate_footprint_unknown",
			Detail:  "candidate " + candidate.TargetID + " has no predicted touched areas; falling back to the flat per-backend seat cap (🎯T1104)",
		}
	}

	var conflicts []string
	for _, w := range inFlight {
		if HasUnknownFootprint(w) {
			continue
		}
		if FootprintsOverlap(candidate, w) {
			conflicts = append(conflicts, w.TargetID)
		}
	}

	if len(conflicts) > 0 {
		return AreaThrottleDecision{
			Verdict:       AreaThrottleSerialize,
			ConflictsWith: conflicts,
			Reason:        "overlapping_areas",
			Detail:        "candidate " + candidate.TargetID + " overlaps in-flight target(s) " + strings.Join(conflicts, ", ") + "; serializing to avoid simultaneous writers (🎯T1104)",
		}
	}

	return AreaThrottleDecision{
		Verdict: AreaThrottleAdmitDisjoint,
		Reason:  "disjoint_areas",
		Detail:  "candidate " + candidate.TargetID + " touches no area any in-flight worker is writing; admitted regardless of the flat per-backend seat cap (🎯T1104)",
	}
}

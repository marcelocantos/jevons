// Copyright 2026 Marcelo Cantos
// SPDX-License-Identifier: Apache-2.0

// Package gate makes a gate's exit status come from the gate command itself,
// so a fleet worker cannot report a green it did not get (🎯T386), and so a
// status the harness relays wrongly can still be read back in band (🎯T396).
//
// # The defect
//
// Oracle-first (🎯T31) demands that a finish report cite executable evidence.
// It assumes the cited evidence is honestly read. Three ways that assumption
// broke in one session, all of them sincere:
//
//   - A pipeline's status is the LAST command's. `go test ./... | tail -20`
//     reports tail's success, which is unconditional. claudia-po found a
//     reported "make test exit=0" that was tail's status while the suite had
//     died on a timeout panic.
//   - zsh does not have bash's PIPESTATUS. A worker ran
//     `make test-web 2>&1 | tail -25; echo "EXIT=${PIPESTATUS[0]}"` under zsh
//     and printed a bare `EXIT=` — a status never read at all, one careless
//     glance from being reported as green. (zsh spells it `pipestatus`, and
//     indexes from 1, so `${pipestatus[0]}` is empty too.)
//   - The harness itself reported a background gate as "exit code 0" when
//     go test had exited 1; only the logged $? told the truth.
//
// A fabricated green is worse than no test: it is cited as evidence and it
// retires a target.
//
// # The mechanism
//
// Run executes the gate as a process — no shell, no pipeline, nothing between
// the command and its wait status — captures its output to a file, and writes
// a Record carrying the real status. The Record's Attestation line is what a
// worker pastes into a finish report, and it says GREEN only when the status
// was zero AND the output does not contradict it.
//
// Three properties make a false green hard rather than merely discouraged:
//
//  1. There is no pipeline to mask the status, so citing "exit=0" cites the
//     command's own wait status.
//  2. An unknown status renders as "exit=unknown", never as zero. A wrapper
//     that cannot vouch for a status must say so (🎯T396 acceptance 3).
//  3. The Record outlives the run and the shell. When the harness misreports
//     a background command, `gate last` reads the truth off disk, in band.
//  4. A host-killed run (SIGKILL / exit 137 / "[killed]") is KILLED, not RED
//     and not GREEN — a shot process decided nothing (🎯T461).
//
// FlagFalseGreen closes the loop at report time: it reads a finish report and
// flags a green claim whose own cited evidence contradicts it, or whose cited
// attestation does not exist or was not green. The daemon runs it on the
// notify path, so a false green reaches the overseer already marked.
package gate

import (
	"strings"
)

// Verdict is how a completed gate run may be reported. It is deliberately
// narrower than "exit status": a run that exited zero while printing a
// timeout panic is not something a worker may cite as a green.
type Verdict string

const (
	// VerdictGreen: the command exited zero and its output does not
	// contradict that. The only verdict that may be cited as a pass.
	VerdictGreen Verdict = "GREEN"
	// VerdictRed: the command exited non-zero.
	VerdictRed Verdict = "RED"
	// VerdictSuspect: the command exited zero but its output shows a panic,
	// a timeout, a data race or a FAIL line. This is the shape claudia-po
	// caught: status says pass, output says otherwise. Never a green.
	VerdictSuspect Verdict = "SUSPECT"
	// VerdictUnknown: the status could not be established (the process could
	// not be started, or died in a way that yields no code). Renders as
	// "exit=unknown" and is never a green — 🎯T396 acceptance 3.
	VerdictUnknown Verdict = "UNKNOWN"
	// VerdictVoid: the record exists but attests nothing, and has been moved
	// out of the citable store (🎯T441). A run is voided when what it measured
	// was not the gate its name suggests — the archetype is a mistyped
	// subcommand that ran an unrelated program off PATH and recorded the
	// result under a plausible-looking name. Never a green, and deliberately
	// not the same answer as "no such record": the run happened, it just does
	// not attest what a reader would take it to attest.
	VerdictVoid Verdict = "VOID"
	// VerdictKilled: the host (or an operator) terminated the gate before the
	// command decided anything — SIGKILL, exit 137 / OOM, or output that is
	// only "[killed]" (🎯T461). Neither GREEN nor RED: a shot process is not
	// a failing suite, and it is not a pass. Citable as neither.
	VerdictKilled Verdict = "KILLED"
)

// IsGreen reports whether v may be cited as a pass. Exactly one verdict may.
func (v Verdict) IsGreen() bool { return v == VerdictGreen }

// IsKilled reports whether v names termination-by-signal. A killed run decided
// nothing, so it is refused both as a pass and as failing evidence (🎯T461).
func (v Verdict) IsKilled() bool { return v == VerdictKilled }

// exitStatusSIGKILL is the shell / Linux OOM convention for a process the
// kernel shot with SIGKILL (128 + 9). Distinct from a command that exited 1:
// that control is what keeps "every nonzero → killed" from landing.
const exitStatusSIGKILL = 128 + 9

// killedOutputMarker is the whole-output shape observed when a session pane
// dies under host pressure and leaves nothing but the harness's kill notice.
const killedOutputMarker = "[killed]"

// Anomaly is one contradiction found in a gate's own output.
type Anomaly struct {
	// Marker is the substring that matched, e.g. "panic:".
	Marker string `json:"marker"`
	// Line is the output line it was found on, trimmed for reporting.
	Line string `json:"line"`
}

// anomalyMarkers are output shapes that contradict a zero exit status.
//
// Chosen to be narrow on purpose. "panic" appears in ordinary prose about a
// panic that was fixed; "panic:" is the runtime's own prefix. Widening this
// list trades a false green for a false red, and a wrapper that fails
// successful runs gets switched off, which costs more than it saves.
var anomalyMarkers = []string{
	"panic:",
	"fatal error:",
	"--- FAIL",
	"FAIL\t",
	"DATA RACE",
	"test timed out after",
	"signal: killed",
	"signal: segmentation",
	"[build failed]",
}

// anomalyLineCap keeps a quoted line short enough to sit in an attestation
// without turning the record into a second copy of the log.
const anomalyLineCap = 200

// ScanOutput reports contradictions in a gate's captured output. An empty
// result means the output does not argue with a zero exit status; it is not a
// claim that the run was correct.
//
// At most one Anomaly per marker: a suite that fails forty tests should
// produce a readable record, not forty near-identical lines.
func ScanOutput(out string) []Anomaly {
	if out == "" {
		return nil
	}
	var found []Anomaly
	seen := make(map[string]bool, len(anomalyMarkers))
	for _, line := range strings.Split(out, "\n") {
		for _, m := range anomalyMarkers {
			if seen[m] || !strings.Contains(line, m) {
				continue
			}
			seen[m] = true
			found = append(found, Anomaly{Marker: m, Line: trimLine(line)})
		}
	}
	return found
}

func trimLine(line string) string {
	s := strings.TrimSpace(strings.ReplaceAll(line, "\r", ""))
	if len(s) > anomalyLineCap {
		return s[:anomalyLineCap] + "…"
	}
	return s
}

// HostKill reports whether a wait status / captured output means the host
// terminated the gate rather than the command deciding its own outcome
// (🎯T461). Signalled deaths, the 137 SIGKILL convention, and a log that is
// only "[killed]" are host-kills. A genuine exit 1 is not — that is the
// over-broadness control: mapping every nonzero status to killed must fail it.
func HostKill(signaled bool, statusKnown bool, status int, output string) bool {
	if signaled {
		return true
	}
	if statusKnown && status == exitStatusSIGKILL {
		return true
	}
	return outputIsOnlyKilledMarker(output)
}

// outputIsOnlyKilledMarker reports the harness-only "[killed]" log: trimmed
// content equals the marker and nothing else. Broader matches would reclassify
// suites that merely print the word.
func outputIsOnlyKilledMarker(output string) bool {
	return strings.TrimSpace(output) == killedOutputMarker
}

// verdictFor derives the reportable verdict from the raw wait status and the
// output. Kept separate from Run so the decision is testable without a
// subprocess, and so there is exactly one place that can call something green.
// hostKill is decided by HostKill before this runs; it wins over every other
// reading because a shot process arrived at no status of its own.
func verdictFor(statusKnown bool, status int, anomalies []Anomaly, hostKill bool) Verdict {
	if hostKill {
		return VerdictKilled
	}
	if !statusKnown {
		return VerdictUnknown
	}
	if status != 0 {
		return VerdictRed
	}
	if len(anomalies) > 0 {
		return VerdictSuspect
	}
	return VerdictGreen
}

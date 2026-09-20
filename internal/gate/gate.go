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
	// VerdictDirty: the command exited zero without contradicting output, but
	// the measured tree carried uncommitted changes (🎯T718). The run passed;
	// it did not measure the commit alone. Distinct from GREEN so quoting the
	// GATE line cannot read as a pass. Never a green.
	VerdictDirty Verdict = "DIRTY"
	// VerdictEmpty: the command exited zero without contradicting output, but
	// it executed no tests (🎯T719). Typical shape: go test -run matching
	// nothing prints [no tests to run] and still exits 0. Distinct from
	// GREEN so quoting the GATE line cannot read as a pass, and distinct
	// from SUSPECT because the output does not contradict a pass — nothing
	// ran. Never a green.
	VerdictEmpty Verdict = "EMPTY"
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
// panic that was fixed; "panic:" is the runtime's own prefix. 🎯T737 then
// requires the rest of the runtime/test-output shape: "panic:/FAIL/DATA RACE"
// is a name catalog, not a panic. Widening this list, or matching the
// marker substring without that shape, trades a false green for a false
// red, and a wrapper that fails successful runs gets switched off, which
// costs more than it saves.
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
//
// A marker counts only in output shape (🎯T737): `panic: message`,
// `--- FAIL: TestX`, `WARNING: DATA RACE`. A report that names those
// markers — "ScanOutput markers are panic:/FAIL/DATA RACE/timeout" — is
// not a contradiction.
func ScanOutput(out string) []Anomaly {
	if out == "" {
		return nil
	}
	var found []Anomaly
	seen := make(map[string]bool, len(anomalyMarkers))
	for _, line := range strings.Split(out, "\n") {
		for _, m := range anomalyMarkers {
			if seen[m] || !outputShapedMarker(line, m) {
				continue
			}
			seen[m] = true
			found = append(found, Anomaly{Marker: m, Line: trimLine(line)})
		}
	}
	return found
}

// LooksLikeMarkerProse reports whether line names ScanOutput's failure
// markers without quoting a run's output (🎯T737). A catalog such as
// "panic:/FAIL/DATA RACE/timeout", a fog-known line listing those names,
// or an acceptance clause of execution-evidence tokens is prose. A go-test
// `--- FAIL: TestX` or a runtime `panic: message` is not.
//
// This is a separate pass from 🎯T722 RoleControl. RoleControl is a role
// on a cited RED gate (before-prose, -before/-control names, gate-role
// slot). T737's specimen cited no RED: it named the markers in fog-known
// and acceptance prose. Extending RoleControl would leave that scout
// flagged, and would not tell ScanOutput that a catalog is not a panic.
func LooksLikeMarkerProse(line string) bool {
	hit := false
	for _, m := range anomalyMarkers {
		if !strings.Contains(line, m) {
			continue
		}
		hit = true
		if outputShapedMarker(line, m) {
			return false
		}
	}
	return hit
}

// outputShapedMarker reports whether marker occurs on line as captured
// gate/test output rather than as a name being discussed (🎯T737).
func outputShapedMarker(line, marker string) bool {
	switch marker {
	case "panic:", "fatal error:":
		return prefixedRuntimeMessage(line, marker)
	case "--- FAIL":
		return failTestOutput(line)
	case "DATA RACE":
		return dataRaceOutput(line)
	default:
		return strings.Contains(line, marker)
	}
}

// prefixedRuntimeMessage is Go's `panic: <message>` / `fatal error: <message>`.
// A catalog join (`panic:/FAIL`, `panic:, DATA RACE`) has no message.
func prefixedRuntimeMessage(line, prefix string) bool {
	for {
		i := strings.Index(line, prefix)
		if i < 0 {
			return false
		}
		rest := strings.TrimLeft(line[i+len(prefix):], " \t")
		if rest == "" {
			return false
		}
		if isMarkerCatalogJoin(rest[0]) {
			line = rest
			continue
		}
		return true
	}
}

func isMarkerCatalogJoin(c byte) bool {
	switch c {
	case '/', ',', ')', ']', '}', '|', ';', ':', '*', '_', '`', '\'', '"', '.', '!', '?':
		return true
	default:
		return false
	}
}

// failTestOutput is go test's `--- FAIL: TestName`. An acceptance list
// (`--- FAIL:, an ok line`) has no identifier after the colon.
func failTestOutput(line string) bool {
	const p = "--- FAIL: "
	i := strings.Index(line, p)
	if i < 0 {
		return false
	}
	rest := line[i+len(p):]
	return rest != "" && !isMarkerCatalogJoin(rest[0])
}

// dataRaceOutput is the race detector's `WARNING: DATA RACE`. A name
// catalog (`FAIL/DATA RACE/timeout`) or a backticked token is not.
func dataRaceOutput(line string) bool {
	return strings.Contains(line, "WARNING: DATA RACE")
}

// EmptyRun reports whether captured output is a Go test run that executed
// no tests (🎯T719). Whole-run, not per-package: a mixed `go test ./... -run`
// that matches in one package and prints [no tests to run] in others is not
// empty. Non-Go commands (true, echo ok) have none of the markers and stay
// a pass. Skipped tests (`=== RUN` / `--- SKIP:`) count as executed.
func EmptyRun(out string) bool {
	if !hasGoEmptyMarker(out) {
		return false
	}
	return !hasTestExecution(out)
}

// EmptyPackages names the packages in a Go test run's output that executed
// no tests (🎯T739): `ok pkg 0.1s [no tests to run]` and `? pkg [no test
// files]`. EmptyRun answers "did anything run at all"; this answers "which
// of the packages the command named proved nothing", so a partly-empty
// multi-package gate cannot be cited as if every package it named ran.
// Order of first appearance, deduplicated. Non-Go output yields nil.
func EmptyPackages(out string) []string {
	var pkgs []string
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		trim := strings.TrimSpace(strings.ReplaceAll(line, "\r", ""))
		var rest string
		switch {
		case strings.HasPrefix(trim, "ok") && strings.Contains(trim, "[no tests to run]"):
			rest = strings.TrimSpace(trim[len("ok"):])
		case strings.HasPrefix(trim, "?") && strings.Contains(trim, "[no test files]"):
			rest = strings.TrimSpace(trim[len("?"):])
		default:
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 || seen[fields[0]] {
			continue
		}
		seen[fields[0]] = true
		pkgs = append(pkgs, fields[0])
	}
	return pkgs
}

func hasGoEmptyMarker(out string) bool {
	return strings.Contains(out, "[no tests to run]") ||
		strings.Contains(out, "testing: warning: no tests to run") ||
		strings.Contains(out, "[no test files]")
}

func hasTestExecution(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "=== RUN") ||
			strings.Contains(line, "--- PASS:") ||
			strings.Contains(line, "--- SKIP:") ||
			strings.Contains(line, "--- FAIL:") {
			return true
		}
		trim := strings.TrimSpace(line)
		if isGoOKPackageLine(trim) && !strings.Contains(trim, "[no tests to run]") {
			return true
		}
		if jsonEventRanATest(trim) {
			return true
		}
	}
	return false
}

// isGoOKPackageLine is go test's package-ok line (`ok\tpkg\t0.1s`), not a
// bare `echo ok`.
func isGoOKPackageLine(line string) bool {
	if !strings.HasPrefix(line, "ok") {
		return false
	}
	rest := strings.TrimSpace(line[len("ok"):])
	return rest != ""
}

// jsonEventRanATest is a go test -json pass/fail/skip/run event that names
// a test. Package-level pass (no Test) is the empty-run JSON shape.
func jsonEventRanATest(line string) bool {
	if !strings.Contains(line, `"Action":"`) || !strings.Contains(line, `"Test":"`) {
		return false
	}
	if strings.Contains(line, `"Test":""`) {
		return false
	}
	return strings.Contains(line, `"Action":"pass"`) ||
		strings.Contains(line, `"Action":"fail"`) ||
		strings.Contains(line, `"Action":"skip"`) ||
		strings.Contains(line, `"Action":"run"`)
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
// reading because a shot process arrived at no status of its own. empty is
// EmptyRun of the captured output: a zero-test Go run is EMPTY, not GREEN.
func verdictFor(statusKnown bool, status int, anomalies []Anomaly, hostKill bool, empty bool) Verdict {
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
	if empty {
		return VerdictEmpty
	}
	return VerdictGreen
}

// applyTreeVerdict demotes a would-be GREEN when the measured tree carried
// uncommitted changes (🎯T718). RED, SUSPECT, EMPTY, KILLED and UNKNOWN
// already answer their own questions and are left alone — a run can be both
// dirty and empty, and EMPTY wins so the worker fixes the -run pattern
// rather than -clean-ing a suite that still executed nothing (🎯T719). A
// nil tree is provenance unknown, which is not DIRTY — absence is not a
// claim about dirt (🎯T397).
func applyTreeVerdict(v Verdict, tree *TreeProvenance) Verdict {
	if v != VerdictGreen {
		return v
	}
	if tree == nil || tree.Clean || tree.DirtyFiles == 0 {
		return v
	}
	return VerdictDirty
}
